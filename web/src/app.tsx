import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { HardDrive, LockKeyhole, LogOut, Plus, RefreshCw, ShieldAlert, TriangleAlert } from "lucide-react";
import { useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { api } from "./api/client";
import { Dashboard } from "./dashboard";

type AuthStatus = { setupRequired: boolean; authenticated: boolean; username?: string };
type BuildInfo = { version: string };
type Operation = { path: string; body: Record<string, unknown> };

class ApiError extends Error {
  constructor(public code: string, public params: Record<string, unknown> = {}) {
    super(code);
  }
}

export function App() {
  const { t } = useTranslation();
  const status = useQuery({ queryKey: ["auth-status"], queryFn: () => request<AuthStatus>("/api/auth/status") });
  const buildInfo = useQuery({ queryKey: ["build-info"], queryFn: () => request<BuildInfo>("/api/build-info") });
  const version = buildInfo.data?.version ?? "dev";
  if (status.isLoading) return <AuthShell version={version}><p className="text-sm text-slate-400">{t("auth.checking")}</p></AuthShell>;
  if (status.isError) return <AuthShell version={version}><p className="text-sm text-rose-300">{t("auth.unavailable")}</p></AuthShell>;
  if (status.data?.setupRequired) return <Bootstrap version={version} onComplete={() => void status.refetch()} />;
  if (!status.data?.authenticated) return <Login version={version} username={status.data?.username ?? ""} onComplete={() => void status.refetch()} />;
  return <Dashboard version={version} username={status.data.username ?? ""} onLogout={() => void status.refetch()} />;
}

function Bootstrap({ version, onComplete }: { version: string; onComplete: () => void }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState("");
  const [systemPassword, setSystemPassword] = useState("");
  const [projectPassword, setProjectPassword] = useState("");
  const [repeatPassword, setRepeatPassword] = useState("");
  const bootstrap = useMutation({ mutationFn: () => request<AuthStatus>("/api/auth/bootstrap", { username, systemPassword, projectPassword }), onSuccess: onComplete });
  function submit(event: FormEvent) {
    event.preventDefault();
    if (projectPassword !== repeatPassword) return;
    bootstrap.mutate();
  }
  return <AuthShell version={version}><AuthCard title={t("auth.initializeTitle")} description={t("auth.initializeDescription")}><form onSubmit={submit} className="space-y-4"><Field label={t("auth.localUsername")} value={username} onChange={setUsername} autoComplete="username" /><Field label={t("auth.systemPassword")} value={systemPassword} onChange={setSystemPassword} type="password" autoComplete="current-password" /><Field label={t("auth.projectPassword")} value={projectPassword} onChange={setProjectPassword} type="password" autoComplete="new-password" hint={t("auth.passwordHint")} /><Field label={t("auth.repeatPassword")} value={repeatPassword} onChange={setRepeatPassword} type="password" autoComplete="new-password" />{projectPassword !== repeatPassword && <p className="text-xs text-rose-300">{t("auth.passwordMismatch")}</p>}<AuthError error={bootstrap.error} /><SubmitButton pending={bootstrap.isPending}>{t("auth.initialize")}</SubmitButton></form></AuthCard></AuthShell>;
}

function Login({ version, username, onComplete }: { version: string; username: string; onComplete: () => void }) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const login = useMutation({ mutationFn: () => request<AuthStatus>("/api/auth/login", { username, password }), onSuccess: onComplete });
  return <AuthShell version={version}><AuthCard title={t("auth.signInTitle")} description={t("auth.signInDescription", { username })}><form onSubmit={(event) => { event.preventDefault(); login.mutate(); }} className="space-y-4"><Field label={t("auth.username")} value={username} onChange={() => undefined} disabled /><Field label={t("auth.projectPassword")} value={password} onChange={setPassword} type="password" autoComplete="current-password" /><AuthError error={login.error} /><SubmitButton pending={login.isPending}>{t("auth.signIn")}</SubmitButton></form></AuthCard></AuthShell>;
}

