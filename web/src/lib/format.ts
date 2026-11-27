export function formatBytes(bytes: number): string {
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let value = bytes;
  let index = 0;
  while (value > 800 && index < units.length - 1) {
    value /= 1000;
    index++;
  }
  const rounded = Math.round(value * 100) / 100;
  return `${rounded.toFixed(Math.abs(Math.trunc(rounded)) >= 100 ? 0 : 2)} ${units[index]}`;
}

function formatCompactDecimal(value: number): string {
  if (value >= 100) return String(Math.round(value));
  const rounded = Math.round(value * 10) / 10;
  return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1);
}

export function formatPowerOnDuration(
  hours: number,
  t: (key: string, options?: Record<string, unknown>) => string,
): string {
  if (hours < 24) {
    return t("units.hours", { value: Math.round(hours).toLocaleString() });
  }
  const days = hours / 24;
  if (days < 30) {
    return t("units.days", { value: formatCompactDecimal(days) });
  }
  const months = days / 30;
  if (days < 365) {
    return t("units.months", { value: formatCompactDecimal(months) });
  }
  const years = days / 365;
  return t("units.years", { value: formatCompactDecimal(years) });
}

export function formatHoursTooltip(hours: number): string {
  return `${Math.round(hours).toLocaleString()} h`;
}
