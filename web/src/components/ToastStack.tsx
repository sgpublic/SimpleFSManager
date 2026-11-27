import { TriangleAlert, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { ErrorText } from "./ErrorText";

export function ToastStack({
  rebootRequired,
  onDismissReboot,
  onRestartSystem,
  error,
  onDismissError,
}: {
  rebootRequired: boolean;
  onDismissReboot: () => void;
  onRestartSystem: () => void;
  error: Error | null;
  onDismissError: () => void;
}) {
  const { t } = useTranslation();
  if (!rebootRequired && !error) return null;

  return (
    <div className="fixed bottom-6 right-6 z-[60] flex max-w-sm flex-col gap-3">
      {rebootRequired && (
        <div
          role="status"
          className="rounded-xl border border-amber-700 bg-amber-950/95 p-4 text-sm text-amber-100 shadow-2xl shadow-black/50"
        >
          <div className="flex items-start gap-3">
            <TriangleAlert
              className="mt-0.5 shrink-0 text-amber-300"
              size={18}
            />
            <p className="leading-6">{t("dashboard.rebootRequired")}</p>
            <button
              type="button"
              onClick={onDismissReboot}
              className="-mr-1 -mt-1 rounded-md p-1 text-amber-300 hover:bg-amber-900 hover:text-white"
              aria-label={t("dialog.cancel")}
            >
              <X size={17} />
            </button>
          </div>
          <button
            type="button"
            onClick={onRestartSystem}
            className="mt-3 rounded-lg bg-amber-400 px-3 py-2 text-xs font-medium text-slate-950 hover:bg-amber-300"
          >
            {t("dashboard.restartSystem")}
          </button>
        </div>
      )}

      {error && (
        <div
          role="alert"
          className="flex items-start gap-3 rounded-xl border border-rose-800 bg-rose-950/95 p-4 text-sm text-rose-100 shadow-2xl shadow-black/50"
        >
          <TriangleAlert className="mt-0.5 shrink-0 text-rose-300" size={18} />
          <p className="leading-6">
            <ErrorText error={error} />
          </p>
          <button
            type="button"
            onClick={onDismissError}
            className="-mr-1 -mt-1 rounded-md p-1 text-rose-300 hover:bg-rose-900 hover:text-white"
            aria-label={t("dialog.cancel")}
          >
            <X size={17} />
          </button>
        </div>
      )}
    </div>
  );
}
