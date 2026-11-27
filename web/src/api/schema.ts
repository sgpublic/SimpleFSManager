// This placeholder is overwritten by `npm run api:generate` while the Go API is running.
export interface FileSystemUsage {
  totalBytes: number;
  usedBytes: number;
  availableBytes: number;
}

export interface Partition {
  path: string;
  name: string;
  number: number;
  sizeBytes: number;
  fileSystem: string;
  uuid: string;
  mountPath?: string;
  mountpoints: string[];
  registered: boolean;
  autoMount?: boolean;
  missing: boolean;
  zoned?: string;
  zoneSizeBytes?: number;
  zoneWriteGranularityBytes?: number;
  usage?: FileSystemUsage;
}

export interface Disk {
  path: string;
  name: string;
  model: string;
  serial: string;
  sizeBytes: number;
  partitioning: string;
  transport: string;
  usb: boolean;
  protected: boolean;
  system: boolean;
  reclaimable: boolean;
  missing: boolean;
  temperatureCelsius?: number;
  smartHealth?: boolean;
  solidState?: boolean;
  dataWrittenBytes?: number;
  powerOnHours?: number;
  mountpoints: string[];
  zoned?: string;
  zoneSizeBytes?: number;
  zoneWriteGranularityBytes?: number;
  partitions: Partition[];
}

export interface OperationResult {
  message: string;
  mountPath?: string;
  uuid?: string;
  rebootRequired?: boolean;
}

export type FileSystemType = "ext4" | "xfs" | "btrfs" | "f2fs";

export interface ProblemDetail {
  title?: string;
  status?: number;
  detail?: string;
  code?: string;
  message?: string;
  errors?: Array<{ message?: string; location?: string }>;
  params?: Record<string, unknown>;
}

interface DefaultErrorResponse {
  default: {
    content: {
      "application/problem+json": ProblemDetail;
    };
  };
}

export interface paths {
  "/api/health": {
    get: {
      responses: DefaultErrorResponse & {
        200: { content: { "application/json": { status: string } } };
      };
    };
  };
  "/api/build-info": {
    get: {
      responses: DefaultErrorResponse & {
        200: { content: { "application/json": { version: string } } };
      };
    };
  };
  "/api/disks": {
    get: {
      parameters?: { query?: { refresh?: boolean } };
      responses: DefaultErrorResponse & {
        200: {
          content: {
            "application/json": { disks: Disk[]; updatedAt?: string };
          };
        };
      };
    };
  };
  "/api/disks/smart": {
    get: {
      parameters: { query: { diskPath: string } };
      responses: DefaultErrorResponse & {
        200: {
          content: {
            "application/json": { data: Record<string, unknown> };
          };
        };
      };
    };
  };
  "/api/disks/gpt": {
    post: {
      requestBody: {
        content: {
          "application/json": { diskPath: string; confirm: string };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/disks/reclaim": {
    post: {
      requestBody: {
        content: {
          "application/json": { diskPath: string; confirm: string };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/disks/format-f2fs": {
    post: {
      requestBody: {
        content: {
          "application/json": { diskPath: string; confirm: string };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/partitions": {
    post: {
      requestBody: {
        content: {
          "application/json": {
            diskPath: string;
            sizeBytes: number;
            useLargestFree: boolean;
            name: string;
            confirm: string;
          };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/partitions/delete": {
    post: {
      requestBody: {
        content: {
          "application/json": {
            diskPath: string;
            partitionNumber: number;
            confirm: string;
          };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/partitions/format": {
    post: {
      requestBody: {
        content: {
          "application/json": {
            partitionPath: string;
            fileSystem: FileSystemType;
            confirm: string;
          };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/volumes/mount": {
    post: {
      requestBody: {
        content: {
          "application/json": { partitionPath: string; confirm: string };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/volumes/mount-path": {
    post: {
      requestBody: {
        content: {
          "application/json": {
            partitionPath?: string;
            partitionUUID?: string;
            mountPath: string;
            autoMount?: boolean;
            confirm: string;
          };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/volumes/auto-mount": {
    post: {
      requestBody: {
        content: {
          "application/json": { uuid: string; autoMount: boolean };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/volumes/unmount": {
    post: {
      requestBody: {
        content: {
          "application/json": { uuid: string; confirm: string };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/usb/mount": {
    post: {
      requestBody: {
        content: {
          "application/json": { partitionPath: string; confirm: string };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/usb/unmount": {
    post: {
      requestBody: {
        content: {
          "application/json": { partitionPath: string; confirm: string };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
  "/api/system/reboot": {
    post: {
      requestBody: {
        content: {
          "application/json": { confirm: string };
        };
      };
      responses: {
        200: { content: { "application/json": OperationResult } };
      };
    };
  };
}
