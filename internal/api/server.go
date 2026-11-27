package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"github.com/sgpublic/simplefsmanager/internal/auth"
	"github.com/sgpublic/simplefsmanager/internal/buildinfo"
	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
	"github.com/sgpublic/simplefsmanager/internal/usb"
	"github.com/sgpublic/simplefsmanager/internal/volume"
)

type Health struct {
	Status string `json:"status" example:"ok" doc:"Service health status"`
}

type HealthOutput struct {
	Body Health
}

type BuildInfo struct {
	Version string `json:"version" example:"v0.1.0" doc:"Version of the running service"`
}

type BuildInfoOutput struct {
	Body BuildInfo
}

type OperationOutput struct {
	Body struct {
		Message        string `json:"message"`
		MountPath      string `json:"mountPath,omitempty"`
		UUID           string `json:"uuid,omitempty"`
		RebootRequired bool   `json:"rebootRequired,omitempty"`
	}
}

type errorPayload struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params,omitempty"`
}

type server struct {
	db         *store.Store
	logger     *slog.Logger
	disks      *storage.Manager
	diskCache  *storage.DiskCache
	volumes    *volume.Manager
	usbVolumes *usb.Manager
	auth       *auth.Service
	limiter    *auth.RateLimiter
}

func New(database *store.Store, logger *slog.Logger, disks *storage.Manager, volumes *volume.Manager, usbVolumes *usb.Manager, frontend http.Handler, caches ...*storage.DiskCache) http.Handler {
	if volumes == nil {
		volumes = volume.New(database, disks)
	}
	var diskCache *storage.DiskCache
	if len(caches) > 0 && caches[0] != nil {
		diskCache = caches[0]
	} else {
		diskCache = storage.NewDiskCache(disks.List)
	}
	srv := &server{
		db:         database,
		logger:     logger,
		disks:      disks,
		diskCache:  diskCache,
		volumes:    volumes,
		usbVolumes: usbVolumes,
		auth:       auth.New(database),
		limiter:    auth.NewRateLimiter(authRateLimitMax, authRateLimitWindow),
	}

	router := chi.NewRouter()
	router.Use(requestErrorLogger(logger))
	router.Use(recoverer(logger))
	router.Use(srv.auth.Middleware)

	srv.registerAuthRoutes(router)

	api := humachi.New(router, huma.DefaultConfig("SimpleFSManager API", buildinfo.Version))
	huma.Get(api, "/api/build-info", func(context.Context, *struct{}) (*BuildInfoOutput, error) {
		return &BuildInfoOutput{Body: BuildInfo{Version: buildinfo.Version}}, nil
	})
	huma.Get(api, "/api/health", func(context.Context, *struct{}) (*HealthOutput, error) {
		return &HealthOutput{Body: Health{Status: "ok"}}, nil
	})

	srv.registerDiskRoutes(api)
	srv.registerVolumeRoutes(api)
	srv.registerUSBRoutes(api)

	router.Handle("/*", frontend)
	return router
}

func confirm(target, value string) error {
	if target != value {
		return apperr.New(http.StatusBadRequest, apperr.CodeConfirmationFailed, fmt.Sprintf("confirmation must exactly match %s", target))
	}
	return nil
}

func operationError(logger *slog.Logger, operation string, err error, args ...any) error {
	status := apperr.Status(err)
	code := apperr.Code(err)
	if status >= http.StatusInternalServerError {
		logOperationError(logger, operation, err, args...)
	}
	return huma.NewError(status, code, err)
}

func operation(message, mountPath, uuid string, rebootRequired ...bool) *OperationOutput {
	output := &OperationOutput{}
	output.Body.Message = message
	output.Body.MountPath = mountPath
	output.Body.UUID = uuid
	if len(rebootRequired) > 0 {
		output.Body.RebootRequired = rebootRequired[0]
	}
	return output
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, errorPayload{
		Code:    apperr.Code(err),
		Message: err.Error(),
		Params:  apperr.Params(err),
	})
}

func (s *server) invalidateDiskCache() {
	if s.diskCache != nil {
		s.diskCache.Invalidate()
	}
}

func logOperationError(logger *slog.Logger, operation string, err error, args ...any) {
	logger.Error("operation failed", append([]any{"operation", operation, "error", err}, args...)...)
}

func logOperationSuccess(logger *slog.Logger, operation string, args ...any) {
	logger.Info("operation completed", append([]any{"operation", operation}, args...)...)
}
