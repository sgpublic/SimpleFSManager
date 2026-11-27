import { useTranslation } from "react-i18next";
import type { Disk, Operation, Partition } from "../lib/api";
import { formatBytes } from "../lib/format";
import { ActionButton, DangerButton } from "./Buttons";
import type { DialogTone, TargetDetail } from "./OperationDialog";

export type ConfirmOptions = {
  tone?: DialogTone;
  delaySeconds?: number;
  description?: string;
  details?: TargetDetail[];
};

export type ConfirmFn = (
  target: string,
  title: string,
  next: (confirmation: string) => Operation,
  options?: ConfirmOptions,
) => void;

export function buildPartitionDetails(
  disk: Disk,
  partition: Partition,
  t: (key: string) => string,
): TargetDetail[] {
  const details: TargetDetail[] = [];
  if (disk.model || disk.name) {
    details.push({
      label: t("dashboard.smartModel"),
      value: disk.model || disk.name,
    });
  }
  if (disk.serial) {
    details.push({
      label: t("dashboard.smartSerial"),
      value: disk.serial,
      mono: true,
    });
  }
  if (partition.sizeBytes > 0) {
    details.push({
      label: t("dashboard.capacity"),
      value: formatBytes(partition.sizeBytes),
    });
  }
  if (partition.fileSystem) {
    details.push({
      label: t("dialog.filesystem"),
      value: partition.fileSystem,
      mono: true,
    });
  }
  if (partition.mountPath) {
    details.push({
      label: t("dialog.mountPath"),
      value: partition.mountPath,
      mono: true,
    });
  }
  return details;
}

