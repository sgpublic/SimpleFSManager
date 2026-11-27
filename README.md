# SimpleFSManager

[English](README.en.md)

SimpleFSManager 是一个面向单管理员的 Linux 磁盘管理 Web 界面，用于管理物理磁盘、分区和应用挂载的文件系统。执行磁盘操作需要 root 权限。

## 功能

- 发现物理磁盘、分区和文件系统信息
- 初始化 GPT 分区表，创建和删除分区
- 格式化 ext4、xfs、btrfs 和 f2fs 文件系统
- 管理应用配置的内部卷挂载路径
- 自动挂载支持的 USB 存储分区，并支持手动挂载和卸载
- 查看 SMART 健康状态、温度和完整 SMART 数据
- 支持分区区域磁盘（zoned disk）
- 通过单管理员账户保护所有磁盘管理操作

## 使用

首次启动时，SimpleFSManager 会自动创建默认管理员账户（默认用户名 `admin`），并在控制台日志中输出一次性随机初始密码（可通过 `journalctl -u simplefsmanager` 查看）。也可以在首次启动前通过环境变量自定义初始凭据（仅首次初始化时生效）：

```sh
SFM_ADMIN_USERNAME=admin SFM_ADMIN_PASSWORD=your-strong-password simplefsmanager
```

登录后可在 Web 界面右上角点击“修改密码”更新管理员密码。

带标签的发布会在 GitHub Releases 中提供 `amd64` Debian 软件包。安装后服务会自动启用并启动：

```sh
sudo apt install ./simplefsmanager_*.deb
systemctl status simplefsmanager
```

服务默认监听 `0.0.0.0:7376`。在通过支持 TLS 的反向代理公开服务前，请使用防火墙限制访问，或通过 systemd 覆盖配置指定 `-listen 127.0.0.1:7376`。

登录 Web 界面后，可以执行 GPT 初始化、分区创建或删除、文件系统格式化、内部卷挂载或卸载，以及 USB 存储挂载或卸载。

## 构建与开发

需要 Go 1.26+、Node.js 24+、npm，以及功能所需的 Linux 工具。`lsblk`、`blkid`、`smartctl` 和 `mkfs.ext4` 分别用于磁盘发现、文件系统信息、SMART 数据和 ext4 格式化。XFS、btrfs 和 f2fs 格式化还分别需要 `mkfs.xfs`、`mkfs.btrfs` 和 `mkfs.f2fs`。

运行 API：

```sh
go run ./cmd/simplefsmanager -data-dir ./var
```

在另一个终端运行前端开发服务器：

```sh
cd web
npm install
npm run dev
```

构建嵌入式前端和服务：

```sh
make build
```

需要时可通过 `make build VERSION=v0.1.0` 覆盖自动检测的版本值。未安装 libudev 开发头文件时，默认构建仍可使用；如需 udev 监控，请使用：

```sh
go build -tags libudev ./cmd/simplefsmanager
```

构建嵌入式前端后运行测试：

```sh
make test
```

修改 Huma 端点后，可在服务本机运行时重新生成前端 API 类型：

```sh
cd web
npm run api:generate
```

## 技术栈

- 后端：Go、Huma、chi
- 数据库：SQLite
- 前端：React、Vite、Tailwind
