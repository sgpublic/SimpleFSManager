import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { fetchSmartDetails } from "../lib/api";
import { parseSmartSummary, type SmartMetricValue } from "../lib/smart";

export function SmartDetailsDialog({
  diskPath,
  solidState,
  onClose,
}: {
  diskPath: string;
  solidState?: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const details = useQuery({
    queryKey: ["smart", diskPath],
    queryFn: () => fetchSmartDetails(diskPath),
  });

  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-slate-950/80 p-4 backdrop-blur-sm"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="smart-details-title"
        className="flex max-h-[85vh] w-full max-w-4xl flex-col rounded-2xl border border-slate-700 bg-slate-900 p-6 shadow-2xl shadow-black/60"
      >
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2 id="smart-details-title" className="text-lg font-semibold">
              {t("dashboard.smartDetails")}
            </h2>
            <p className="mt-1 break-all font-mono text-xs text-slate-400">
              {diskPath}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg p-1 text-slate-400 hover:bg-slate-800 hover:text-slate-100"
            aria-label={t("dialog.cancel")}
          >
            <X size={20} />
          </button>
        </div>
        <div className="mt-5 min-h-0 overflow-auto rounded-lg border border-slate-800 bg-slate-950 p-4">
          {details.isLoading && (
            <p className="text-sm text-slate-400">
              {t("dashboard.smartLoading")}
            </p>
          )}
          {details.isError && (
            <p className="text-sm text-rose-300">
              {t("dashboard.smartUnavailable")}
            </p>
          )}
          {details.data && (
            <SmartDetails data={details.data} solidState={solidState} />
          )}
        </div>
      </section>
    </div>
  );
}

function SmartDetails({
  data,
  solidState,
}: {
  data: Record<string, unknown>;
  solidState?: boolean;
}) {
  const { t } = useTranslation();
  const summary = parseSmartSummary(data, t, solidState);

  return (
    <div className="space-y-6">
      <div className="grid gap-3 sm:grid-cols-2">
        <SmartDetail
          label={t("dashboard.smartOverallHealth")}
          value={
            summary.health === undefined
              ? t("dashboard.smartNotReported")
              : summary.health
                ? t("dashboard.smartHealthy")
                : t("dashboard.smartFailed")
          }
          tone={
            summary.health === undefined
              ? "neutral"
              : summary.health
                ? "good"
                : "critical"
          }
        />
        <SmartDetail
          label={t("dashboard.temperature")}
          value={
            summary.temperature === undefined
              ? t("dashboard.smartNotReported")
              : `${Math.round(summary.temperature)} °C`
          }
          tone={
            summary.temperature === undefined
              ? "neutral"
              : summary.temperature >= 60
                ? "critical"
                : summary.temperature >= 50
                  ? "warning"
                  : "good"
          }
        />
      </div>
      {summary.device.length > 0 && (
        <SmartSection
          title={t("dashboard.smartDeviceInfo")}
          metrics={summary.device}
        />
      )}
      {summary.usage.length > 0 && (
        <SmartSection
          title={t("dashboard.smartUsage")}
          metrics={summary.usage}
        />
      )}
      {summary.nvmeRisks.length > 0 && (
        <SmartSection
          title={t("dashboard.smartNvmeHealth")}
          metrics={summary.nvmeRisks}
        />
      )}
      {summary.ataRisks.length > 0 && (
        <SmartSection
          title={t("dashboard.smartAtaHealth")}
          metrics={summary.ataRisks}
        />
      )}
    </div>
  );
}

function SmartSection({
  title,
  metrics,
}: {
  title: string;
  metrics: SmartMetricValue[];
}) {
  return (
    <section>
      <h3 className="mb-2 text-sm font-medium text-slate-200">{title}</h3>
      <div className="grid gap-3 sm:grid-cols-2">
        {metrics.map((metric) => (
          <SmartDetail key={metric.label} {...metric} />
        ))}
      </div>
    </section>
  );
}

function SmartDetail({ label, value, tone, tooltip }: SmartMetricValue) {
  const color = {
    neutral: "text-slate-200",
    good: "text-emerald-300",
    warning: "text-amber-300",
    critical: "text-rose-300",
  }[tone];
  return (
    <div className="rounded-lg border border-slate-800 bg-slate-900 p-3">
      <p className="text-xs uppercase tracking-wide text-slate-500">{label}</p>
      <p className={`mt-1 break-words text-sm font-medium ${color}`}>
        {tooltip ? (
          <span
            title={tooltip}
            className="cursor-help underline decoration-dotted decoration-slate-600 underline-offset-4"
          >
            {value}
          </span>
        ) : (
          value
        )}
      </p>
    </div>
  );
}
