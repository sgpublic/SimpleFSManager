import {
  formatBytes,
  formatHoursTooltip,
  formatPowerOnDuration,
} from "./format";

export type SmartTone = "neutral" | "good" | "warning" | "critical";
export type SmartMetricValue = {
  label: string;
  value: string;
  tone: SmartTone;
  tooltip?: string;
};

export interface SmartSummary {
  health: boolean | undefined;
  temperature: number | undefined;
  device: SmartMetricValue[];
  usage: SmartMetricValue[];
  nvmeRisks: SmartMetricValue[];
  ataRisks: SmartMetricValue[];
}

export function parseSmartSummary(
  data: Record<string, unknown>,
  t: (key: string, options?: Record<string, unknown>) => string,
  solidStateHint?: boolean,
): SmartSummary {
  const nvme = smartObject(data, "nvme_smart_health_information_log");
  const ata = smartObject(data, "ata_smart_attributes");
  const criticalWarning = smartNumber(nvme, "critical_warning");
  const reportedHealth = smartBoolean(data, "smart_status", "passed");
  const health =
    reportedHealth ??
    (criticalWarning === undefined ? undefined : criticalWarning === 0);

  const reportedTemperature =
    smartNumber(data, "temperature", "current") ??
    smartNumber(nvme, "temperature") ??
    ataTemperature(ata);
  const temperature =
    reportedTemperature !== undefined && reportedTemperature > 200
      ? reportedTemperature - 273
      : reportedTemperature;

  const powerOnHours =
    smartNumber(data, "power_on_time", "hours") ??
    smartNumber(nvme, "power_on_hours") ??
    ataRawNumber(ata, 9);
  const powerCycles =
    smartNumber(data, "power_cycle_count") ??
    smartNumber(nvme, "power_cycles");
  const model =
    smartText(data, "model_name") ?? smartText(data, "device", "model_name");
  const serial =
    smartText(data, "serial_number") ??
    smartText(data, "device", "serial_number");
  const firmware =
    smartText(data, "firmware_version") ??
    smartText(data, "device", "revision");
  const protocol = smartText(data, "device", "protocol");

  const rotationRate = smartNumber(data, "rotation_rate");
  const isSolidState =
    rotationRate !== undefined
      ? rotationRate === 0
      : nvme !== undefined || protocol?.toLowerCase() === "nvme"
        ? true
        : solidStateHint;

  const dataWrittenBytes = extractDataWrittenBytes(data, nvme, ata);
  const percentageUsed = smartNumber(nvme, "percentage_used");
  const availableSpare = smartNumber(nvme, "available_spare");

  const usage = [
    isSolidState !== false &&
      dataWrittenBytes !== undefined && {
        label: t("dashboard.smartDataWritten"),
        value: formatBytes(dataWrittenBytes),
        tone: "neutral" as const,
      },
    powerOnHours !== undefined && {
      label: t("dashboard.smartPowerOnTime"),
      value: formatPowerOnDuration(powerOnHours, t),
      tone: "neutral" as const,
      tooltip: formatHoursTooltip(powerOnHours),
    },
    smartMetric(t("dashboard.smartPowerCycles"), powerCycles, "neutral"),
    nvme &&
      smartMetric(
        t("dashboard.smartLifeUsed"),
        percentageUsed,
        percentageUsedTone(percentageUsed),
        "%",
      ),
    nvme &&
      smartMetric(
        t("dashboard.smartSpareAvailable"),
        availableSpare,
        spareTone(availableSpare),
        "%",
      ),
  ].filter((metric): metric is SmartMetricValue => Boolean(metric));

  const device = [
    smartMetric(t("dashboard.smartModel"), model, "neutral"),
    smartMetric(t("dashboard.smartSerial"), serial, "neutral"),
    smartMetric(t("dashboard.smartFirmware"), firmware, "neutral"),
    smartMetric(t("dashboard.smartProtocol"), protocol, "neutral"),
  ].filter((metric): metric is SmartMetricValue => Boolean(metric));

  return {
    health,
    temperature,
    device,
    usage,
    nvmeRisks: nvmeRiskMetrics(nvme, t),
    ataRisks: ataRiskMetrics(ata, t),
  };
}

function extractDataWrittenBytes(
  data: Record<string, unknown>,
  nvme: Record<string, unknown> | undefined,
  ata: Record<string, unknown> | undefined,
): number | undefined {
  const dataUnitsWritten = smartNumber(nvme, "data_units_written");
  if (dataUnitsWritten !== undefined && dataUnitsWritten >= 0) {
    return dataUnitsWritten * 512000;
  }

  const blockSize = smartNumber(data, "logical_block_size") || 512;
  const stats = smartObject(data, "ata_device_statistics");
  for (const page of smartArray(stats, "pages")) {
    for (const entry of smartArray(
      page && typeof page === "object"
        ? (page as Record<string, unknown>)
        : undefined,
      "table",
    )) {
      if (
        smartText(entry, "name")?.toLowerCase() === "logical sectors written"
      ) {
        const value = smartNumber(entry, "value");
        if (value !== undefined && value >= 0) {
          return value * blockSize;
        }
      }
    }
  }

  let fallbackNandBytes: number | undefined;
  for (const attribute of smartArray(ata, "table")) {
    const id = smartNumber(attribute, "id");
    const raw = smartNumber(attribute, "raw", "value");
    if (id === undefined || raw === undefined || raw < 0) continue;
    const lowerName = (smartText(attribute, "name") ?? "").toLowerCase();
    if (id === 241 || id === 246) {
      return ataWrittenAttributeBytes(raw, lowerName, blockSize);
    }
    if (id === 249 && (lowerName.includes("gib") || lowerName.includes("gb"))) {
      fallbackNandBytes = raw * 1024 ** 3;
    }
  }
  return fallbackNandBytes;
}

