# SimpleFSManager

Single-user Linux disk management UI. The first release is intentionally limited to physical disks, GPT partition tables, ext4 filesystems, and application-managed mounts at `/volN`.

## Current status

- Go HTTP service using Huma with a chi adapter
- SQLite state store using the pure-Go `modernc.org/sqlite` driver
- React, Vite, Tailwind, shadcn-compatible frontend setup
- Chinese and English UI through `react-i18next`, with browser detection and persisted language selection
- OpenAPI endpoint at `/openapi.json` and generated TypeScript API types
- Embedded production frontend
- Live physical disk and partition discovery through `lsblk --json`, with `blkid` fallback for UUID and filesystem type
- Mounted filesystem capacity through `unix.Statfs`
- GPT initialization and partition changes through `go-diskfs`, ext4/xfs formatting, mount/unmount, and `BLKRRPART` partition-table rereads

`GET /api/disks` reports the live topology. The WebUI exposes GPT initialization, partition creation/deletion, formatting, and mount/unmount with explicit target confirmation.

A disk is treated as the system disk when it contains a mounted partition whose UUID is not registered in this project's SQLite database. A whole-disk mount is also always treated as a system disk. System disks are never eligible for format or partition changes. This assumes, by design, that only the system disk is mounted through `/etc/fstab`.

The first successful mount registers its filesystem UUID in SQLite and allocates the next permanent `/volN` path. At startup and on udev events, only these registered UUIDs are restored; `/etc/fstab` is never modified or read.

## Development

Requirements: Go 1.26+, Node.js 24+, npm, and the Linux tools required by the implemented storage features.

`lsblk`, `blkid`, and `mkfs.ext4` are required for their respective features. XFS formatting also requires `mkfs.xfs`. The `go-udev` monitor is behind the `libudev` build tag because it needs CGO and libudev development headers:

```sh
go build -tags libudev ./cmd/simplefsmanager
```

The default build works without libudev but reports udev monitoring as unavailable.

## Authentication

On first access, an eligible local Linux user (`UID >= 1000`, non-root) authenticates through PAM and then creates a separate SimpleFSManager password. The project password is stored as an Argon2id hash in SQLite; the system password is not stored or changed. Later logins use only the project password.

Install PAM development headers before building and install the supplied PAM service file before first login:

```sh
sudo apt install libpam0g-dev
sudo install -m 644 deploy/simplefsmanager.pam /etc/pam.d/simplefsmanager
```

Sessions use HttpOnly, SameSite=Strict cookies. The default HTTP deployment is intentionally loopback-only; keep it that way until TLS is configured.

User-visible API errors are returned as stable error codes and translated by the frontend. Do not use raw system command, PAM, or database error text as a UI message.

Run the API:

```sh
go run ./cmd/simplefsmanager -data-dir ./var
```

In another terminal, run the frontend development server:

```sh
cd web
npm install
npm run dev
```

Build the embedded frontend, then the service:

```sh
cd web
npm run build
cd ..
go build -o simplefsmanager ./cmd/simplefsmanager
```

Regenerate frontend API types after changing Huma endpoints. The service must be running locally:

```sh
cd web
npm run api:generate
```

## Deployment

The example unit lives at `deploy/simplefsmanager.service`. It deliberately binds to loopback; put a TLS-enabled reverse proxy in front of it before exposing the UI on a network.

The service needs root privileges for storage operations. It has a single-administrator login, but keep it bound to loopback until TLS is configured for network exposure.