function LegacyDashboard({ username, onLogout }: { username: string; onLogout: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const disks = useQuery({ queryKey: ["disks"], queryFn: async () => {
    const { data, error } = await api.GET("/api/disks");
    if (error || !data) throw new ApiError("request_failed");
    return data.disks;
  } });
  const operation = useMutation({ mutationFn: ({ path, body }: Operation) => request(path, body), onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["disks"] }) });
  function confirmAndRun(target: string, next: (confirm: string) => Operation) {
    const confirm = window.prompt(t("prompts.confirm", { target }));
    if (confirm === target) operation.mutate(next(confirm));
  }
  function createPartition(diskPath: string) {
    const gibibytes = Number(window.prompt(t("prompts.partitionSize"), "100"));
    if (!Number.isFinite(gibibytes) || gibibytes <= 0) return;
    const name = window.prompt(t("prompts.partitionName"), "") ?? "";
    confirmAndRun(diskPath, (confirm) => ({ path: "/api/partitions", body: { diskPath, sizeBytes: Math.round(gibibytes * 1024 ** 3), name, confirm } }));
  }
  function format(partitionPath: string) {
    const fileSystem = window.prompt(t("prompts.filesystem"), "ext4");
    if (fileSystem === "ext4" || fileSystem === "xfs") confirmAndRun(partitionPath, (confirm) => ({ path: "/api/partitions/format", body: { partitionPath, fileSystem, confirm } }));
  }
  async function logout() {
    await fetch("/api/auth/logout", { method: "POST" });
    queryClient.clear();
    onLogout();
  }
  return <main className="min-h-screen bg-slate-950 px-6 py-10 text-slate-100"><section className="mx-auto max-w-5xl"><header className="flex items-center justify-between border-b border-slate-800 pb-6"><div className="flex items-center gap-3"><HardDrive className="text-cyan-400" /><div><h1 className="text-xl font-semibold">SimpleFSManager</h1><p className="text-sm text-slate-400">{t("dashboard.signedIn", { username })}</p></div></div><div className="flex items-center gap-4"><LanguageSelect /><button onClick={() => void logout()} className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-slate-100"><LogOut size={16} />{t("dashboard.signOut")}</button></div></header><section className="mt-12"><div className="mb-5 flex items-end justify-between"><div><h2 className="text-lg font-medium">{t("dashboard.physicalDisks")}</h2><p className="mt-1 text-sm text-slate-400">{t("dashboard.protection")}</p></div><button onClick={() => void disks.refetch()} className="rounded-lg border border-slate-700 p-2 text-slate-300 hover:border-cyan-400 hover:text-cyan-300" aria-label={t("dashboard.refresh")}><RefreshCw size={17} /></button></div>{operation.isError && <p className="mb-4 rounded-xl border border-rose-900 bg-rose-950/40 p-4 text-sm text-rose-200"><ErrorText error={operation.error} /></p>}{operation.isPending && <p className="mb-4 text-sm text-cyan-300">{t("dashboard.operationPending")}</p>}{disks.isLoading && <p className="text-sm text-slate-400">{t("dashboard.discovering")}</p>}{disks.isError && <p className="rounded-xl border border-rose-900 bg-rose-950/40 p-4 text-sm text-rose-200">{t("dashboard.discoveryFailed")}</p>}<div className="grid gap-4">{disks.data?.map((disk) => { const partitions = disk.partitions ?? []; return <article key={disk.path} className="rounded-2xl border border-slate-800 bg-slate-900/50 p-6"><div className="flex flex-wrap items-start justify-between gap-4"><div><div className="flex items-center gap-2"><HardDrive size={18} className="text-cyan-400" /><h3 className="font-medium">{disk.model || disk.name}</h3></div><p className="mt-2 font-mono text-xs text-slate-400">{disk.path}{disk.serial ? ` · ${disk.serial}` : ""}</p></div><span className={disk.system ? "flex items-center gap-1.5 text-xs text-rose-300" : disk.protected ? "flex items-center gap-1.5 text-xs text-amber-300" : "text-xs text-emerald-300"}>{(disk.system || disk.protected) && <ShieldAlert size={14} />}{disk.system ? t("dashboard.systemDisk") : disk.protected ? t("dashboard.mounted") : t("dashboard.available")}</span></div><div className="mt-5 grid grid-cols-2 gap-4 text-sm sm:grid-cols-3"><Metric label={t("dashboard.capacity")} value={formatBytes(disk.sizeBytes)} /><Metric label={t("dashboard.partitionTable")} value={disk.partitioning || t("dashboard.none")} /><Metric label={t("dashboard.partitions")} value={String(partitions.length)} /></div><div className="mt-5 flex flex-wrap gap-2">{!disk.protected && <DangerButton onClick={() => confirmAndRun(disk.path, (confirm) => ({ path: "/api/disks/gpt", body: { diskPath: disk.path, confirm } }))}>{t("dashboard.initializeGPT")}</DangerButton>}{!disk.protected && disk.partitioning === "gpt" && <ActionButton onClick={() => createPartition(disk.path)}><Plus size={15} /> {t("dashboard.createPartition")}</ActionButton>}</div>{partitions.length > 0 && <div className="mt-5 border-t border-slate-800 pt-4">{partitions.map((partition) => <div key={partition.path} className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-800/70 py-3 last:border-0"><div><p className="font-mono text-sm text-slate-300">{partition.path}</p><p className="mt-1 text-xs text-slate-400">{partition.fileSystem || t("dashboard.unformatted")} · {formatBytes(partition.sizeBytes)}{partition.mountpoints?.length > 0 ? ` · ${partition.mountpoints.join(", ")}` : ""}</p></div><div className="flex flex-wrap gap-2">{partition.mountpoints?.some((path) => path.startsWith("/vol")) && partition.uuid && <ActionButton onClick={() => confirmAndRun(partition.uuid, (confirm) => ({ path: "/api/volumes/unmount", body: { uuid: partition.uuid, confirm } }))}>{t("dashboard.unmount")}</ActionButton>}{!disk.system && !partition.mountpoints?.length && <>{partition.fileSystem && <ActionButton onClick={() => confirmAndRun(partition.path, (confirm) => ({ path: "/api/volumes/mount", body: { partitionPath: partition.path, confirm } }))}>{t("dashboard.mount")}</ActionButton>}<DangerButton onClick={() => format(partition.path)}>{t("dashboard.format")}</DangerButton><DangerButton onClick={() => confirmAndRun(disk.path, (confirm) => ({ path: "/api/partitions/delete", body: { diskPath: disk.path, partitionNumber: partition.number, confirm } }))}>{t("dashboard.delete")}</DangerButton></>}</div></div>)}</div>}</article>; })}</div></section></section></main>;
}

