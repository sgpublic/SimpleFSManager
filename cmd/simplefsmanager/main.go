package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sgpublic/simplefsmanager/internal/api"
	"github.com/sgpublic/simplefsmanager/internal/auth"
	"github.com/sgpublic/simplefsmanager/internal/buildinfo"
	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
	"github.com/sgpublic/simplefsmanager/internal/usb"
	"github.com/sgpublic/simplefsmanager/internal/volume"
	"github.com/sgpublic/simplefsmanager/internal/web"
)

const (
	udevDebounceDelay      = 500 * time.Millisecond
	sessionCleanupInterval = time.Hour
)

func main() {
	var (
		address     = flag.String("listen", "0.0.0.0:7376", "HTTP listen address")
		dataDir     = flag.String("data-dir", "/var/lib/simplefsmanager", "persistent data directory")
		showHelp    = flag.Bool("help", false, "show usage")
		showVersion = flag.Bool("v", false, "show version")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}
	if *showHelp {
		flag.PrintDefaults()
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		logger.Error("create data directory", "error", err)
		os.Exit(1)
	}
	logger.Info("data directory ready", "path", *dataDir)

	db, err := store.Open(filepath.Join(*dataDir, "simplefsmanager.db"))
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("close database", "error", err)
		}
	}()
	logger.Info("database opened", "path", filepath.Join(*dataDir, "simplefsmanager.db"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	initResult, err := auth.New(db).EnsureAdministrator(ctx, auth.BootstrapOptions{
		Username: os.Getenv("SFM_ADMIN_USERNAME"),
		Password: os.Getenv("SFM_ADMIN_PASSWORD"),
	})
	if err != nil {
		logger.Error("initialize administrator", "error", err)
		os.Exit(1)
	}
	if initResult.GeneratedPassword != "" {
		logger.Info("initial administrator created; please change this password after signing in", "username", initResult.Username, "password", initResult.GeneratedPassword)
	} else if initResult.Created {
		logger.Info("initial administrator created from environment", "username", initResult.Username)
	}

	if err := db.DeleteExpiredSessions(ctx, time.Now()); err != nil {
		logger.Error("clean expired sessions", "error", err)
	}

	disks := storage.New(db.IsManagedUUID)
	volumes := volume.New(db, disks)
	usbVolumes := usb.New(disks)
	if err := volumes.Recover(ctx); err != nil {
		logger.Error("restore managed volumes", "error", err)
	} else {
		logger.Info("managed volumes restored")
	}
	if err := usbVolumes.Reconcile(ctx, ""); err != nil {
		logger.Error("mount USB volumes", "error", err)
	} else {
		logger.Info("USB volumes reconciled")
	}

	diskCache := storage.NewDiskCache(disks.List)
	diskCache.Start(ctx)

	reconciler := newTopologyReconciler(udevDebounceDelay, func(runCtx context.Context, addedDisks []string) {
		if err := volumes.Recover(runCtx); err != nil {
			logger.Error("restore managed volumes after udev event", "error", err)
		} else {
			logger.Info("managed volumes restored after udev event")
		}
		if len(addedDisks) == 0 {
			if err := usbVolumes.Reconcile(runCtx, ""); err != nil {
				logger.Error("mount USB volumes after udev event", "error", err)
			} else {
				logger.Info("USB volumes reconciled after udev event")
			}
			diskCache.TriggerRefresh()
			return
		}
		for _, addedDiskPath := range addedDisks {
			if err := usbVolumes.Reconcile(runCtx, addedDiskPath); err != nil {
				logger.Error("mount USB volumes after udev event", "disk", addedDiskPath, "error", err)
			} else {
				logger.Info("USB volumes reconciled after udev event", "disk", addedDiskPath)
			}
		}
		diskCache.TriggerRefresh()
	})
	defer reconciler.Stop()

	go func() {
		if err := storage.WatchUdev(ctx, func(event storage.UdevEvent) {
			logger.Info("block device topology changed", "action", event.Action, "devnode", event.Devnode, "devtype", event.Devtype)
			reconciler.Notify(ctx, event)
		}); err != nil && !errors.Is(err, storage.ErrUdevUnavailable) {
			logger.Error("watch udev", "error", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(sessionCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := db.DeleteExpiredSessions(ctx, now); err != nil {
					logger.Error("clean expired sessions", "error", err)
				}
			}
		}
	}()

	handler := api.New(db, logger, disks, volumes, usbVolumes, web.Handler(), diskCache)
	server := &http.Server{
		Addr:              *address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		logger.Error("listen HTTP", "address", *address, "error", err)
		os.Exit(1)
	}

	go func() {
		logger.Info("SimpleFSManager listening", "address", *address)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve HTTP", "error", err)
			os.Exit(1)
		}
	}()
	if err := notifySystemd("READY=1"); err != nil {
		logger.Error("notify systemd that service is ready", "error", err)
		os.Exit(1)
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown HTTP server", "error", err)
	}
}
