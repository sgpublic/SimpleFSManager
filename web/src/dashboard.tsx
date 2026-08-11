import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  HardDrive,
  LogOut,
  Plus,
  Power,
  RefreshCw,
  ShieldAlert,
  TriangleAlert,
  Usb,
  X,
} from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { api } from "./api/client";

type Operation = { path: string; body: Record<string, unknown> };
type OperationResult = { message: string; rebootRequired?: boolean };
type ConfirmDialog = {
  kind: "confirm";
  title: string;
  target: string;
  operation: Operation;
};
type FormatDialog = { kind: "format"; target: string };
type CreateDialog = { kind: "create"; target: string; maxGiB: number };
type Dialog = ConfirmDialog | FormatDialog | CreateDialog;

class ApiError extends Error {
  constructor(public code: string) {
    super(code);
  }
}

export function Dashboard({
  version,
  username,
  onLogout,
}: {
  version: string;
  username: string;
  onLogout: () => void;
}) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [rebootRequired, setRebootRequired] = useState(false);
  const disks = useQuery({
    queryKey: ["disks"],
    queryFn: async () => {
      const { data, error } = await api.GET("/api/disks");
      if (error || !data) throw new ApiError("request_failed");
      return data.disks;
    },
  });
  const operation = useMutation({
    mutationFn: ({ path, body }: Operation) => request(path, body),
    onSuccess: (result: OperationResult) => {
      setDialog(null);
      setRebootRequired(Boolean(result.rebootRequired));
      void queryClient.invalidateQueries({ queryKey: ["disks"] });
    },
  });

  function confirm(
    target: string,
    title: string,
    next: (confirmation: string) => Operation,
  ) {
    setDialog({ kind: "confirm", title, target, operation: next(target) });
  }

  async function logout() {
    await fetch("/api/auth/logout", { method: "POST" });
    queryClient.clear();
    onLogout();
  }

  return (
    <main className="min-h-screen bg-slate-950 px-6 py-10 text-slate-100">
      <section className="mx-auto max-w-5xl">
        <header className="flex items-center justify-between border-b border-slate-800 pb-6">
          <div className="flex items-center gap-3">
            <HardDrive className="text-cyan-400" />
            <div>
              <h1 className="text-xl font-semibold">SimpleFSManager</h1>
              <p className="text-sm text-slate-400">
                {t("dashboard.signedIn", { username })}
              </p>
              <p className="mt-1 text-xs text-slate-600">{version}</p>
            </div>
          </div>
          <div className="flex items-center gap-4">
            <LanguageSelect
              value={i18n.resolvedLanguage}
              onChange={(language) => void i18n.changeLanguage(language)}
            />
            <button
              onClick={() =>
                confirm(
                  "system",
                  t("dashboard.restartSystem"),
                  (confirmation) => ({
                    path: "/api/system/reboot",
                    body: { confirm: confirmation },
                  }),
                )
              }
              className="inline-flex items-center gap-2 text-sm text-amber-300 hover:text-amber-100"
            >
              <Power size={16} />
              {t("dashboard.restartSystem")}
            </button>
            <button
              onClick={() => void logout()}
              className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-slate-100"
            >
              <LogOut size={16} />
              {t("dashboard.signOut")}
            </button>
          </div>
        </header>
        <section className="mt-12">
          <div className="mb-5 flex items-end justify-between">
            <div>
              <h2 className="text-lg font-medium">
                {t("dashboard.physicalDisks")}
              </h2>
              <p className="mt-1 text-sm text-slate-400">
                {t("dashboard.protection")}
              </p>
            </div>
            <button
              onClick={() => void disks.refetch()}
              className="rounded-lg border border-slate-700 p-2 text-slate-300 hover:border-cyan-400 hover:text-cyan-300"
              aria-label={t("dashboard.refresh")}
            >
              <RefreshCw size={17} />
            </button>
          </div>
          {operation.isPending && (
            <p className="mb-4 text-sm text-cyan-300">
              {t("dashboard.operationPending")}
            </p>
          )}
          {disks.isLoading && (
            <p className="text-sm text-slate-400">
              {t("dashboard.discovering")}
            </p>
          )}
          {disks.isError && (
            <p className="rounded-xl border border-rose-900 bg-rose-950/40 p-4 text-sm text-rose-200">
              {t("dashboard.discoveryFailed")}
            </p>
          )}
          <div className="grid gap-4">
            {disks.data?.map((disk) => {
              const partitions = disk.partitions ?? [];
              return (
                <article
                  key={disk.path}
                  className="rounded-2xl border border-slate-800 bg-slate-900/50 p-6"
                >
                  <div className="flex flex-wrap items-start justify-between gap-4">
                    <div>
                      <div className="flex items-center gap-2">
                        <HardDrive size={18} className="text-cyan-400" />
                        <h3 className="font-medium">
                          {disk.model || disk.name}
                        </h3>
                        {disk.usb && (
                          <span className="inline-flex items-center gap-1 rounded-full bg-cyan-400/10 px-2 py-0.5 text-xs text-cyan-300">
                            <Usb size={12} />
                            USB
                          </span>
                        )}
                      </div>
                      <p className="mt-2 font-mono text-xs text-slate-400">
                        {disk.path}
                        {disk.serial ? ` · ${disk.serial}` : ""}
                      </p>
                    </div>
                    <span
                      className={
                        disk.system
                          ? "flex items-center gap-1.5 text-xs text-rose-300"
                          : disk.protected
                            ? "flex items-center gap-1.5 text-xs text-amber-300"
                            : "text-xs text-emerald-300"
                      }
                    >
                      {(disk.system || disk.protected) && (
                        <ShieldAlert size={14} />
                      )}
                      {disk.system
                        ? t("dashboard.systemDisk")
                        : disk.protected
                          ? t("dashboard.mounted")
                          : t("dashboard.available")}
                    </span>
                  </div>
                  <div className="mt-5 grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
                    <Metric
                      label={t("dashboard.capacity")}
                      value={formatBytes(disk.sizeBytes)}
                    />
                    <Metric
                      label={t("dashboard.transport")}
                      value={disk.transport || t("dashboard.unknown")}
                    />
                    <Metric
                      label={t("dashboard.partitionTable")}
                      value={disk.partitioning || t("dashboard.none")}
                    />
                    <Metric
                      label={t("dashboard.partitions")}
                      value={String(partitions.length)}
                    />
                  </div>
                  {!disk.usb && (
                    <div className="mt-5 flex flex-wrap gap-2">
                      {!disk.protected && disk.reclaimable ? (
                        <DangerButton
                          onClick={() =>
                            confirm(
                              disk.path,
                              t("dashboard.reclaim"),
                              (confirmation) => ({
                                path: "/api/disks/reclaim",
                                body: {
                                  diskPath: disk.path,
                                  confirm: confirmation,
                                },
                              }),
                            )
                          }
                        >
                          {t("dashboard.reclaim")}
                        </DangerButton>
                      ) : (
                        !disk.protected && (
                          <>
                            <DangerButton
                              onClick={() =>
                                confirm(
                                  disk.path,
                                  t("dashboard.initializeGPT"),
                                  (confirmation) => ({
                                    path: "/api/disks/gpt",
                                    body: {
                                      diskPath: disk.path,
                                      confirm: confirmation,
                                    },
                                  }),
                                )
                              }
                            >
                              {t("dashboard.initializeGPT")}
                            </DangerButton>
                            {disk.partitioning === "gpt" && (
                              <ActionButton
                                onClick={() =>
                                  setDialog({
                                    kind: "create",
                                    target: disk.path,
                                    maxGiB: Math.max(
                                      1,
                                      Math.floor(disk.sizeBytes / 1024 ** 3),
                                    ),
                                  })
                                }
                              >
                                <Plus size={15} />{" "}
                                {t("dashboard.createPartition")}
                              </ActionButton>
                            )}
                          </>
                        )
                      )}
                    </div>
                  )}
                  {partitions.length > 0 && (
                    <div className="mt-5 border-t border-slate-800 pt-4">
                      {partitions.map((partition) => {
                        const usedPercent =
                          partition.usage && partition.usage.totalBytes > 0
                            ? Math.min(
                                100,
                                Math.max(
                                  0,
                                  (partition.usage.usedBytes /
                                    partition.usage.totalBytes) *
                                    100,
                                ),
                              )
                            : 0;
                        return (
                          <div
                            key={partition.path}
                            className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-800/70 py-3 last:border-0"
                          >
                            <div className="min-w-0 flex-1">
                              <p className="font-mono text-sm text-slate-300">
                                {partition.path}
                              </p>
                              <p className="mt-1 text-xs text-slate-400">
                                {partition.fileSystem ||
                                  t("dashboard.unformatted")}{" "}
                                · {formatBytes(partition.sizeBytes)}
                                {partition.mountpoints?.length > 0
                                  ? ` · ${partition.mountpoints.join(", ")}`
                                  : ""}
                              </p>
                              {partition.usage && (
                                <div className="mt-3 max-w-md">
                                  <div className="mb-1 flex justify-between text-xs text-slate-400">
                                    <span>
                                      {t("dashboard.spaceUsage", {
                                        used: formatBytes(
                                          partition.usage.usedBytes,
                                        ),
                                        total: formatBytes(
                                          partition.usage.totalBytes,
                                        ),
                                      })}
                                    </span>
                                    <span>{usedPercent.toFixed(0)}%</span>
                                  </div>
                                  <div
                                    className="h-1.5 overflow-hidden rounded-full bg-slate-800"
                                    role="progressbar"
                                    aria-valuenow={usedPercent}
                                    aria-valuemin={0}
                                    aria-valuemax={100}
                                    aria-label={t("dashboard.spaceUsage", {
                                      used: formatBytes(
                                        partition.usage.usedBytes,
                                      ),
                                      total: formatBytes(
                                        partition.usage.totalBytes,
                                      ),
                                    })}
                                  >
                                    <div
                                      className="h-full rounded-full bg-cyan-400"
                                      style={{ width: `${usedPercent}%` }}
                                    />
                                  </div>
                                </div>
                              )}
                            </div>
                            <div className="flex shrink-0 flex-wrap gap-2">
                              {disk.usb ? (
                                <USBPartitionActions
                                  partition={partition}
                                  confirm={confirm}
                                />
                              ) : (
                                <ManagedPartitionActions
                                  diskPath={disk.path}
                                  partition={partition}
                                  system={disk.system}
                                  confirm={confirm}
                                  openFormat={(target) =>
                                    setDialog({ kind: "format", target })
                                  }
                                />
                              )}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </article>
              );
            })}
          </div>
        </section>
      </section>
      {dialog && (
        <OperationDialog
          dialog={dialog}
          pending={operation.isPending}
          onCancel={() => setDialog(null)}
          onSubmit={(next) => operation.mutate(next)}
        />
      )}
      {rebootRequired && (
        <div
          role="status"
          className="fixed bottom-6 right-6 z-[60] max-w-sm rounded-xl border border-amber-700 bg-amber-950/95 p-4 text-sm text-amber-100 shadow-2xl shadow-black/50"
        >
          <div className="flex items-start gap-3">
            <TriangleAlert
              className="mt-0.5 shrink-0 text-amber-300"
              size={18}
            />
            <p className="leading-6">{t("dashboard.rebootRequired")}</p>
            <button
              onClick={() => setRebootRequired(false)}
              className="-mr-1 -mt-1 rounded-md p-1 text-amber-300 hover:bg-amber-900 hover:text-white"
              aria-label={t("dialog.cancel")}
            >
              <X size={17} />
            </button>
          </div>
          <button
            onClick={() =>
              confirm(
                "system",
                t("dashboard.restartSystem"),
                (confirmation) => ({
                  path: "/api/system/reboot",
                  body: { confirm: confirmation },
                }),
              )
            }
            className="mt-3 rounded-lg bg-amber-400 px-3 py-2 text-xs font-medium text-slate-950 hover:bg-amber-300"
          >
            {t("dashboard.restartSystem")}
          </button>
        </div>
      )}
      {operation.isError && (
        <div
          role="alert"
          className="fixed bottom-6 right-6 z-[60] flex max-w-sm items-start gap-3 rounded-xl border border-rose-800 bg-rose-950/95 p-4 text-sm text-rose-100 shadow-2xl shadow-black/50"
        >
          <TriangleAlert className="mt-0.5 shrink-0 text-rose-300" size={18} />
          <p className="leading-6">
            <ErrorText error={operation.error} />
          </p>
          <button
            onClick={() => operation.reset()}
            className="-mr-1 -mt-1 rounded-md p-1 text-rose-300 hover:bg-rose-900 hover:text-white"
            aria-label={t("dialog.cancel")}
          >
            <X size={17} />
          </button>
        </div>
      )}
    </main>
  );
}

function USBPartitionActions({
  partition,
  confirm,
}: {
  partition: { path: string; mountpoints: string[] };
  confirm: (
    target: string,
    title: string,
    next: (confirmation: string) => Operation,
  ) => void;
}) {
  const { t } = useTranslation();
  if (partition.mountpoints?.some((path) => path.startsWith("/usb")))
    return (
      <ActionButton
        onClick={() =>
          confirm(partition.path, t("dashboard.unmount"), (confirmation) => ({
            path: "/api/usb/unmount",
            body: { partitionPath: partition.path, confirm: confirmation },
          }))
        }
      >
        {t("dashboard.unmount")}
      </ActionButton>
    );
  if (partition.mountpoints?.length > 0) return null;
  return (
    <ActionButton
      onClick={() =>
        confirm(partition.path, t("dashboard.mount"), (confirmation) => ({
          path: "/api/usb/mount",
          body: { partitionPath: partition.path, confirm: confirmation },
        }))
      }
    >
      {t("dashboard.mount")}
    </ActionButton>
  );
}

function ManagedPartitionActions({
  diskPath,
  partition,
  system,
  confirm,
  openFormat,
}: {
  diskPath: string;
  partition: {
    path: string;
    number: number;
    uuid: string;
    fileSystem: string;
    mountpoints: string[];
  };
  system: boolean;
  confirm: (
    target: string,
    title: string,
    next: (confirmation: string) => Operation,
  ) => void;
  openFormat: (target: string) => void;
}) {
  const { t } = useTranslation();
  if (
    partition.mountpoints?.some((path) => path.startsWith("/vol")) &&
    partition.uuid
  )
    return (
      <ActionButton
        onClick={() =>
          confirm(partition.uuid, t("dashboard.unmount"), (confirmation) => ({
            path: "/api/volumes/unmount",
            body: { uuid: partition.uuid, confirm: confirmation },
          }))
        }
      >
        {t("dashboard.unmount")}
      </ActionButton>
    );
  if (system || partition.mountpoints?.length > 0) return null;
  return (
    <>
      <>
        {partition.fileSystem && (
          <ActionButton
            onClick={() =>
              confirm(partition.path, t("dashboard.mount"), (confirmation) => ({
                path: "/api/volumes/mount",
                body: { partitionPath: partition.path, confirm: confirmation },
              }))
            }
          >
            {t("dashboard.mount")}
          </ActionButton>
        )}
      </>
      <DangerButton onClick={() => openFormat(partition.path)}>
        {t("dashboard.format")}
      </DangerButton>
      <DangerButton
        onClick={() =>
          confirm(diskPath, t("dashboard.delete"), (confirmation) => ({
            path: "/api/partitions/delete",
            body: {
              diskPath,
              partitionNumber: partition.number,
              confirm: confirmation,
            },
          }))
        }
      >
        {t("dashboard.delete")}
      </DangerButton>
    </>
  );
}

function OperationDialog({
  dialog,
  pending,
  onCancel,
  onSubmit,
}: {
  dialog: Dialog;
  pending: boolean;
  onCancel: () => void;
  onSubmit: (operation: Operation) => void;
}) {
  const { t } = useTranslation();
  const [filesystem, setFilesystem] = useState<"ext4" | "xfs">("ext4");
  const [size, setSize] = useState(() =>
    dialog.kind === "create" ? String(Math.min(100, dialog.maxGiB)) : "100",
  );
  const [name, setName] = useState("");
  const [partitionMode, setPartitionMode] = useState<"largest" | "manual">(
    "largest",
  );
  const [seconds, setSeconds] = useState(5);
  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !pending) onCancel();
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [onCancel, pending]);
  useEffect(() => {
    const timer = window.setInterval(
      () => setSeconds((value) => Math.max(0, value - 1)),
      1000,
    );
    return () => window.clearInterval(timer);
  }, []);

  const title =
    dialog.kind === "create"
      ? t("dashboard.createPartition")
      : dialog.kind === "format"
        ? t("dashboard.format")
        : dialog.title;
  const validSize =
    Number.isFinite(Number(size)) &&
    Number(size) > 0 &&
    (dialog.kind !== "create" || Number(size) <= dialog.maxGiB);
  const canSubmit =
    seconds === 0 &&
    (dialog.kind !== "create" || partitionMode === "largest" || validSize) &&
    !pending;
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!canSubmit) return;
    if (dialog.kind === "format")
      onSubmit({
        path: "/api/partitions/format",
        body: {
          partitionPath: dialog.target,
          fileSystem: filesystem,
          confirm: dialog.target,
        },
      });
    if (dialog.kind === "create")
      onSubmit({
        path: "/api/partitions",
        body: {
          diskPath: dialog.target,
          sizeBytes:
            partitionMode === "largest"
              ? 0
              : Math.round(Number(size) * 1024 ** 3),
          useLargestFree: partitionMode === "largest",
          name,
          confirm: dialog.target,
        },
      });
    if (dialog.kind === "confirm") onSubmit(dialog.operation);
  }

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-slate-950/80 p-4 backdrop-blur-sm"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !pending) onCancel();
      }}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="operation-dialog-title"
        className="w-full max-w-md rounded-2xl border border-slate-700 bg-slate-900 p-6 shadow-2xl shadow-black/60"
      >
        <div className="flex items-start justify-between gap-4">
          <div>
            <p className="text-xs font-medium uppercase tracking-widest text-rose-300">
              {t("dialog.irreversible")}
            </p>
            <h2
              id="operation-dialog-title"
              className="mt-1 text-lg font-semibold"
            >
              {title}
            </h2>
          </div>
          <button
            disabled={pending}
            onClick={onCancel}
            className="rounded-lg p-1 text-slate-400 hover:bg-slate-800 hover:text-slate-100 disabled:opacity-50"
            aria-label={t("dialog.cancel")}
          >
            <X size={20} />
          </button>
        </div>
        <form className="mt-6 space-y-5" onSubmit={submit}>
          <p className="text-sm leading-6 text-slate-400">
            {t("dialog.warning")}
          </p>
          {dialog.kind === "format" && (
            <fieldset>
              <legend className="text-sm font-medium text-slate-200">
                {t("dialog.filesystem")}
              </legend>
              <div className="mt-2 grid grid-cols-2 gap-3">
                {(["ext4", "xfs"] as const).map((value) => (
                  <label
                    key={value}
                    className={`cursor-pointer rounded-lg border px-3 py-3 text-sm ${filesystem === value ? "border-cyan-400 bg-cyan-400/10 text-cyan-200" : "border-slate-700 text-slate-300"}`}
                  >
                    <input
                      className="sr-only"
                      type="radio"
                      value={value}
                      checked={filesystem === value}
                      onChange={() => setFilesystem(value)}
                    />
                    {value}
                  </label>
                ))}
              </div>
            </fieldset>
          )}
          {dialog.kind === "create" && (
            <>
              <fieldset>
                <legend className="text-sm font-medium text-slate-200">
                  {t("dialog.partitionMode")}
                </legend>
                <div className="mt-2 grid gap-3">
                  <label
                    className={`cursor-pointer rounded-lg border px-3 py-3 text-sm ${partitionMode === "largest" ? "border-cyan-400 bg-cyan-400/10 text-cyan-200" : "border-slate-700 text-slate-300"}`}
                  >
                    <input
                      className="sr-only"
                      type="radio"
                      checked={partitionMode === "largest"}
                      onChange={() => setPartitionMode("largest")}
                    />
                    {t("dialog.largestFree")}
                    <span className="mt-1 block text-xs text-slate-400">
                      {t("dialog.largestFreeHint")}
                    </span>
                  </label>
                  <label
                    className={`cursor-pointer rounded-lg border px-3 py-3 text-sm ${partitionMode === "manual" ? "border-cyan-400 bg-cyan-400/10 text-cyan-200" : "border-slate-700 text-slate-300"}`}
                  >
                    <input
                      className="sr-only"
                      type="radio"
                      checked={partitionMode === "manual"}
                      onChange={() => setPartitionMode("manual")}
                    />
                    {t("dialog.manualSize")}
                  </label>
                </div>
              </fieldset>
              {partitionMode === "manual" && (
                <label className="block text-sm text-slate-300">
                  {t("dialog.partitionSize")}
                  <div className="mt-2 flex items-center gap-3">
                    <input
                      className="min-w-0 flex-1 accent-cyan-400"
                      type="range"
                      min="1"
                      max={dialog.maxGiB}
                      step="1"
                      value={Math.min(
                        Math.max(1, Number(size) || 1),
                        dialog.maxGiB,
                      )}
                      onChange={(event) => setSize(event.target.value)}
                      disabled={pending}
                    />
                    <input
                      className="w-28 rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100 outline-none focus:border-cyan-400"
                      type="number"
                      min="1"
                      max={dialog.maxGiB}
                      step="1"
                      value={size}
                      onChange={(event) => setSize(event.target.value)}
                      disabled={pending}
                      required
                    />
                  </div>
                  <span className="mt-1 block text-xs text-slate-500">
                    {t("dialog.maximumSize", { size: dialog.maxGiB })}
                  </span>
                </label>
              )}
              <label className="block text-sm text-slate-300">
                {t("dialog.partitionName")}
                <input
                  className="mt-1.5 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100 outline-none focus:border-cyan-400"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  disabled={pending}
                  maxLength={36}
                />
              </label>
            </>
          )}
          <div className="rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-sm text-cyan-200 break-all">
            {dialog.target}
          </div>
          <div className="flex justify-end gap-3 pt-1">
            <button
              type="button"
              onClick={onCancel}
              disabled={pending}
              className="rounded-lg px-4 py-2 text-sm text-slate-300 hover:bg-slate-800 disabled:opacity-50"
            >
              {t("dialog.cancel")}
            </button>
            <button
              type="submit"
              disabled={!canSubmit}
              className="rounded-lg bg-rose-500 px-4 py-2 text-sm font-medium text-white hover:bg-rose-400 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {pending
                ? t("dialog.processing")
                : seconds > 0
                  ? t("dialog.confirmIn", { seconds })
                  : title}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}

