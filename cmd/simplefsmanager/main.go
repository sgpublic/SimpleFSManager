package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sgpublic/simplefsmanager/internal/api"
	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
	"github.com/sgpublic/simplefsmanager/internal/volume"
	"github.com/sgpublic/simplefsmanager/internal/web"
)

func main() {
	var (
		address  = flag.String("listen", "0.0.0.0:7376", "HTTP listen address")
		dataDir  = flag.String("data-dir", "/var/lib/simplefsmanager", "persistent data directory")
		showHelp = flag.Bool("help", false, "show usage")
	)
	flag.Parse()
	if *showHelp {
		flag.PrintDefaults()
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		logger.Error("create data directory", "error", err)
		os.Exit(1)
	}

	db, err := store.Open(filepath.Join(*dataDir, "simplefsmanager.db"))
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	disks := storage.New(db.IsManagedUUID)
	volumes := volume.New(db, disks)
	if err := volumes.Recover(ctx); err != nil {
		logger.Error("restore managed volumes", "error", err)
	}
	go func() {
		if err := storage.WatchUdev(ctx, func() {
			logger.Info("block device topology changed")
			go func() {
				if err := volumes.Recover(ctx); err != nil {
					logger.Error("restore managed volumes after udev event", "error", err)
				}
			}()
		}); err != nil && !errors.Is(err, storage.ErrUdevUnavailable) {
			logger.Error("watch udev", "error", err)
		}
	}()

	handler := api.New(db, logger, disks, web.Handler())
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
