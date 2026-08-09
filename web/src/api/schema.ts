// This placeholder is overwritten by `npm run api:generate` while the Go API is running.
export interface paths {
  "/api/health": { get: { responses: { 200: { content: { "application/json": { status: string } } } } } };
  "/api/disks": { get: { responses: { 200: { content: { "application/json": { disks: Array<{ path: string; name: string; model: string; serial: string; sizeBytes: number; partitioning: string; transport: string; usb: boolean; protected: boolean; system: boolean; mountpoints: string[]; partitions: Array<{ path: string; name: string; number: number; sizeBytes: number; fileSystem: string; uuid: string; mountpoints: string[] }> }> } } } } } };
}
