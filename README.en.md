# SimpleFSManager

[中文](README.md)

SimpleFSManager is a single-administrator Linux disk-management web UI for physical disks, partitions, and application-mounted filesystems. Disk operations require root privileges.

## Features

- Discover physical disks, partitions, and filesystem information
- Initialize GPT partition tables and create or delete partitions
- Format ext4, xfs, btrfs, and f2fs filesystems
- Manage application-configured mount paths for internal volumes
- Automatically mount supported USB storage partitions, with manual mount and unmount controls
- View SMART health, temperature, and complete SMART data
- Support zoned disks
- Protect all disk-management operations with a single administrator account

## Usage

On first access, authenticate through PAM with an eligible local Linux user and set a separate SimpleFSManager project password. Later logins require the bound username and project password.

Tagged releases provide an `amd64` Debian package on GitHub Releases. Installing it enables and starts the service:

```sh
sudo apt install ./simplefsmanager_*.deb
systemctl status simplefsmanager
```

The service listens on `0.0.0.0:7376` by default. Restrict access with a firewall or set a systemd override with `-listen 127.0.0.1:7376` before exposing it through a TLS-enabled reverse proxy.

After signing in to the web UI, you can initialize GPT, create or delete partitions, format filesystems, mount or unmount internal volumes, and mount or unmount USB storage.

## Build and Development

Go 1.26+, Node.js 24+, npm, and the required Linux tools are needed. `lsblk`, `blkid`, `smartctl`, and `mkfs.ext4` are used for disk discovery, filesystem information, SMART data, and ext4 formatting. XFS, btrfs, and f2fs formatting additionally require `mkfs.xfs`, `mkfs.btrfs`, and `mkfs.f2fs`.

Install PAM development headers before building, and install the supplied PAM service file before the first login:

```sh
sudo apt install libpam0g-dev
sudo install -m 644 deploy/simplefsmanager.pam /etc/pam.d/simplefsmanager
```

Run the API:

```sh
go run ./cmd/simplefsmanager -data-dir ./var
```

Run the frontend development server in another terminal:

```sh
cd web
npm install
npm run dev
```

Build the embedded frontend and service:

```sh
make build
```

Override the detected version with `make build VERSION=v0.1.0` when needed. The default build works without libudev development headers; build with udev monitoring using:

```sh
go build -tags libudev ./cmd/simplefsmanager
```

Run the tests after building the embedded frontend:

```sh
make test
```

After changing Huma endpoints, regenerate frontend API types while the service is running locally:

```sh
cd web
npm run api:generate
```

## Technology Stack

- Backend: Go, Huma, chi
- Database: SQLite
- Frontend: React, Vite, Tailwind