function ataWrittenAttributeBytes(
  raw: number,
  lowerName: string,
  blockSize: number,
): number {
  if (lowerName.includes("32mib")) return raw * 32 * 1024 * 1024;
  if (lowerName.includes("gib")) return raw * 1024 ** 3;
  if (lowerName.includes("gb")) return raw * 1000 ** 3;
  if (lowerName.includes("mib")) return raw * 1024 ** 2;
  if (lowerName.includes("mb")) return raw * 1000 ** 2;
  return raw * blockSize;
}

function smartObject(data: unknown, ...path: string[]) {
  const value = smartValue(data, ...path);
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function smartValue(data: unknown, ...path: string[]) {
  let value: unknown = data;
  for (const key of path) {
    value =
      value && typeof value === "object"
        ? (value as Record<string, unknown>)[key]
        : undefined;
  }
  return value;
}

function smartNumber(data: unknown, ...path: string[]) {
  const value = smartValue(data, ...path);
  return typeof value === "number" && Number.isFinite(value)
    ? value
    : undefined;
}

function smartText(data: unknown, ...path: string[]) {
  const value = smartValue(data, ...path);
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function smartBoolean(data: unknown, ...path: string[]) {
  const value = smartValue(data, ...path);
  return typeof value === "boolean" ? value : undefined;
}

function ataTemperature(ata?: Record<string, unknown>) {
  for (const attribute of smartArray(ata, "table")) {
    const id = smartNumber(attribute, "id");
    if (id === 190 || id === 194) {
      return smartNumber(attribute, "raw", "value");
    }
  }
}

function ataRawNumber(ata: Record<string, unknown> | undefined, id: number) {
  for (const attribute of smartArray(ata, "table")) {
    if (smartNumber(attribute, "id") === id) {
      const value = smartNumber(attribute, "raw", "value");
      if (value !== undefined && value >= 0) return value;
    }
  }
}

function ataRiskMetrics(
  ata: Record<string, unknown> | undefined,
  t: (key: string) => string,
) {
  return [
    ataAttribute(ata, 5, t("dashboard.smartReallocatedSectors"), "critical"),
    ataAttribute(ata, 197, t("dashboard.smartPendingSectors"), "critical"),
    ataAttribute(ata, 198, t("dashboard.smartUncorrectableSectors"), "critical"),
    ataAttribute(ata, 199, t("dashboard.smartCrcErrors"), "warning"),
    ataAttribute(ata, 188, t("dashboard.smartCommandTimeouts"), "warning"),
  ].filter((metric): metric is SmartMetricValue => Boolean(metric));
}

function ataAttribute(
  ata: Record<string, unknown> | undefined,
  id: number,
  label: string,
  nonZeroTone: SmartTone,
) {
  for (const attribute of smartArray(ata, "table")) {
    if (smartNumber(attribute, "id") === id) {
      const value = smartNumber(attribute, "raw", "value");
      return smartMetric(
        label,
        value,
        value && value > 0 ? nonZeroTone : "good",
      );
    }
  }
}

function nvmeRiskMetrics(
  nvme: Record<string, unknown> | undefined,
  t: (key: string) => string,
) {
  const criticalWarnings = smartNumber(nvme, "critical_warning");
  const integrityErrors = smartNumber(nvme, "media_and_data_integrity_errors");
  const errorLogEntries = smartNumber(nvme, "num_err_log_entries");
  const unsafeShutdowns = smartNumber(nvme, "unsafe_shutdowns");
  return [
    smartMetric(
      t("dashboard.smartCriticalWarnings"),
      criticalWarnings,
      criticalWarnings ? "critical" : "good",
    ),
    smartMetric(
      t("dashboard.smartIntegrityErrors"),
      integrityErrors,
      integrityErrors ? "critical" : "good",
    ),
    smartMetric(
      t("dashboard.smartErrorLogEntries"),
      errorLogEntries,
      errorLogEntries ? "warning" : "good",
    ),
    smartMetric(
      t("dashboard.smartUnsafeShutdowns"),
      unsafeShutdowns,
      unsafeShutdowns ? "warning" : "good",
    ),
  ].filter((metric): metric is SmartMetricValue => Boolean(metric));
}

function smartArray(data: Record<string, unknown> | undefined, key: string) {
  const value = data?.[key];
  return Array.isArray(value) ? value : [];
}

function smartMetric(
  label: string,
  value: number | string | undefined,
  tone: SmartTone,
  suffix = "",
): SmartMetricValue | undefined {
  if (value === undefined) return undefined;
  return {
    label,
    value:
      typeof value === "number" ? `${value.toLocaleString()}${suffix}` : value,
    tone,
  };
}

function percentageUsedTone(value: number | undefined): SmartTone {
  if (value === undefined || value < 90) return "good";
  return value >= 100 ? "critical" : "warning";
}

function spareTone(value: number | undefined): SmartTone {
  if (value === undefined || value > 20) return "good";
  return value <= 10 ? "critical" : "warning";
}
