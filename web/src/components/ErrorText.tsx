import { useTranslation } from "react-i18next";
import { ApiError } from "../lib/api";

export function ErrorText({ error }: { error: Error }) {
  const { t } = useTranslation();
  const apiError =
    error instanceof ApiError
      ? error
      : new ApiError("internal_error", error.message);

  return (
    <>
      {t(`errors.${apiError.code}`, {
        ...apiError.params,
        defaultValue: t("errors.internal_error"),
      })}
      {apiError.message && apiError.message !== apiError.code && (
        <span className="mt-1 block break-words text-xs text-rose-300/80">
          {apiError.message}
        </span>
      )}
    </>
  );
}
