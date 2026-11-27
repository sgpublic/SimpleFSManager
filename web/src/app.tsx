import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LockKeyhole } from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ErrorText } from "./components/ErrorText";
import { LanguageSelect } from "./components/LanguageSelect";
import { Dashboard } from "./dashboard";
import {
  requestJSON,
  setUnauthorizedHandler,
  type AuthStatus,
  type BuildInfo,
} from "./lib/api";

export function App() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  useEffect(() => {
    setUnauthorizedHandler(() => {
      queryClient.removeQueries({ queryKey: ["disks"] });
      queryClient.removeQueries({ queryKey: ["smart"] });
      void queryClient.invalidateQueries({ queryKey: ["auth-status"] });
    });
    return () => setUnauthorizedHandler(null);
  }, [queryClient]);

  const status = useQuery({
    queryKey: ["auth-status"],
    queryFn: () => requestJSON<AuthStatus>("/api/auth/status"),
  });
  const buildInfo = useQuery({
    queryKey: ["build-info"],
    queryFn: () => requestJSON<BuildInfo>("/api/build-info"),
  });
  const version = buildInfo.data?.version ?? "dev";

  if (status.isLoading) {
    return (
      <AuthShell version={version}>
        <p className="text-sm text-slate-400">{t("auth.checking")}</p>
      </AuthShell>
    );
  }
  if (status.isError) {
    return (
      <AuthShell version={version}>
        <p className="text-sm text-rose-300">{t("auth.unavailable")}</p>
      </AuthShell>
    );
  }
  if (!status.data?.authenticated) {
    return <Login version={version} onComplete={() => void status.refetch()} />;
  }
  return (
    <Dashboard
      version={version}
      username={status.data.username ?? ""}
      onLogout={() => void status.refetch()}
    />
  );
}

function Login({
  version,
  onComplete,
}: {
  version: string;
  onComplete: () => void;
}) {
  const { t } = useTranslation();
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const login = useMutation({
    mutationFn: () =>
      requestJSON<AuthStatus>("/api/auth/login", { username, password }),
    onSuccess: onComplete,
  });

  function submit(event: FormEvent) {
    event.preventDefault();
    login.mutate();
  }

  return (
    <AuthShell version={version}>
      <AuthCard
        title={t("auth.signInTitle")}
        description={t("auth.signInHint")}
      >
        <form onSubmit={submit} className="space-y-4">
          <Field
            label={t("auth.username")}
            value={username}
            onChange={setUsername}
            autoComplete="username"
          />
          <Field
            label={t("auth.password")}
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

function Field({
  label,
  value,
  onChange,
  type = "text",
  disabled,
  autoComplete,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: string;
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