function LanguageSelect({
  value,
  onChange,
}: {
  value?: string;
  onChange: (language: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <label className="text-xs text-slate-400">
      <span className="sr-only">{t("language.label")}</span>
      <select
        className="rounded-md border border-slate-700 bg-slate-900 px-2 py-1 text-slate-200"
        value={value === "zh-CN" ? "zh-CN" : "en"}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="zh-CN">{t("language.chinese")}</option>
        <option value="en">{t("language.english")}</option>
      </select>
    </label>
  );
}
function ErrorText({ error }: { error: Error }) {
  const { t } = useTranslation();
  const code = error instanceof ApiError ? error.code : "internal_error";
  return (
    <>{t(`errors.${code}`, { defaultValue: t("errors.internal_error") })}</>
  );
}
function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-slate-500">{label}</p>
      <p className="mt-1 text-slate-200">{value}</p>
    </div>
  );
}
function ActionButton({
  children,
  onClick,
}: {
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="inline-flex items-center gap-1.5 rounded-lg border border-slate-700 px-3 py-1.5 text-xs text-slate-200 hover:border-cyan-400 hover:text-cyan-300"
    >
      {children}
    </button>
  );
}
function DangerButton({
  children,
  onClick,
}: {
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className="inline-flex items-center gap-1.5 rounded-lg border border-rose-900/80 px-3 py-1.5 text-xs text-rose-300 hover:bg-rose-950/50"
    >
      <TriangleAlert size={14} />
      {children}
    </button>
  );
}
async function request(
  path: string,
  body: Record<string, unknown>,
): Promise<OperationResult> {
  const response = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    const error = (await response.json().catch(() => null)) as {
      code?: string;
      detail?: string;
    } | null;
    throw new ApiError(error?.code ?? error?.detail ?? "request_failed");
  }
  return response.json();
}
function formatBytes(bytes: number) {
  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  let value = bytes;
  let index = 0;
  while (value > 800 && index < units.length - 1) {
    value /= 1024;
    index++;
  }
  const rounded = Math.round(value * 100) / 100;
  return `${rounded.toFixed(Math.abs(Math.trunc(rounded)) >= 100 ? 0 : 2)} ${units[index]}`;
}