export function PartitionRow({
  disk,
  partition,
  confirm,
  openMountPath,
  openFormat,
  onToggleAutoMount,
  autoMountPending,
}: {
  disk: Disk;
  partition: Partition;
  confirm: ConfirmFn;
  openMountPath: (
    target: string,
    currentPath: string,
    currentAutoMount: boolean,
    missing: boolean,
    details: TargetDetail[],
  ) => void;
  openFormat: (target: string, details: TargetDetail[]) => void;
  onToggleAutoMount: (uuid: string, enabled: boolean) => void;
  autoMountPending: boolean;
}) {
  const { t } = useTranslation();
  const details = buildPartitionDetails(disk, partition, t);
  const usedPercent =
    partition.usage && partition.usage.totalBytes > 0
      ? Math.min(
          100,
          Math.max(
            0,
            (partition.usage.usedBytes / partition.usage.totalBytes) * 100,
          ),
        )
      : 0;

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-800/70 py-3 last:border-0">
      <div className="min-w-0 flex-1">
        <p className="font-mono text-sm text-slate-300">
          {partition.path || partition.uuid}
        </p>
        <p className="mt-1 text-xs text-slate-400">
          {partition.fileSystem || t("dashboard.unformatted")} ·{" "}
          {formatBytes(partition.sizeBytes)}
          {partition.mountPath ? ` · ${partition.mountPath}` : ""}
          {partition.mountpoints?.length > 0
            ? ` · ${partition.mountpoints.join(", ")}`
            : ""}
        </p>
        {partition.usage && (
          <div className="mt-3 max-w-md">
            <div className="mb-1 flex justify-between text-xs text-slate-400">
              <span>
                {t("dashboard.spaceUsage", {
                  used: formatBytes(partition.usage.usedBytes),
                  total: formatBytes(partition.usage.totalBytes),
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
                used: formatBytes(partition.usage.usedBytes),
                total: formatBytes(partition.usage.totalBytes),
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
      <div className="flex shrink-0 flex-wrap items-center gap-2">
        {!disk.usb && partition.registered && partition.uuid && (
          <AutoMountToggle
            checked={Boolean(partition.autoMount)}
            pending={autoMountPending}
            onChange={(enabled) => onToggleAutoMount(partition.uuid, enabled)}
          />
        )}
        {disk.usb ? (
          <USBPartitionActions
            partition={partition}
            details={details}
            confirm={confirm}
          />
        ) : (
          <ManagedPartitionActions
            diskPath={disk.path}
            partition={partition}
            details={details}
            system={disk.system}
            confirm={confirm}
            openMountPath={(target, currentPath) =>
              openMountPath(
                target,
                currentPath,
                Boolean(partition.autoMount),
                partition.missing,
                details,
              )
            }
            openFormat={(target) => openFormat(target, details)}
          />
        )}
      </div>
    </div>
  );
}

function AutoMountToggle({
  checked,
  pending,
  onChange,
}: {
  checked: boolean;
  pending: boolean;
  onChange: (enabled: boolean) => void;
}) {
  const { t } = useTranslation();
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={pending}
      onClick={() => onChange(!checked)}
      className="inline-flex items-center gap-2 rounded-lg border border-slate-700 px-2.5 py-1.5 text-xs text-slate-300 hover:border-cyan-400 hover:text-cyan-200 disabled:cursor-not-allowed disabled:opacity-50"
    >
      <span
        className={`relative inline-flex h-4 w-7 shrink-0 items-center rounded-full transition-colors ${
          checked ? "bg-cyan-400" : "bg-slate-700"
        }`}
      >
        <span
          className={`inline-block h-3 w-3 rounded-full bg-slate-950 transition-transform ${
            checked ? "translate-x-3.5" : "translate-x-0.5"
          }`}
        />
      </span>
      <span>{t("dashboard.autoMount")}</span>
    </button>
  );
}

function USBPartitionActions({
  partition,
  details,
  confirm,
}: {
  partition: Partition;
  details: TargetDetail[];
  confirm: ConfirmFn;
}) {
  const { t } = useTranslation();
  if (partition.mountpoints?.some((path) => path.startsWith("/usb"))) {
    return (
      <ActionButton
        onClick={() =>
          confirm(
            partition.path,
            t("dashboard.unmount"),
            (confirmation) => ({
              path: "/api/usb/unmount",
              body: { partitionPath: partition.path, confirm: confirmation },
            }),
            { tone: "normal", delaySeconds: 0, details },
          )
        }
      >
        {t("dashboard.unmount")}
      </ActionButton>
    );
  }
  if (partition.mountpoints?.length > 0) return null;
  return (
    <ActionButton
      onClick={() =>
        confirm(
          partition.path,
          t("dashboard.mount"),
          (confirmation) => ({
            path: "/api/usb/mount",
            body: { partitionPath: partition.path, confirm: confirmation },
          }),
          { tone: "normal", delaySeconds: 0, details },
        )
      }
    >
      {t("dashboard.mount")}
    </ActionButton>
  );
}

function ManagedPartitionActions({
  diskPath,
  partition,
  details,
  system,
  confirm,
  openMountPath,
  openFormat,
}: {
  diskPath: string;
  partition: Partition;
  details: TargetDetail[];
  system: boolean;
  confirm: ConfirmFn;
  openMountPath: (target: string, currentPath: string) => void;
  openFormat: (target: string) => void;
}) {
  const { t } = useTranslation();
  if (partition.missing) {
    return (
      <ActionButton
        onClick={() =>
          openMountPath(partition.uuid, partition.mountPath ?? "")
        }
      >
        {t("dashboard.modifyMountPath")}
      </ActionButton>
    );
  }
  if (partition.mountpoints?.length > 0) {
    return partition.mountPath &&
      partition.mountpoints.includes(partition.mountPath) ? (
      <ActionButton
        onClick={() =>
          confirm(
            partition.uuid,
            t("dashboard.unmount"),
            (confirmation) => ({
              path: "/api/volumes/unmount",
              body: { uuid: partition.uuid, confirm: confirmation },
            }),
            { tone: "normal", delaySeconds: 0, details },
          )
        }
      >
        {t("dashboard.unmount")}
      </ActionButton>
    ) : null;
  }
  if (system) return null;

  return (
    <>
      {partition.fileSystem && partition.mountPath && (
        <ActionButton
          onClick={() =>
            confirm(
              partition.path,
              t("dashboard.mount"),
              (confirmation) => ({
                path: "/api/volumes/mount",
                body: { partitionPath: partition.path, confirm: confirmation },
              }),
              { tone: "normal", delaySeconds: 0, details },
            )
          }
        >
          {t("dashboard.mount")}
        </ActionButton>
      )}
      {partition.fileSystem && (
        <ActionButton
          onClick={() =>
            openMountPath(partition.path, partition.mountPath ?? "")
          }
        >
          {t("dashboard.modifyMountPath")}
        </ActionButton>
      )}
      {partition.number > 0 && (
        <DangerButton onClick={() => openFormat(partition.path)}>
          {t("dashboard.format")}
        </DangerButton>
      )}
      {partition.number > 0 && (
        <DangerButton
          onClick={() =>
            confirm(
              diskPath,
              t("dashboard.delete"),
              (confirmation) => ({
                path: "/api/partitions/delete",
                body: {
                  diskPath,
                  partitionNumber: partition.number,
                  confirm: confirmation,
                },
              }),
              {
                tone: "danger",
                delaySeconds: 3,
                description: t("dialog.warningDeletePartition"),
                details: [
                  {
                    label: t("dashboard.partitions"),
                    value: `${partition.path} (#${partition.number})`,
                    mono: true,
                  },
                  ...details,
                ],
              },
            )
          }
        >
          {t("dashboard.delete")}
        </DangerButton>
      )}
    </>
  );
}