function AuthShell({ version, children }: { version: string; children: ReactNode }) { return <main className="relative grid min-h-screen place-items-center bg-slate-950 px-6 text-slate-100"><div className="absolute right-6 top-6"><LanguageSelect /></div><div className="w-full max-w-md">{children}</div><p className="absolute bottom-6 text-xs text-slate-600">{version}</p></main>; }
function AuthCard({ title, description, children }: { title: string; description: string; children: ReactNode }) { return <section className="rounded-2xl border border-slate-800 bg-slate-900/70 p-7 shadow-2xl shadow-black/30"><div className="mb-6 flex items-center gap-3"><div className="rounded-xl bg-cyan-400/10 p-2 text-cyan-300"><LockKeyhole size={20} /></div><div><h1 className="font-semibold">{title}</h1><p className="mt-1 text-sm text-slate-400">{description}</p></div></div>{children}</section>; }
function LanguageSelect() { const { i18n, t } = useTranslation(); return <label className="text-xs text-slate-400"><span className="sr-only">{t("language.label")}</span><select className="rounded-md border border-slate-700 bg-slate-900 px-2 py-1 text-slate-200" value={i18n.resolvedLanguage === "zh-CN" ? "zh-CN" : "en"} onChange={(event) => void i18n.changeLanguage(event.target.value)}><option value="zh-CN">{t("language.chinese")}</option><option value="en">{t("language.english")}</option></select></label>; }
function Field({ label, value, onChange, type = "text", hint, disabled, autoComplete }: { label: string; value: string; onChange: (value: string) => void; type?: string; hint?: string; disabled?: boolean; autoComplete?: string }) { return <label className="block text-sm text-slate-300">{label}<input className="mt-1.5 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100 outline-none focus:border-cyan-400 disabled:cursor-not-allowed disabled:opacity-60" value={value} onChange={(event) => onChange(event.target.value)} type={type} disabled={disabled} autoComplete={autoComplete} required />{hint && <span className="mt-1 block text-xs text-slate-500">{hint}</span>}</label>; }
function AuthError({ error }: { error: Error | null }) { return error ? <p className="rounded-lg bg-rose-950/50 p-3 text-sm text-rose-200"><ErrorText error={error} /></p> : null; }
function ErrorText({ error }: { error: Error }) { const { t } = useTranslation(); const apiError = error instanceof ApiError ? error : new ApiError("internal_error"); return <>{t(`errors.${apiError.code}`, { ...apiError.params, defaultValue: t("errors.internal_error") })}</>; }
function SubmitButton({ children, pending }: { children: ReactNode; pending: boolean }) { const { t } = useTranslation(); return <button disabled={pending} className="w-full rounded-lg bg-cyan-400 px-4 py-2.5 text-sm font-medium text-slate-950 hover:bg-cyan-300 disabled:opacity-60">{pending ? t("auth.waiting") : children}</button>; }
function Metric({ label, value }: { label: string; value: string }) { return <div><p className="text-xs uppercase tracking-wide text-slate-500">{label}</p><p className="mt-1 text-slate-200">{value}</p></div>; }
function ActionButton({ children, onClick }: { children: ReactNode; onClick: () => void }) { return <button onClick={onClick} className="inline-flex items-center gap-1.5 rounded-lg border border-slate-700 px-3 py-1.5 text-xs text-slate-200 hover:border-cyan-400 hover:text-cyan-300">{children}</button>; }
function DangerButton({ children, onClick }: { children: ReactNode; onClick: () => void }) { return <button onClick={onClick} className="inline-flex items-center gap-1.5 rounded-lg border border-rose-900/80 px-3 py-1.5 text-xs text-rose-300 hover:bg-rose-950/50"><TriangleAlert size={14} />{children}</button>; }
async function request<T = unknown>(path: string, body?: Record<string, unknown>): Promise<T> { const response = await fetch(path, { method: body ? "POST" : "GET", headers: body ? { "Content-Type": "application/json" } : undefined, body: body ? JSON.stringify(body) : undefined }); if (!response.ok) { const error = await response.json().catch(() => null) as { code?: string; detail?: string; params?: Record<string, unknown> } | null; throw new ApiError(error?.code ?? error?.detail ?? "request_failed", error?.params); } return response.status === 204 ? undefined as T : response.json() as Promise<T>; }
function formatBytes(bytes: number) { if (bytes < 1024) return `${bytes} B`; const units = ["KiB", "MiB", "GiB", "TiB", "PiB"]; const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1); return `${(bytes / 1024 ** (index + 1)).toFixed(1)} ${units[index]}`; }
