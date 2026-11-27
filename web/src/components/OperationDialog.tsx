import { Pause, X } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { FileSystemType, Operation } from "../lib/api";
import { formatBytes } from "../lib/format";

export type DialogTone = "normal" | "warning" | "danger";

export type TargetDetail = {
  label: string;
  value: string;
  mono?: boolean;
};

export type ConfirmDialog = {
  kind: "confirm";
  title: string;
  target: string;
  tone: DialogTone;
  delaySeconds: number;
  description: string;
  details: TargetDetail[];
  operation: Operation;
};

export type FormatDialog = {
  kind: "format";
  target: string;
  details: TargetDetail[];
};

export type WholeDiskFormatDialog = {
  kind: "formatDisk";
  target: string;
  details: TargetDetail[];
};

export type MountPathDialog = {
  kind: "mountPath";
  target: string;
  currentPath: string;
  currentAutoMount: boolean;
  missing: boolean;
  details: TargetDetail[];
};

export type CreateDialog = {
  kind: "create";
  target: string;
  diskSizeBytes: number;
  maxGB: number;
  zoneSizeBytes?: number;
  details: TargetDetail[];
};

export type DialogState =
  | ConfirmDialog
  | FormatDialog
  | WholeDiskFormatDialog
  | MountPathDialog
  | CreateDialog;

function dialogTone(dialog: DialogState): DialogTone {
  if (dialog.kind === "format" || dialog.kind === "formatDisk") return "danger";
  if (dialog.kind === "confirm") return dialog.tone;
  return "normal";
}

function dialogDelayMs(dialog: DialogState): number {
  if (dialog.kind === "formatDisk") return 5000;
  if (dialog.kind === "format") return 3000;
  if (dialog.kind === "confirm") return Math.max(0, dialog.delaySeconds) * 1000;
  return 0;
}

function usePauseableCountdown(dialog: DialogState, delayMs: number) {
  const [remainingMs, setRemainingMs] = useState(delayMs);
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    if (delayMs <= 0) {
      setRemainingMs(0);
      setPaused(false);
      return;
    }

    let cancelled = false;
    let rafId = 0;
    let elapsed = 0;
    let isFocused = !document.hidden && document.hasFocus();
    let lastTick = performance.now();

    setRemainingMs(delayMs);
    setPaused(!isFocused);

    const step = (now: number) => {
      if (cancelled) return;
      if (isFocused) {
        elapsed += Math.max(0, now - lastTick);
        const nextRemaining = Math.max(0, delayMs - elapsed);
        setRemainingMs(nextRemaining);
        if (nextRemaining <= 0) {
          setPaused(false);
          return;
        }
      }
      lastTick = now;
      rafId = window.requestAnimationFrame(step);
    };

    const syncFocus = () => {
      const now = performance.now();
      const focusedNow = !document.hidden && document.hasFocus();
      if (focusedNow === isFocused) return;
      if (focusedNow) {
        lastTick = now;
      } else {
        elapsed += Math.max(0, now - lastTick);
        setRemainingMs(Math.max(0, delayMs - elapsed));
      }
      isFocused = focusedNow;
      setPaused(!isFocused);
    };

    window.addEventListener("focus", syncFocus);
    window.addEventListener("blur", syncFocus);
    document.addEventListener("visibilitychange", syncFocus);
    rafId = window.requestAnimationFrame(step);

    return () => {
      cancelled = true;
      window.cancelAnimationFrame(rafId);
      window.removeEventListener("focus", syncFocus);
      window.removeEventListener("blur", syncFocus);
      document.removeEventListener("visibilitychange", syncFocus);
    };
  }, [dialog, delayMs]);

  const progress =
    delayMs > 0 ? Math.min(1, Math.max(0, 1 - remainingMs / delayMs)) : 1;
  const remainingSeconds = Math.ceil(remainingMs / 1000);

  return {
    cooling: remainingMs > 0,
    paused: remainingMs > 0 && paused,
    progress,
    remainingSeconds,
  };
}

