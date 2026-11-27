import { CircleAlert, HardDrive, Plus, ShieldAlert, Usb } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { Disk } from "../lib/api";
import {
  formatBytes,
  formatHoursTooltip,
  formatPowerOnDuration,
} from "../lib/format";
import { ActionButton, DangerButton } from "./Buttons";
import type { TargetDetail } from "./OperationDialog";
import { PartitionRow, type ConfirmFn } from "./PartitionRow";

function buildDiskDetails(
  disk: Disk,
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
  details.push({
    label: t("dashboard.capacity"),
    value: formatBytes(disk.sizeBytes),
  });
  if (disk.partitions?.length > 0) {
    details.push({
      label: t("dashboard.partitions"),
      value: String(disk.partitions.length),
    });
  }
  return details;
}

export function DiskCard({
  disk,
  confirm,
  onOpenSmart,
  onOpenCreatePartition,
  onOpenFormatWholeDisk,
  onOpenFormatPartition,
  onOpenMountPath,
  onToggleAutoMount,
  pendingAutoMountUUID,
}: {
  disk: Disk;
  confirm: ConfirmFn;
  onOpenSmart: (disk: Disk) => void;
  onOpenCreatePartition: (disk: Disk, details: TargetDetail[]) => void;
  onOpenFormatWholeDisk: (diskPath: string, details: TargetDetail[]) => void;
  onOpenFormatPartition: (partitionPath: string, details: TargetDetail[]) => void;
  onOpenMountPath: (
    target: string,
    currentPath: string,
    currentAutoMount: boolean,
    missing: boolean,
    details: TargetDetail[],
  ) => void;
  onToggleAutoMount: (uuid: string, enabled: boolean) => void;
  pendingAutoMountUUID: string | null;
}) {
  const { t } = useTranslation();
  const partitions = disk.partitions ?? [];
  const canInspectSmart = !disk.missing && Boolean(disk.path);
  const diskDetails = buildDiskDetails(disk, t);

  return (
    <article className="rounded-2xl border border-slate-800 bg-slate-900/50 p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <HardDrive size={18} className="text-cyan-400" />
            <h3 className="font-medium">{disk.model || disk.name}</h3>
            {disk.usb && (
              <span className="inline-flex items-center gap-1 rounded-full bg-cyan-400/10 px-2 py-0.5 text-xs text-cyan-300">
                <Usb size={12} />
                USB
              </span>
            )}
          </div>
          <p className="mt-2 font-mono text-xs text-slate-400">
            {disk.path || t("dashboard.registeredVolume")}
            {disk.serial ? ` · ${disk.serial}` : ""}
          </p>
        </div>
        <span
          className={
            disk.missing
              ? "flex items-center gap-1.5 text-xs text-amber-300"
              : disk.system
                ? "flex items-center gap-1.5 text-xs text-rose-300"
                : disk.protected
                  ? "flex items-center gap-1.5 text-xs text-amber-300"
                  : "text-xs text-emerald-300"
          }
        >
          {(disk.missing || disk.system || disk.protected) && (
            <ShieldAlert size={14} />
          )}
          {disk.missing
            ? t("dashboard.missing")
            : disk.system
              ? t("dashboard.systemDisk")
              : disk.protected
                ? t("dashboard.mounted")
                : t("dashboard.available")}
        </span>
      </div>

      <div className="mt-5 grid grid-cols-2 gap-4 text-sm sm:hidden">
        <Metric
          label={t("dashboard.capacity")}
          value={formatBytes(disk.sizeBytes)}
        />
        <Metric
          label={t("dashboard.transport")}
          value={disk.transport || t("dashboard.unknown")}
        />
        <TemperatureMetric temperature={disk.temperatureCelsius} />
        <SmartMetric
          health={disk.smartHealth}
          solidState={disk.solidState}
          dataWrittenBytes={disk.dataWrittenBytes}
          powerOnHours={disk.powerOnHours}
          onDetails={canInspectSmart ? () => onOpenSmart(disk) : undefined}
        />
      </div>

      <details className="mt-4 sm:hidden">
        <summary className="cursor-pointer text-sm text-cyan-300 hover:text-cyan-200">
          {t("dashboard.showMore")}
        </summary>
        <div className="mt-4 grid grid-cols-2 gap-4 text-sm">
          <Metric
            label={t("dashboard.partitionTable")}
            value={disk.partitioning || t("dashboard.none")}
          />
          {disk.zoned && disk.zoned !== "none" && (
            <Metric
              label={t("dashboard.zoned")}
              value={`${disk.zoned} · ${formatBytes(disk.zoneSizeBytes ?? 0)}`}
            />
          )}
          <Metric
            label={t("dashboard.partitions")}
            value={String(partitions.length)}
          />
        </div>
      </details>

      <div className="mt-5 hidden grid-cols-7 gap-4 text-sm sm:grid">
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
          label={t("dashboard.zoned")}
          value={
            disk.zoned && disk.zoned !== "none"
              ? `${disk.zoned} · ${formatBytes(disk.zoneSizeBytes ?? 0)}`
              : t("dashboard.none")
          }
        />
        <Metric
          label={t("dashboard.partitions")}
          value={String(partitions.length)}
        />
        <TemperatureMetric temperature={disk.temperatureCelsius} />
        <SmartMetric
          health={disk.smartHealth}
          solidState={disk.solidState}
          dataWrittenBytes={disk.dataWrittenBytes}
          powerOnHours={disk.powerOnHours}
          onDetails={canInspectSmart ? () => onOpenSmart(disk) : undefined}
        />
      </div>

      {!disk.usb && !disk.missing && !disk.protected && (
        <div className="mt-5 flex flex-wrap gap-2">
          {disk.reclaimable ? (
            <DangerButton
              onClick={() =>
                confirm(
                  disk.path,
                  t("dashboard.reclaim"),
                  (confirmation) => ({
                    path: "/api/disks/reclaim",
                    body: { diskPath: disk.path, confirm: confirmation },
                  }),
                  {
                    tone: "danger",
                    delaySeconds: 5,
                    description: t("dialog.warningWholeDisk"),
                    details: diskDetails,
                  },
                )
              }
            >
              {t("dashboard.reclaim")}
            </DangerButton>
          ) : disk.zoned === "host-managed" ? (
            <DangerButton
              onClick={() => onOpenFormatWholeDisk(disk.path, diskDetails)}
            >
              {t("dashboard.formatWholeDisk")}
            </DangerButton>
          ) : (
            <>
              <DangerButton
                onClick={() =>
                  confirm(
                    disk.path,
                    t("dashboard.initializeGPT"),
                    (confirmation) => ({
                      path: "/api/disks/gpt",
                      body: { diskPath: disk.path, confirm: confirmation },
                    }),
                    {
                      tone: "danger",
                      delaySeconds: 5,
                      description: t("dialog.warningWholeDisk"),
                      details: diskDetails,
                    },
                  )
                }
              >
                {t("dashboard.initializeGPT")}
              </DangerButton>
              {disk.partitioning === "gpt" && (
                <ActionButton
                  onClick={() => onOpenCreatePartition(disk, diskDetails)}
                >
                  <Plus size={15} /> {t("dashboard.createPartition")}
                </ActionButton>
              )}
            </>
          )}
        </div>
      )}

      {partitions.length > 0 && (
        <div className="mt-5 border-t border-slate-800 pt-4">
          {partitions.map((partition) => (
            <PartitionRow
              key={partition.path || partition.uuid || `${partition.number}`}
              disk={disk}
              partition={partition}
              confirm={confirm}
              openMountPath={onOpenMountPath}
              openFormat={onOpenFormatPartition}
              onToggleAutoMount={onToggleAutoMount}
              autoMountPending={
                Boolean(partition.uuid) &&
                pendingAutoMountUUID === partition.uuid
              }
            />
          ))}
        </div>
      )}
    </article>
  );
}

