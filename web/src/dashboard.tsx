import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { HardDrive, LogOut, Plus, RefreshCw, ShieldAlert, TriangleAlert, Usb } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ReactNode } from "react";
import { api } from "./api/client";

type Operation = { path: string; body: Record<string, unknown> };

class ApiError extends Error {
  constructor(public code: string) {
    super(code);
  }
}

export function Dashboard({ version, username, onLogout }: { version: string; username: string; onLogout: () => void }) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const disks = useQuery({ queryKey: ["disks"], queryFn: async () => {
    const { data, error } = await api.GET("/api/disks");
    if (error || !data) throw new ApiError("request_failed");
    return data.disks;
  } });
  const operation = useMutation({
    mutationFn: ({ path, body }: Operation) => request(path, body),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["disks"] }),
  });

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
    if (fileSystem === "ext4" || fileSystem === "xfs") {
      confirmAndRun(partitionPath, (confirm) => ({ path: "/api/partitions/format", body: { partitionPath, fileSystem, confirm } }));
    }
  }

  async function logout() {
    await fetch("/api/auth/logout", { method: "POST" });
    queryClient.clear();
    onLogout();
  }

  return <main className="min-h-screen bg-slate-950 px-6 py-10 text-slate-100">
    <section className="mx-auto max-w-5xl">
      <header className="flex items-center justify-between border-b border-slate-800 pb-6">
        <div className="flex items-center gap-3"><HardDrive className="text-cyan-400" /><div><h1 className="text-xl font-semibold">SimpleFSManager</h1><p className="text-sm text-slate-400">{t("dashboard.signedIn", { username })}</p><p className="mt-1 text-xs text-slate-600">{version}</p></div></div>
        <div className="flex items-center gap-4"><LanguageSelect value={i18n.resolvedLanguage} onChange={(language) => void i18n.changeLanguage(language)} /><button onClick={() => void logout()} className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-slate-100"><LogOut size={16} />{t("dashboard.signOut")}</button></div>
      </header>
      <section className="mt-12">
        <div className="mb-5 flex items-end justify-between"><div><h2 className="text-lg font-medium">{t("dashboard.physicalDisks")}</h2><p className="mt-1 text-sm text-slate-400">{t("dashboard.protection")}</p></div><button onClick={() => void disks.refetch()} className="rounded-lg border border-slate-700 p-2 text-slate-300 hover:border-cyan-400 hover:text-cyan-300" aria-label={t("dashboard.refresh")}><RefreshCw size={17} /></button></div>
        {operation.isError && <p className="mb-4 rounded-xl border border-rose-900 bg-rose-950/40 p-4 text-sm text-rose-200"><ErrorText error={operation.error} /></p>}
        {operation.isPending && <p className="mb-4 text-sm text-cyan-300">{t("dashboard.operationPending")}</p>}
        {disks.isLoading && <p className="text-sm text-slate-400">{t("dashboard.discovering")}</p>}
        {disks.isError && <p className="rounded-xl border border-rose-900 bg-rose-950/40 p-4 text-sm text-rose-200">{t("dashboard.discoveryFailed")}</p>}
        <div className="grid gap-4">{disks.data?.map((disk) => {
          const partitions = disk.partitions ?? [];
          return <article key={disk.path} className="rounded-2xl border border-slate-800 bg-slate-900/50 p-6">
            <div className="flex flex-wrap items-start justify-between gap-4"><div><div className="flex items-center gap-2"><HardDrive size={18} className="text-cyan-400" /><h3 className="font-medium">{disk.model || disk.name}</h3>{disk.usb && <span className="inline-flex items-center gap-1 rounded-full bg-cyan-400/10 px-2 py-0.5 text-xs text-cyan-300"><Usb size={12} />USB</span>}</div><p className="mt-2 font-mono text-xs text-slate-400">{disk.path}{disk.serial ? ` · ${disk.serial}` : ""}</p></div><span className={disk.system ? "flex items-center gap-1.5 text-xs text-rose-300" : disk.protected ? "flex items-center gap-1.5 text-xs text-amber-300" : "text-xs text-emerald-300"}>{(disk.system || disk.protected) && <ShieldAlert size={14} />}{disk.system ? t("dashboard.systemDisk") : disk.protected ? t("dashboard.mounted") : t("dashboard.available")}</span></div>
            <div className="mt-5 grid grid-cols-2 gap-4 text-sm sm:grid-cols-4"><Metric label={t("dashboard.capacity")} value={formatBytes(disk.sizeBytes)} /><Metric label={t("dashboard.transport")} value={disk.transport || t("dashboard.unknown")} /><Metric label={t("dashboard.partitionTable")} value={disk.partitioning || t("dashboard.none")} /><Metric label={t("dashboard.partitions")} value={String(partitions.length)} /></div>
            {!disk.usb && <div className="mt-5 flex flex-wrap gap-2">{!disk.protected && <DangerButton onClick={() => confirmAndRun(disk.path, (confirm) => ({ path: "/api/disks/gpt", body: { diskPath: disk.path, confirm } }))}>{t("dashboard.initializeGPT")}</DangerButton>}{!disk.protected && disk.partitioning === "gpt" && <ActionButton onClick={() => createPartition(disk.path)}><Plus size={15} /> {t("dashboard.createPartition")}</ActionButton>}</div>}
            {partitions.length > 0 && <div className="mt-5 border-t border-slate-800 pt-4">{partitions.map((partition) => <div key={partition.path} className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-800/70 py-3 last:border-0"><div><p className="font-mono text-sm text-slate-300">{partition.path}</p><p className="mt-1 text-xs text-slate-400">{partition.fileSystem || t("dashboard.unformatted")} · {formatBytes(partition.sizeBytes)}{partition.mountpoints?.length > 0 ? ` · ${partition.mountpoints.join(", ")}` : ""}</p></div><div className="flex flex-wrap gap-2">{disk.usb ? <USBPartitionActions partition={partition} confirmAndRun={confirmAndRun} /> : <ManagedPartitionActions diskPath={disk.path} partition={partition} system={disk.system} confirmAndRun={confirmAndRun} format={format} />}</div></div>)}</div>}
          </article>;
        })}</div>
      </section>
    </section>
  </main>;
}

function USBPartitionActions({ partition, confirmAndRun }: { partition: { path: string; mountpoints: string[] }; confirmAndRun: (target: string, next: (confirm: string) => Operation) => void }) {
  const { t } = useTranslation();
  if (partition.mountpoints?.some((path) => path.startsWith("/usb"))) return <ActionButton onClick={() => confirmAndRun(partition.path, (confirm) => ({ path: "/api/usb/unmount", body: { partitionPath: partition.path, confirm } }))}>{t("dashboard.unmount")}</ActionButton>;
  if (partition.mountpoints?.length > 0) return null;
  return <ActionButton onClick={() => confirmAndRun(partition.path, (confirm) => ({ path: "/api/usb/mount", body: { partitionPath: partition.path, confirm } }))}>{t("dashboard.mount")}</ActionButton>;
}

function ManagedPartitionActions({ diskPath, partition, system, confirmAndRun, format }: { diskPath: string; partition: { path: string; number: number; uuid: string; fileSystem: string; mountpoints: string[] }; system: boolean; confirmAndRun: (target: string, next: (confirm: string) => Operation) => void; format: (path: string) => void }) {
  const { t } = useTranslation();
  if (partition.mountpoints?.some((path) => path.startsWith("/vol")) && partition.uuid) return <ActionButton onClick={() => confirmAndRun(partition.uuid, (confirm) => ({ path: "/api/volumes/unmount", body: { uuid: partition.uuid, confirm } }))}>{t("dashboard.unmount")}</ActionButton>;
  if (system || partition.mountpoints?.length > 0) return null;
  return <><>{partition.fileSystem && <ActionButton onClick={() => confirmAndRun(partition.path, (confirm) => ({ path: "/api/volumes/mount", body: { partitionPath: partition.path, confirm } }))}>{t("dashboard.mount")}</ActionButton>}</><DangerButton onClick={() => format(partition.path)}>{t("dashboard.format")}</DangerButton><DangerButton onClick={() => confirmAndRun(diskPath, (confirm) => ({ path: "/api/partitions/delete", body: { diskPath, partitionNumber: partition.number, confirm } }))}>{t("dashboard.delete")}</DangerButton></>;
}

function LanguageSelect({ value, onChange }: { value?: string; onChange: (language: string) => void }) { const { t } = useTranslation(); return <label className="text-xs text-slate-400"><span className="sr-only">{t("language.label")}</span><select className="rounded-md border border-slate-700 bg-slate-900 px-2 py-1 text-slate-200" value={value === "zh-CN" ? "zh-CN" : "en"} onChange={(event) => onChange(event.target.value)}><option value="zh-CN">{t("language.chinese")}</option><option value="en">{t("language.english")}</option></select></label>; }
function ErrorText({ error }: { error: Error }) { const { t } = useTranslation(); const code = error instanceof ApiError ? error.code : "internal_error"; return <>{t(`errors.${code}`, { defaultValue: t("errors.internal_error") })}</>; }
function Metric({ label, value }: { label: string; value: string }) { return <div><p className="text-xs uppercase tracking-wide text-slate-500">{label}</p><p className="mt-1 text-slate-200">{value}</p></div>; }
function ActionButton({ children, onClick }: { children: ReactNode; onClick: () => void }) { return <button onClick={onClick} className="inline-flex items-center gap-1.5 rounded-lg border border-slate-700 px-3 py-1.5 text-xs text-slate-200 hover:border-cyan-400 hover:text-cyan-300">{children}</button>; }
function DangerButton({ children, onClick }: { children: ReactNode; onClick: () => void }) { return <button onClick={onClick} className="inline-flex items-center gap-1.5 rounded-lg border border-rose-900/80 px-3 py-1.5 text-xs text-rose-300 hover:bg-rose-950/50"><TriangleAlert size={14} />{children}</button>; }
async function request(path: string, body: Record<string, unknown>) { const response = await fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }); if (!response.ok) { const error = await response.json().catch(() => null) as { code?: string; detail?: string } | null; throw new ApiError(error?.code ?? error?.detail ?? "request_failed"); } return response.json(); }
function formatBytes(bytes: number) { if (bytes < 1024) return `${bytes} B`; const units = ["KiB", "MiB", "GiB", "TiB", "PiB"]; const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1); return `${(bytes / 1024 ** (index + 1)).toFixed(1)} ${units[index]}`; }
