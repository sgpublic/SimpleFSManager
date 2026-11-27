import { useMutation } from "@tanstack/react-query";
import { X } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { requestJSON } from "../lib/api";
import { ErrorText } from "./ErrorText";

export function ChangePasswordDialog({
  onClose,
  onSuccess,
}: {
  onClose: () => void;
  onSuccess: () => void;
}) {
  const { t } = useTranslation();
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [repeatPassword, setRepeatPassword] = useState("");

  const mutation = useMutation({
    mutationFn: () =>
      requestJSON<void>("/api/auth/password", {
        currentPassword,
        newPassword,
      }),
    onSuccess: () => {
      onSuccess();
      onClose();
    },
  });

  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !mutation.isPending) onClose();
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [onClose, mutation.isPending]);

  const mismatch = repeatPassword.length > 0 && newPassword !== repeatPassword;
  const canSubmit =
    !mutation.isPending &&
    currentPassword.length > 0 &&
    newPassword.length >= 12 &&
    newPassword === repeatPassword;

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!canSubmit) return;
    mutation.mutate();
  }

  return (
    <div
      className="fixed inset-0 z-50 grid place-items-center bg-slate-950/80 p-4 backdrop-blur-sm"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !mutation.isPending) {
          onClose();
        }
      }}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="change-password-title"
        className="w-full max-w-md rounded-2xl border border-slate-700 bg-slate-900 p-6 shadow-2xl shadow-black/60"
      >
        <div className="flex items-start justify-between gap-4">
          <div>
            <h2
              id="change-password-title"
              className="text-lg font-semibold text-slate-100"
            >
              {t("auth.changePasswordTitle")}
            </h2>
            <p className="mt-1 text-sm text-slate-400">
              {t("auth.changePasswordDescription")}
            </p>
          </div>
          <button
            type="button"
            disabled={mutation.isPending}
            onClick={onClose}
            className="rounded-lg p-1 text-slate-400 hover:bg-slate-800 hover:text-slate-100 disabled:opacity-50"
            aria-label={t("dialog.cancel")}
          >
            <X size={20} />
          </button>
        </div>

        <form onSubmit={submit} className="mt-5 space-y-4">
          <label className="block text-sm text-slate-300">
            {t("auth.currentPassword")}
            <input
              type="password"
              autoComplete="current-password"
              required
              disabled={mutation.isPending}
              value={currentPassword}
              onChange={(event) => setCurrentPassword(event.target.value)}
              className="mt-1.5 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100 outline-none focus:border-cyan-400 disabled:opacity-60"
            />
          </label>

          <label className="block text-sm text-slate-300">
            {t("auth.newPassword")}
            <input
              type="password"
              autoComplete="new-password"
              required
              minLength={12}
              disabled={mutation.isPending}
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
              className="mt-1.5 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100 outline-none focus:border-cyan-400 disabled:opacity-60"
            />
            <span className="mt-1 block text-xs text-slate-500">
              {t("auth.passwordHint")}
            </span>
          </label>

          <label className="block text-sm text-slate-300">
            {t("auth.repeatPassword")}
            <input
              type="password"
              autoComplete="new-password"
              required
              minLength={12}
              disabled={mutation.isPending}
              value={repeatPassword}
              onChange={(event) => setRepeatPassword(event.target.value)}
              className="mt-1.5 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100 outline-none focus:border-cyan-400 disabled:opacity-60"
            />
          </label>

          {mismatch && (
            <p className="text-xs text-rose-300">{t("auth.passwordMismatch")}</p>
          )}

          {mutation.isError && (
            <p className="rounded-lg bg-rose-950/50 p-3 text-sm text-rose-200">
              <ErrorText error={mutation.error} />
            </p>
          )}

          <div className="flex justify-end gap-3 pt-1">
            <button
              type="button"
              disabled={mutation.isPending}
              onClick={onClose}
              className="rounded-lg px-4 py-2 text-sm text-slate-300 hover:bg-slate-800 disabled:opacity-50"
            >
              {t("dialog.cancel")}
            </button>
            <button
              type="submit"
              disabled={!canSubmit}
              className="rounded-lg bg-cyan-400 px-4 py-2 text-sm font-medium text-slate-950 hover:bg-cyan-300 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {mutation.isPending
                ? t("dialog.processing")
                : t("auth.changePassword")}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
