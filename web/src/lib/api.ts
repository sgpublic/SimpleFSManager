import { api } from "../api/client";
import type {
  Disk,
  FileSystemType,
  OperationResult,
  Partition,
} from "../api/schema";

export type { Disk, FileSystemType, OperationResult, Partition };

export type AuthStatus = {
  authenticated: boolean;
  username?: string;
};

export type BuildInfo = {
  version: string;
};

export type Operation =
  | { path: "/api/disks/gpt"; body: { diskPath: string; confirm: string } }
  | { path: "/api/disks/reclaim"; body: { diskPath: string; confirm: string } }
  | {
      path: "/api/disks/format-f2fs";
      body: { diskPath: string; confirm: string };
    }
  | {
      path: "/api/partitions";
      body: {
        diskPath: string;
        sizeBytes: number;
        useLargestFree: boolean;
        name: string;
        confirm: string;
      };
    }
  | {
      path: "/api/partitions/delete";
      body: { diskPath: string; partitionNumber: number; confirm: string };
    }
  | {
      path: "/api/partitions/format";
      body: {
        partitionPath: string;
        fileSystem: FileSystemType;
        confirm: string;
      };
    }
  | {
      path: "/api/volumes/mount";
      body: { partitionPath: string; confirm: string };
    }
  | {
      path: "/api/volumes/mount-path";
      body: {
        partitionPath?: string;
        partitionUUID?: string;
        mountPath: string;
        autoMount?: boolean;
        confirm: string;
      };
    }
  | {
      path: "/api/volumes/auto-mount";
      body: { uuid: string; autoMount: boolean };
    }
  | { path: "/api/volumes/unmount"; body: { uuid: string; confirm: string } }
  | {
      path: "/api/usb/mount";
      body: { partitionPath: string; confirm: string };
    }
  | {
      path: "/api/usb/unmount";
      body: { partitionPath: string; confirm: string };
    }
  | { path: "/api/system/reboot"; body: { confirm: string } };

type RawErrorPayload = {
  code?: string;
  message?: string;
  detail?: string;
  errors?: Array<{ message?: string }>;
  params?: Record<string, unknown>;
} | null;

export class ApiError extends Error {
  constructor(
    public code: string,
    public message = code,
    public params: Record<string, unknown> = {},
    public status?: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

let onUnauthorized: (() => void) | null = null;

export function setUnauthorizedHandler(handler: (() => void) | null) {
  onUnauthorized = handler;
}

function toApiError(
  payload: RawErrorPayload,
  fallbackCode: string,
  status?: number,
): ApiError {
  const code = payload?.code ?? payload?.detail ?? fallbackCode;
  const message =
    payload?.message ??
    payload?.errors?.find((item) => item.message)?.message ??
    payload?.detail ??
    code;
  return new ApiError(code, message, payload?.params ?? {}, status);
}

function handleUnauthorized(status: number, path: string) {
  if (
    status === 401 &&
    path !== "/api/auth/login" &&
    path !== "/api/auth/password" &&
    onUnauthorized
  ) {
    onUnauthorized();
  }
}

export async function requestJSON<T = unknown>(
  path: string,
  body?: Record<string, unknown>,
): Promise<T> {
  const response = await fetch(path, {
    method: body ? "POST" : "GET",
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!response.ok) {
    handleUnauthorized(response.status, path);
    const payload = (await response.json().catch(() => null)) as RawErrorPayload;
    throw toApiError(payload, "request_failed", response.status);
  }
  return response.status === 204
    ? (undefined as T)
    : (response.json() as Promise<T>);
}

export type DiskSnapshot = {
  disks: Disk[];
  updatedAt?: string;
};

export async function fetchDisks(forceRefresh = false): Promise<DiskSnapshot> {
  const { data, error, response } = await api.GET(
    "/api/disks",
    forceRefresh ? { params: { query: { refresh: true } } } : undefined,
  );
  if (error || !data) {
    handleUnauthorized(response.status, "/api/disks");
    throw toApiError(
      (error as RawErrorPayload) ?? null,
      "request_failed",
      response.status,
    );
  }
  return { disks: data.disks, updatedAt: data.updatedAt };
}

export async function fetchSmartDetails(
  diskPath: string,
): Promise<Record<string, unknown>> {
  const { data, error, response } = await api.GET("/api/disks/smart", {
    params: { query: { diskPath } },
  });
  if (error || !data) {
    handleUnauthorized(response.status, "/api/disks/smart");
    throw toApiError(
      (error as RawErrorPayload) ?? null,
      "smart_query_failed",
      response.status,
    );
  }
  return data.data;
}

export async function executeOperation(
  operation: Operation,
): Promise<OperationResult> {
  return requestJSON<OperationResult>(
    operation.path,
    operation.body as Record<string, unknown>,
  );
}