export function OperationDialog({
  dialog,
  pending,
  onCancel,
  onSubmit,
}: {
  dialog: DialogState;
  pending: boolean;
  onCancel: () => void;
  onSubmit: (operation: Operation) => void;
}) {
  const { t } = useTranslation();
  const tone = dialogTone(dialog);
  const delayMs = dialogDelayMs(dialog);
  const { cooling, paused, progress, remainingSeconds } = usePauseableCountdown(
    dialog,
    delayMs,
  );
  const cancelButtonRef = useRef<HTMLButtonElement | null>(null);

  const isZonedCreate =
    dialog.kind === "create" &&
    Boolean(dialog.zoneSizeBytes && dialog.zoneSizeBytes > 0);
  const maxZones =
    dialog.kind === "create" && dialog.zoneSizeBytes && dialog.zoneSizeBytes > 0
      ? Math.max(1, Math.floor(dialog.diskSizeBytes / dialog.zoneSizeBytes))
      : 1;

  const [filesystem, setFilesystem] = useState<FileSystemType>(
    dialog.kind === "formatDisk" ? "f2fs" : "ext4",
  );
  const [sizeGB, setSizeGB] = useState(() =>
    dialog.kind === "create" ? String(Math.min(100, dialog.maxGB)) : "100",
  );
  const [zoneCount, setZoneCount] = useState("1");
  const [name, setName] = useState("");
  const [partitionMode, setPartitionMode] = useState<"largest" | "manual">(
    "largest",
  );
  const [mountPath, setMountPath] = useState(
    dialog.kind === "mountPath" ? dialog.currentPath : "",
  );
  const [autoMount, setAutoMount] = useState(
    dialog.kind === "mountPath" ? dialog.currentAutoMount : false,
  );

  useEffect(() => {
    if (dialog.kind === "formatDisk") {
      setFilesystem("f2fs");
    } else if (dialog.kind === "format") {
      setFilesystem("ext4");
    }
    if (dialog.kind === "mountPath") {
      setMountPath(dialog.currentPath);
      setAutoMount(dialog.currentAutoMount);
    }
    if (dialog.kind === "create") {
      setSizeGB(String(Math.min(100, dialog.maxGB)));
      setZoneCount("1");
      setPartitionMode("largest");
      setName("");
    }
    // Keep keyboard focus on Cancel when opening delayed/high-risk dialogs so
    // an accidental Enter cannot trigger the action.
    if (dialogDelayMs(dialog) > 0) {
      cancelButtonRef.current?.focus();
    }
  }, [dialog]);

  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !pending) onCancel();
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [onCancel, pending]);

  const title =
    dialog.kind === "create"
      ? t("dashboard.createPartition")
      : dialog.kind === "format" || dialog.kind === "formatDisk"
        ? t("dashboard.format")
        : dialog.kind === "mountPath"
          ? t("dashboard.modifyMountPath")
          : dialog.title;

  const badgeText =
    tone === "danger"
      ? t("dialog.irreversible")
      : tone === "warning"
        ? t("dialog.systemAction")
        : t("dialog.confirmOperation");

  const badgeColor =
    tone === "danger"
      ? "text-rose-300"
      : tone === "warning"
        ? "text-amber-300"
        : "text-cyan-300";

  const description =
    dialog.kind === "formatDisk"
      ? t("dialog.warningWholeDisk")
      : dialog.kind === "format"
        ? t("dialog.warningFormatPartition")
        : dialog.kind === "confirm"
          ? dialog.description
          : t("dialog.confirmHint");

  const parsedZones = Number(zoneCount);
  const validZonedSize =
    Number.isInteger(parsedZones) &&
    parsedZones >= 1 &&
    parsedZones <= maxZones;

  const parsedGB = Number(sizeGB);
  const validRegularSize =
    Number.isFinite(parsedGB) &&
    parsedGB > 0 &&
    (dialog.kind !== "create" || parsedGB <= dialog.maxGB);

  const validManualSize = isZonedCreate ? validZonedSize : validRegularSize;

  const formValid =
    (dialog.kind !== "create" ||
      partitionMode === "largest" ||
      validManualSize) &&
    (dialog.kind !== "mountPath" || mountPath.trim().length > 0);

  const canSubmit = !cooling && formValid && !pending;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!canSubmit) return;

    if (dialog.kind === "format") {
      onSubmit({
        path: "/api/partitions/format",
        body: {
          partitionPath: dialog.target,
          fileSystem: filesystem,
          confirm: dialog.target,
        },
      });
      return;
    }
    if (dialog.kind === "formatDisk") {
      onSubmit({
        path: "/api/disks/format-f2fs",
        body: { diskPath: dialog.target, confirm: dialog.target },
      });
      return;
    }
    if (dialog.kind === "create") {
      const sizeBytes =
        partitionMode === "largest"
          ? 0
          : isZonedCreate && dialog.zoneSizeBytes
            ? parsedZones * dialog.zoneSizeBytes
            : Math.round(parsedGB * 1000 ** 3);
      onSubmit({
        path: "/api/partitions",
        body: {
          diskPath: dialog.target,
          sizeBytes,
          useLargestFree: partitionMode === "largest",
          name,
          confirm: dialog.target,
        },
      });
      return;
    }
    if (dialog.kind === "mountPath") {
      onSubmit({
        path: "/api/volumes/mount-path",
        body: {
          ...(dialog.missing
            ? { partitionUUID: dialog.target }
            : { partitionPath: dialog.target }),
          mountPath: mountPath.trim(),
          autoMount,
          confirm: dialog.target,
        },
      });
      return;
    }
    onSubmit(dialog.operation);
  }

  const submitButtonClass = cooling
    ? tone === "warning"
      ? "relative overflow-hidden rounded-lg border border-amber-500/40 bg-amber-950/60 px-4 py-2 text-sm font-medium tabular-nums text-amber-200 cursor-not-allowed"
      : "relative overflow-hidden rounded-lg border border-rose-500/40 bg-rose-950/60 px-4 py-2 text-sm font-medium tabular-nums text-rose-200 cursor-not-allowed"
    : tone === "danger"
      ? "relative overflow-hidden rounded-lg bg-rose-500 px-4 py-2 text-sm font-medium text-white hover:bg-rose-400 disabled:cursor-not-allowed disabled:opacity-50"
      : tone === "warning"
        ? "relative overflow-hidden rounded-lg bg-amber-400 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-amber-300 disabled:cursor-not-allowed disabled:opacity-50"
        : "relative overflow-hidden rounded-lg bg-cyan-400 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-cyan-300 disabled:cursor-not-allowed disabled:opacity-50";

  const progressFillClass =
    tone === "warning"
      ? "pointer-events-none absolute inset-y-0 left-0 bg-amber-400/35"
      : "pointer-events-none absolute inset-y-0 left-0 bg-rose-500/45";

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
            <p
              className={`text-xs font-medium uppercase tracking-widest ${badgeColor}`}
            >
              {badgeText}
            </p>
            <h2
              id="operation-dialog-title"
              className="mt-1 text-lg font-semibold"
            >
              {title}
            </h2>
          </div>
          <button
            type="button"
            disabled={pending}
            onClick={onCancel}
            className="rounded-lg p-1 text-slate-400 hover:bg-slate-800 hover:text-slate-100 disabled:opacity-50"
            aria-label={t("dialog.cancel")}
          >
            <X size={20} />
          </button>
        </div>

        <form className="mt-5 space-y-5" onSubmit={submit}>
          <p className="text-sm leading-6 text-slate-300">{description}</p>

          <dl className="space-y-2 rounded-xl border border-slate-800 bg-slate-950/90 p-3.5 text-xs">
            <div className="flex items-baseline justify-between gap-4">
              <dt className="shrink-0 text-slate-500">
                {t("dialog.targetDevice")}
              </dt>
              <dd className="break-all text-right font-mono text-sm text-cyan-200">
                {dialog.target}
              </dd>
            </div>
            {dialog.details.map((item) => (
              <div
                key={item.label}
                className="flex items-baseline justify-between gap-4 border-t border-slate-800/70 pt-2"
              >
                <dt className="shrink-0 text-slate-500">{item.label}</dt>
                <dd
                  className={`break-all text-right text-slate-200 ${item.mono ? "font-mono" : ""}`}
                >
                  {item.value}
                </dd>
              </div>
            ))}
          </dl>

          {dialog.kind === "mountPath" && (
            <>
              <label className="block text-sm text-slate-300">
                {t("dialog.mountPath")}
                <input
                  className="mt-1.5 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 font-mono text-slate-100 outline-none focus:border-cyan-400"
                  value={mountPath}
                  onChange={(event) => setMountPath(event.target.value)}
                  disabled={pending}
                  placeholder="/mnt/data"
                  required
                />
                <span className="mt-1 block text-xs text-slate-500">
                  {t("dialog.mountPathHint")}
                </span>
              </label>
              <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-slate-800 bg-slate-950/60 p-3 text-sm text-slate-200">
                <input
                  type="checkbox"
                  checked={autoMount}
                  onChange={(event) => setAutoMount(event.target.checked)}
                  disabled={pending}
                  className="mt-0.5 h-4 w-4 rounded border-slate-600 accent-cyan-400"
                />
                <span>
                  <span className="block font-medium">
                    {t("dialog.autoMountLabel")}
                  </span>
                  <span className="mt-0.5 block text-xs text-slate-500">
                    {t("dialog.autoMountHint")}
                  </span>
                </span>
              </label>
            </>
          )}

          {(dialog.kind === "format" || dialog.kind === "formatDisk") && (
            <fieldset>
              <legend className="text-sm font-medium text-slate-200">
                {t("dialog.filesystem")}
              </legend>
              <div className="mt-2 grid grid-cols-2 gap-3">
                {(dialog.kind === "formatDisk"
                  ? (["f2fs"] as const)
                  : (["ext4", "xfs", "btrfs", "f2fs"] as const)
                ).map((value) => (
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

              {partitionMode === "manual" &&
                (isZonedCreate && dialog.zoneSizeBytes ? (
                  <label className="block text-sm text-slate-300">
                    {t("dialog.zoneCount")}
                    <div className="mt-2 flex items-center gap-3">
                      <input
                        className="min-w-0 flex-1 accent-cyan-400"
                        type="range"
                        min="1"
                        max={maxZones}
                        step="1"
                        value={Math.min(
                          Math.max(1, Math.trunc(parsedZones) || 1),
                          maxZones,
                        )}
                        onChange={(event) => setZoneCount(event.target.value)}
                        disabled={pending}
                      />
                      <input
                        className="w-28 rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100 outline-none focus:border-cyan-400"
                        type="number"
                        min="1"
                        max={maxZones}
                        step="1"
                        value={zoneCount}
                        onChange={(event) => setZoneCount(event.target.value)}
                        disabled={pending}
                        required
                      />
                    </div>
                    <span className="mt-1 block text-xs text-slate-500">
                      {t("dialog.calculatedSize", {
                        size: formatBytes(
                          Math.max(1, Math.trunc(parsedZones) || 1) *
                            dialog.zoneSizeBytes,
                        ),
                        max: maxZones,
                      })}
                    </span>
                  </label>
                ) : (
                  <label className="block text-sm text-slate-300">
                    {t("dialog.partitionSize")}
                    <div className="mt-2 flex items-center gap-3">
                      <input
                        className="min-w-0 flex-1 accent-cyan-400"
                        type="range"
                        min="1"
                        max={dialog.maxGB}
                        step="1"
                        value={Math.min(
                          Math.max(1, parsedGB || 1),
                          dialog.maxGB,
                        )}
                        onChange={(event) => setSizeGB(event.target.value)}
                        disabled={pending}
                      />
                      <input
                        className="w-28 rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100 outline-none focus:border-cyan-400"
                        type="number"
                        min="1"
                        max={dialog.maxGB}
                        step="1"
                        value={sizeGB}
                        onChange={(event) => setSizeGB(event.target.value)}
                        disabled={pending}
                        required
                      />
                    </div>
                    <span className="mt-1 block text-xs text-slate-500">
                      {t("dialog.maximumSize", { size: dialog.maxGB })}
                    </span>
                  </label>
                ))}

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

              {dialog.zoneSizeBytes && dialog.zoneSizeBytes > 0 && (
                <p className="text-xs leading-5 text-amber-300">
                  {t("dashboard.zonedPartitionHint", {
                    size: formatBytes(dialog.zoneSizeBytes),
                  })}
                </p>
              )}
            </>
          )}

          {paused && (
            <p className="flex items-center gap-1.5 text-xs text-amber-300">
              <Pause size={13} className="shrink-0" />
              {t("dialog.pausedHint")}
            </p>
          )}

          <div className="flex justify-end gap-3 pt-1">
            <button
              ref={cancelButtonRef}
              type="button"
              onClick={onCancel}
              disabled={pending}
              className="rounded-lg px-4 py-2 text-sm text-slate-300 hover:bg-slate-800 focus:outline-none focus:ring-2 focus:ring-slate-600 disabled:opacity-50"
            >
              {t("dialog.cancel")}
            </button>
            <button
              type="submit"
              disabled={!canSubmit}
              className={submitButtonClass}
            >
              {cooling && (
                <span
                  aria-hidden="true"
                  className={progressFillClass}
                  style={{ width: `${progress * 100}%` }}
                />
              )}
              <span className="relative z-10 inline-flex items-center gap-1.5">
                {pending
                  ? t("dialog.processing")
                  : cooling
                    ? paused
                      ? t("dialog.paused", { seconds: remainingSeconds })
                      : t("dialog.confirmIn", { seconds: remainingSeconds })
                    : title}
              </span>
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
