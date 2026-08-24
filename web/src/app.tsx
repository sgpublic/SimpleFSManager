import { useMutation, useQuery } from "@tanstack/react-query";
import { LockKeyhole } from "lucide-react";
import { useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Dashboard } from "./dashboard";

type AuthStatus = {
  setupRequired: boolean;
  authenticated: boolean;
  username?: string;
};
type BuildInfo = { version: string };

class ApiError extends Error {
  constructor(
    public code: string,
    public params: Record<string, unknown> = {},
    public message = code,
  ) {
    super(message);
  }
}

export function App() {
  const { t } = useTranslation();
  const status = useQuery({
    queryKey: ["auth-status"],
    queryFn: () => request<AuthStatus>("/api/auth/status"),
  });
  const buildInfo = useQuery({
    queryKey: ["build-info"],
    queryFn: () => request<BuildInfo>("/api/build-info"),
  });
  const version = buildInfo.data?.version ?? "dev";
  if (status.isLoading)
    return (
      <AuthShell version={version}>
        <p className="text-sm text-slate-400">{t("auth.checking")}</p>
      </AuthShell>
    );
  if (status.isError)
    return (
      <AuthShell version={version}>
        <p className="text-sm text-rose-300">{t("auth.unavailable")}</p>
      </AuthShell>
    );
  if (status.data?.setupRequired)
    return (
      <Bootstrap version={version} onComplete={() => void status.refetch()} />
    );
  if (!status.data?.authenticated)
    return (
      <Login
        version={version}
        username={status.data?.username ?? ""}
        onComplete={() => void status.refetch()}
      />
    );
  return (
    <Dashboard
      version={version}
      username={status.data.username ?? ""}
      onLogout={() => void status.refetch()}
    />
  );
}

function Bootstrap({
  version,
  onComplete,
}: {
  version: string;
  onComplete: () => void;
}) {
  const { t } = useTranslation();
  const [username, setUsername] = useState("");
  const [systemPassword, setSystemPassword] = useState("");
  const [projectPassword, setProjectPassword] = useState("");
  const [repeatPassword, setRepeatPassword] = useState("");
  const bootstrap = useMutation({
    mutationFn: () =>
      request<AuthStatus>("/api/auth/bootstrap", {
        username,
        systemPassword,
        projectPassword,
      }),
    onSuccess: onComplete,
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    if (projectPassword !== repeatPassword) return;
    bootstrap.mutate();
  }
  return (
    <AuthShell version={version}>
      <AuthCard
        title={t("auth.initializeTitle")}
        description={t("auth.initializeDescription")}
      >
        <form onSubmit={submit} className="space-y-4">
          <Field
            label={t("auth.localUsername")}
            value={username}
            onChange={setUsername}
            autoComplete="username"
          />
          <Field
            label={t("auth.systemPassword")}
            value={systemPassword}
            onChange={setSystemPassword}
            type="password"
            autoComplete="current-password"
          />
          <Field
            label={t("auth.projectPassword")}
            value={projectPassword}
            onChange={setProjectPassword}
            type="password"
            autoComplete="new-password"
            hint={t("auth.passwordHint")}
          />
          <Field
            label={t("auth.repeatPassword")}
            value={repeatPassword}
            onChange={setRepeatPassword}
            type="password"
            autoComplete="new-password"
          />
          {projectPassword !== repeatPassword && (
            <p className="text-xs text-rose-300">
              {t("auth.passwordMismatch")}
            </p>
          )}
          <AuthError error={bootstrap.error} />
          <SubmitButton pending={bootstrap.isPending}>
            {t("auth.initialize")}
          </SubmitButton>
        </form>
      </AuthCard>
    </AuthShell>
  );
}

function Login({
  version,
  username,
  onComplete,
}: {
  version: string;
  username: string;
  onComplete: () => void;
}) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const login = useMutation({
    mutationFn: () =>
      request<AuthStatus>("/api/auth/login", { username, password }),
    onSuccess: onComplete,
  });
  return (
    <AuthShell version={version}>
      <AuthCard
        title={t("auth.signInTitle")}
        description={t("auth.signInDescription", { username })}
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            login.mutate();
          }}
          className="space-y-4"
        >
          <Field
            label={t("auth.username")}
            value={username}
            onChange={() => undefined}
            disabled
          />
          <Field
            label={t("auth.projectPassword")}
            value={password}
            onChange={setPassword}
            type="password"
            autoComplete="current-password"
          />
          <AuthError error={login.error} />
          <SubmitButton pending={login.isPending}>
            {t("auth.signIn")}
          </SubmitButton>
        </form>
      </AuthCard>
    </AuthShell>
  );
}