function Metric({
  label,
  value,
  valueClassName = "text-slate-200",
}: {
  label: string;
  value: string;
  valueClassName?: string;
}) {
  return (
    <div>
      <p className="text-xs uppercase tracking-wide text-slate-500">{label}</p>
      <p className={`mt-1 ${valueClassName}`}>{value}</p>
    </div>
  );
}

function TemperatureMetric({ temperature }: { temperature?: number }) {
  const { t } = useTranslation();
  const color =
    temperature === undefined
      ? "text-slate-400"
      : temperature >= 60
        ? "text-rose-300"
        : temperature >= 50
          ? "text-amber-300"
          : "text-emerald-300";
  return (
    <Metric
      label={t("dashboard.temperature")}
      value={
        temperature === undefined ? "-- °C" : `${Math.round(temperature)} °C`
      }
      valueClassName={color}
    />
  );
}

function SmartMetric({
  health,
  solidState,
  dataWrittenBytes,
  powerOnHours,
  onDetails,
}: {
  health?: boolean;
  solidState?: boolean;
  dataWrittenBytes?: number;
  powerOnHours?: number;
  onDetails?: () => void;
}) {
  const { t } = useTranslation();
  const color =
    health === undefined
      ? "text-slate-400"
      : health
        ? "text-emerald-300"
        : "text-rose-300";

  const showSSDWritten =
    solidState === true && dataWrittenBytes !== undefined;
  const showHDDPowerOn = solidState === false && powerOnHours !== undefined;

  return (
    <div>
      <div className="flex items-start gap-2">
        <Metric
          label="SMART"
          value={
            health === undefined
              ? t("dashboard.unknown")
              : health
                ? t("dashboard.smartHealthy")
                : t("dashboard.smartFailed")
          }
          valueClassName={color}
        />
        {onDetails && (
          <button
            type="button"
            onClick={onDetails}
            className="mb-0.5 rounded-md p-1 text-slate-400 hover:bg-slate-800 hover:text-cyan-300"
            aria-label={t("dashboard.smartDetails")}
            title={t("dashboard.smartDetails")}
          >
            <CircleAlert size={15} />
          </button>
        )}
      </div>
      {showSSDWritten && (
        <p className="mt-1 text-xs text-slate-400">
          {t("dashboard.smartDataWrittenShort", {
            value: formatBytes(dataWrittenBytes),
          })}
        </p>
      )}
      {showHDDPowerOn && (
        <p className="mt-1 text-xs text-slate-400">
          <span
            title={formatHoursTooltip(powerOnHours)}
            className="cursor-help underline decoration-dotted decoration-slate-600 underline-offset-4"
          >
            {t("dashboard.smartPowerOnTimeShort", {
              value: formatPowerOnDuration(powerOnHours, t),
            })}
          </span>
        </p>
      )}
    </div>
  );
}
