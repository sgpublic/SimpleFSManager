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

USB storage (`lsblk TRAN=usb`) uses a separate transient mount space. Attached devices receive insertion-order letters and supported ext4/xfs partitions are automatically mounted at `/usb<device-letter><partition-letter>`, for example `/usbaa` and `/usbab`. Removing a USB device releases its letter for the next device; USB storage only supports mount and unmount.

Manually unmounting a USB partition suppresses its automatic mount for the current insertion cycle. Mounting it again or physically removing the device clears that suppression.

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

Sessions use HttpOnly, SameSite=Strict cookies. Restrict network access until TLS is configured.

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

Build the embedded frontend and service. The binary version comes from the
nearest Git tag plus its commit distance; an untagged checkout uses its commit
ID and a dirty checkout includes the `-dirty` suffix:

```sh
make build
```

Override the detected value with `make build VERSION=v0.1.0` when needed.

Regenerate frontend API types after changing Huma endpoints. The service must be running locally:

```sh
cd web
npm run api:generate
```

## Deployment

Tagged releases publish an `amd64` Debian package on GitHub Releases. Install it with:

```sh
sudo apt install ./simplefsmanager_*.deb
systemctl status simplefsmanager
```

The package installs the service unit and PAM configuration, then enables and starts the service. Removing the package stops and disables the service, but preserves `/var/lib/simplefsmanager`.

The example unit lives at `deploy/simplefsmanager.service`. The service defaults to listening on `0.0.0.0:7376`; restrict it with a firewall or add a systemd override with `-listen 127.0.0.1:7376` before exposing the UI through a TLS-enabled reverse proxy.

The service needs root privileges for storage operations. It has a single-administrator login, but restrict network access until TLS is configured for network exposure.