function AuthShell({
  version,
  children,
}: {
  version: string;
  children: ReactNode;
}) {
  return (
    <main className="relative grid min-h-screen place-items-center bg-slate-950 px-6 text-slate-100">
      <div className="absolute right-6 top-6">
        <LanguageSelect />
      </div>
      <div className="w-full max-w-md">{children}</div>
      <p className="absolute bottom-6 text-xs text-slate-600">{version}</p>
    </main>
  );
}
function AuthCard({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="rounded-2xl border border-slate-800 bg-slate-900/70 p-7 shadow-2xl shadow-black/30">
      <div className="mb-6 flex items-center gap-3">
        <div className="rounded-xl bg-cyan-400/10 p-2 text-cyan-300">
          <LockKeyhole size={20} />
        </div>
        <div>
          <h1 className="font-semibold">{title}</h1>
          <p className="mt-1 text-sm text-slate-400">{description}</p>
        </div>
      </div>
      {children}
    </section>
  );
}
function LanguageSelect() {
  const { i18n, t } = useTranslation();
  return (
    <label className="text-xs text-slate-400">
      <span className="sr-only">{t("language.label")}</span>
      <select
        className="rounded-md border border-slate-700 bg-slate-900 px-2 py-1 text-slate-200"
        value={i18n.resolvedLanguage === "zh-CN" ? "zh-CN" : "en"}
        onChange={(event) => void i18n.changeLanguage(event.target.value)}
      >
        <option value="zh-CN">{t("language.chinese")}</option>
        <option value="en">{t("language.english")}</option>
      </select>
    </label>
  );
}
function Field({
  label,
  value,
  onChange,
  type = "text",
  hint,
  disabled,
  autoComplete,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: string;
  hint?: string;
  disabled?: boolean;
  autoComplete?: string;
}) {
  return (
    <label className="block text-sm text-slate-300">
      {label}
      <input
        className="mt-1.5 w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-slate-100 outline-none focus:border-cyan-400 disabled:cursor-not-allowed disabled:opacity-60"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        type={type}
        disabled={disabled}
        autoComplete={autoComplete}
        required
      />
      {hint && (
        <span className="mt-1 block text-xs text-slate-500">{hint}</span>
      )}
    </label>
  );
}
function AuthError({ error }: { error: Error | null }) {
  return error ? (
    <p className="rounded-lg bg-rose-950/50 p-3 text-sm text-rose-200">
      <ErrorText error={error} />
    </p>
  ) : null;
}
function ErrorText({ error }: { error: Error }) {
  const { t } = useTranslation();
  const apiError =
    error instanceof ApiError ? error : new ApiError("internal_error");
  return (
    <>
      {t(`errors.${apiError.code}`, {
        ...apiError.params,
        defaultValue: t("errors.internal_error"),
      })}
      {apiError.message !== apiError.code && (
        <span className="mt-1 block break-words text-xs text-rose-300/80">
          {apiError.message}
        </span>
      )}
    </>
  );
}
function SubmitButton({
  children,
  pending,
}: {
  children: ReactNode;
  pending: boolean;
}) {
  const { t } = useTranslation();
  return (
    <button
      disabled={pending}
      className="w-full rounded-lg bg-cyan-400 px-4 py-2.5 text-sm font-medium text-slate-950 hover:bg-cyan-300 disabled:opacity-60"
    >
      {pending ? t("auth.waiting") : children}
    </button>
  );
}
async function request<T = unknown>(
  path: string,
  body?: Record<string, unknown>,
): Promise<T> {
  const response = await fetch(path, {
    method: body ? "POST" : "GET",
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!response.ok) {
    const error = (await response.json().catch(() => null)) as {
      code?: string;
      message?: string;
      detail?: string;
      errors?: Array<{ message?: string }>;
      params?: Record<string, unknown>;
    } | null;
    const message =
      error?.message ?? error?.errors?.find((item) => item.message)?.message ?? error?.detail ?? "request_failed";
    throw new ApiError(
      error?.code ?? error?.detail ?? "request_failed",
      error?.params,
      message,
    );
  }
  return response.status === 204
    ? (undefined as T)
    : (response.json() as Promise<T>);
}
