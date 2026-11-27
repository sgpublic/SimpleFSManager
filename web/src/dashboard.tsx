import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { HardDrive, LogOut, Power, RefreshCw } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { DiskCard } from "./components/DiskCard";
import { LanguageSelect } from "./components/LanguageSelect";
import {
  OperationDialog,
  type DialogState,
} from "./components/OperationDialog";
import type { ConfirmFn } from "./components/PartitionRow";
import { SmartDetailsDialog } from "./components/SmartDetailsDialog";
import { ToastStack } from "./components/ToastStack";
import {
  executeOperation,
  fetchDisks,
  type Disk,
  type Operation,
  type OperationResult,
} from "./lib/api";

function diskKey(disk: Disk, index: number): string {
  if (disk.path) return disk.path;
  const firstUUID = disk.partitions?.[0]?.uuid;
  if (firstUUID) return `missing-${firstUUID}`;
  return `disk-${disk.serial || disk.name}-${index}`;
}

export function Dashboard({
  version,
  username,
  onLogout,
}: {
  version: string;
  username: string;
  onLogout: () => void;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [dialog, setDialog] = useState<DialogState | null>(null);
  const [smartTarget, setSmartTarget] = useState<{
    path: string;
    solidState?: boolean;
  } | null>(null);
  const [rebootRequired, setRebootRequired] = useState(false);

  const disks = useQuery({
    queryKey: ["disks"],
    queryFn: () => fetchDisks(false),
    refetchInterval: 15_000,
    refetchIntervalInBackground: false,
  });

  const forceRefresh = useMutation({
    mutationFn: () => fetchDisks(true),
    onSuccess: (snapshot) => {
      queryClient.setQueryData(["disks"], snapshot);
    },
  });

  const operation = useMutation({
    mutationFn: (op: Operation) => executeOperation(op),
    onSuccess: (result: OperationResult) => {
      setDialog(null);
      setRebootRequired(Boolean(result.rebootRequired));
      void queryClient.invalidateQueries({ queryKey: ["disks"] });
    },
  });

  const autoMountToggle = useMutation({
    mutationFn: ({ uuid, autoMount }: { uuid: string; autoMount: boolean }) =>
      executeOperation({
        path: "/api/volumes/auto-mount",
        body: { uuid, autoMount },
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["disks"] });
    },
  });

  const confirm: ConfirmFn = (target, title, next, options) => {
    const tone = options?.tone ?? "danger";
    const delaySeconds = options?.delaySeconds ?? (tone === "danger" ? 5 : 0);
    setDialog({
      kind: "confirm",
      title,
      target,
      tone,
      delaySeconds,
      description:
        options?.description ??
        (tone === "normal" ? t("dialog.confirmHint") : t("dialog.warning")),
      details: options?.details ?? [],
      operation: next(target),
    });
  };

  function requestRestart() {
    confirm(
      "system",
      t("dashboard.restartSystem"),
      (confirmation) => ({
        path: "/api/system/reboot",
        body: { confirm: confirmation },
      }),
      {
        tone: "warning",
        delaySeconds: 3,
        description: t("dialog.warningReboot"),
      },
    );
  }

  async function logout() {
    await fetch("/api/auth/logout", { method: "POST" });
    queryClient.clear();
    onLogout();
  }

  return (
    <main className="min-h-screen w-full min-w-0 bg-slate-950 px-4 py-6 text-slate-100 sm:px-6 sm:py-10">
      <section className="mx-auto w-full min-w-0 max-w-5xl">
        <header className="flex flex-col gap-4 border-b border-slate-800 pb-6 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 items-center gap-3">
            <HardDrive className="text-cyan-400" />
            <div className="min-w-0">
              <h1 className="text-xl font-semibold">SimpleFSManager</h1>
              <p className="text-sm text-slate-400">
                {t("dashboard.signedIn", { username })}
              </p>
              <p className="mt-1 text-xs text-slate-600">{version}</p>
            </div>
          </div>
          <div className="flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
            <LanguageSelect />
            <button
              type="button"
              onClick={requestRestart}
              className="inline-flex items-center gap-2 text-sm text-amber-300 hover:text-amber-100"
            >
              <Power size={16} />
              {t("dashboard.restartSystem")}
            </button>
            <button
              type="button"
              onClick={() => void logout()}
              className="inline-flex items-center gap-2 text-sm text-slate-400 hover:text-slate-100"
            >
              <LogOut size={16} />
              {t("dashboard.signOut")}
            </button>
          </div>
        </header>

        <section className="mt-12">
          <div className="mb-5 flex items-end justify-between">
            <div>
              <h2 className="text-lg font-medium">
                {t("dashboard.physicalDisks")}
              </h2>
              <p className="mt-1 text-sm text-slate-400">
                {t("dashboard.protection")}
              </p>
            </div>
            <div className="flex items-center gap-3">
              {disks.data?.updatedAt && (
                <span className="hidden text-xs text-slate-500 sm:inline">
                  {t("dashboard.lastUpdated", {
                    time: new Date(disks.data.updatedAt).toLocaleTimeString(),
                  })}
                </span>
              )}
              <button
                type="button"
                disabled={forceRefresh.isPending}
                onClick={() => forceRefresh.mutate()}
                className="rounded-lg border border-slate-700 p-2 text-slate-300 hover:border-cyan-400 hover:text-cyan-300 disabled:opacity-60"
                aria-label={t("dashboard.refresh")}
                title={t("dashboard.refresh")}
              >
                <RefreshCw
                  size={17}
                  className={
                    disks.isFetching || forceRefresh.isPending
                      ? "animate-spin"
                      : undefined
                  }
                />
              </button>
            </div>
          </div>

          {operation.isPending && (
            <p className="mb-4 text-sm text-cyan-300">
              {t("dashboard.operationPending")}
            </p>
          )}
          {disks.isLoading && (
            <p className="text-sm text-slate-400">
              {t("dashboard.discovering")}
            </p>
          )}
          {disks.isError && (
            <p className="rounded-xl border border-rose-900 bg-rose-950/40 p-4 text-sm text-rose-200">
              {t("dashboard.discoveryFailed")}
            </p>
          )}

          <div className="grid gap-4">
            {disks.data?.disks.map((disk, index) => (
              <DiskCard
                key={diskKey(disk, index)}
                disk={disk}
                confirm={confirm}
                onOpenSmart={(targetDisk) =>
                  setSmartTarget({
                    path: targetDisk.path,
                    solidState: targetDisk.solidState,
                  })
                }
                onOpenCreatePartition={(targetDisk, details) =>
                  setDialog({
                    kind: "create",
                    target: targetDisk.path,
                    diskSizeBytes: targetDisk.sizeBytes,
                    maxGB: Math.max(
                      1,
                      Math.floor(targetDisk.sizeBytes / 1000 ** 3),
                    ),
                    zoneSizeBytes: targetDisk.zoneSizeBytes,
                    details,
                  })
                }
                onOpenFormatWholeDisk={(diskPath, details) =>
                  setDialog({ kind: "formatDisk", target: diskPath, details })
                }
                onOpenFormatPartition={(partitionPath, details) =>
                  setDialog({ kind: "format", target: partitionPath, details })
                }
                onOpenMountPath={(
                  target,
                  currentPath,
                  currentAutoMount,
                  missing,
                  details,
                ) =>
                  setDialog({
                    kind: "mountPath",
                    target,
                    currentPath,
                    currentAutoMount,
                    missing,
                    details,
                  })
                }
                onToggleAutoMount={(uuid, autoMount) =>
                  autoMountToggle.mutate({ uuid, autoMount })
                }
                pendingAutoMountUUID={
                  autoMountToggle.isPending
                    ? (autoMountToggle.variables?.uuid ?? null)
                    : null
                }
              />
            ))}
          </div>
        </section>
      </section>

      {dialog && (
        <OperationDialog
          dialog={dialog}
          pending={operation.isPending}
          onCancel={() => setDialog(null)}
          onSubmit={(next) => operation.mutate(next)}
        />
      )}

      {smartTarget && (
        <SmartDetailsDialog
          diskPath={smartTarget.path}
          solidState={smartTarget.solidState}
          onClose={() => setSmartTarget(null)}
        />
      )}

      <ToastStack
        rebootRequired={rebootRequired}
        onDismissReboot={() => setRebootRequired(false)}
        onRestartSystem={requestRestart}
        error={
          operation.isError
            ? operation.error
            : autoMountToggle.isError
              ? autoMountToggle.error
              : null
        }
        onDismissError={() => {
          operation.reset();
          autoMountToggle.reset();
        }}
      />
    </main>
  );
}
