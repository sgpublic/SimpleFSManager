package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sgpublic/simplefsmanager/internal/api"
	"github.com/sgpublic/simplefsmanager/internal/buildinfo"
	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
	"github.com/sgpublic/simplefsmanager/internal/usb"
	"github.com/sgpublic/simplefsmanager/internal/volume"
	"github.com/sgpublic/simplefsmanager/internal/web"
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
	go func() {
		if err := storage.WatchUdev(ctx, func(event storage.UdevEvent) {
			logger.Info("block device topology changed")
			go func() {
				time.Sleep(500 * time.Millisecond)
				if err := volumes.Recover(ctx); err != nil {
					logger.Error("restore managed volumes after udev event", "error", err)
				} else {
					logger.Info("managed volumes restored after udev event")
				}
				addedDiskPath := ""
				if event.Action == "add" && event.Devtype == "disk" {
					addedDiskPath = event.Devnode
				}
				if err := usbVolumes.Reconcile(ctx, addedDiskPath); err != nil {
					logger.Error("mount USB volumes after udev event", "error", err)
				} else {
					logger.Info("USB volumes reconciled after udev event")
				}
			}()
		}); err != nil && !errors.Is(err, storage.ErrUdevUnavailable) {
			logger.Error("watch udev", "error", err)
		}
	}()

	handler := api.New(db, logger, disks, usbVolumes, web.Handler())
	server := &http.Server{
		Addr:              *address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("SimpleFSManager listening", "address", *address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve HTTP", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown HTTP server", "error", err)
	}
}
