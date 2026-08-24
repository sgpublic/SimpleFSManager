package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
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

type DiskListOutput struct {
	Body struct {
		Disks []storage.Disk `json:"disks"`
	}
}

type DiskConfirmation struct {
	DiskPath string `json:"diskPath" minLength:"1"`
	Confirm  string `json:"confirm" minLength:"1"`
}

type InitializeGPTInput struct {
	Body DiskConfirmation
}

type ReclaimDiskInput struct {
	Body DiskConfirmation
}

type RebootInput struct {
	Body struct {
		Confirm string `json:"confirm" minLength:"1"`
	}
}

type CreatePartitionInput struct {
	Body struct {
		DiskConfirmation
		SizeBytes      uint64 `json:"sizeBytes"`
		UseLargestFree bool   `json:"useLargestFree"`
		Name           string `json:"name" maxLength:"36"`
	}
}

type DeletePartitionInput struct {
	Body struct {
		DiskConfirmation
		PartitionNumber int `json:"partitionNumber" minimum:"1"`
	}
}

type FormatInput struct {
	Body struct {
		PartitionPath string `json:"partitionPath" minLength:"1"`
		FileSystem    string `json:"fileSystem" enum:"ext4,xfs,btrfs,f2fs"`
		Confirm       string `json:"confirm" minLength:"1"`
	}
}

type FormatWholeDiskInput struct {
	Body DiskConfirmation
}

type MountInput struct {
	Body struct {
		PartitionPath string `json:"partitionPath" minLength:"1"`
		Confirm       string `json:"confirm" minLength:"1"`
	}
}

type MountPathInput struct {
	Body struct {
		PartitionPath string `json:"partitionPath"`
		PartitionUUID string `json:"partitionUUID"`
		MountPath     string `json:"mountPath" minLength:"1"`
		Confirm       string `json:"confirm" minLength:"1"`
	}
}

type UnmountInput struct {
	Body struct {
		UUID    string `json:"uuid" minLength:"1"`
		Confirm string `json:"confirm" minLength:"1"`
	}
}

type OperationOutput struct {
	Body struct {
		Message        string `json:"message"`
		MountPath      string `json:"mountPath,omitempty"`
		UUID           string `json:"uuid,omitempty"`
		RebootRequired bool   `json:"rebootRequired,omitempty"`
	}
}

type bootstrapRequest struct {
	Username        string `json:"username"`
	SystemPassword  string `json:"systemPassword"`
	ProjectPassword string `json:"projectPassword"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func New(database *store.Store, logger *slog.Logger, disks *storage.Manager, usbVolumes *usb.Manager, frontend http.Handler) http.Handler {
	router := chi.NewRouter()
	router.Use(recoverer(logger))
	authentication := auth.New(database)
	router.Use(authentication.Middleware)
	router.Get("/api/auth/status", func(writer http.ResponseWriter, request *http.Request) {
		status, err := authentication.Status(request.Context(), request)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, err)
			return
		}
		writeJSON(writer, http.StatusOK, status)
	})
	router.Post("/api/auth/bootstrap", func(writer http.ResponseWriter, request *http.Request) {
		var input bootstrapRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			writeError(writer, http.StatusBadRequest, fmt.Errorf("invalid request body"))
			return
		}
		token, err := authentication.Bootstrap(request.Context(), input.Username, input.SystemPassword, input.ProjectPassword)
		if err != nil {
			writeError(writer, http.StatusUnauthorized, err)
			return
		}
		http.SetCookie(writer, auth.SessionCookie(token))
		writeJSON(writer, http.StatusCreated, auth.Status{Authenticated: true, Username: input.Username})
	})
	router.Post("/api/auth/login", func(writer http.ResponseWriter, request *http.Request) {
		var input loginRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			writeError(writer, http.StatusBadRequest, fmt.Errorf("invalid request body"))
			return
		}
		token, err := authentication.Login(request.Context(), input.Username, input.Password)
		if err != nil {
			writeError(writer, http.StatusUnauthorized, err)
			return
		}
		http.SetCookie(writer, auth.SessionCookie(token))
		writeJSON(writer, http.StatusOK, auth.Status{Authenticated: true, Username: input.Username})
	})
	router.Post("/api/auth/logout", func(writer http.ResponseWriter, request *http.Request) {
		authentication.Logout(request.Context(), request)
		http.SetCookie(writer, auth.ExpiredSessionCookie())
		writer.WriteHeader(http.StatusNoContent)
	})
	api := humachi.New(router, huma.DefaultConfig("SimpleFSManager API", buildinfo.Version))

	huma.Get(api, "/api/build-info", func(context.Context, *struct{}) (*BuildInfoOutput, error) {
		return &BuildInfoOutput{Body: BuildInfo{Version: buildinfo.Version}}, nil
	})
	huma.Get(api, "/api/health", func(context.Context, *struct{}) (*HealthOutput, error) {
		return &HealthOutput{Body: Health{Status: "ok"}}, nil
	})
	huma.Get(api, "/api/disks", func(ctx context.Context, _ *struct{}) (*DiskListOutput, error) {
		found, err := disks.List(ctx)
		if err != nil {
			return nil, err
		}
		volumes, err := database.Volumes(ctx)
		if err != nil {
			return nil, err
		}
		found, err = mergeManagedVolumes(ctx, found, volumes, database)
		if err != nil {
			return nil, err
		}
		output := &DiskListOutput{}
		output.Body.Disks = found
		return output, nil
	})
	volumes := volume.New(database, disks)

	huma.Post(api, "/api/disks/gpt", func(ctx context.Context, input *InitializeGPTInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		disk, err := diskByPath(ctx, disks, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(logger, "initialize GPT", err, "disk", input.Body.DiskPath)
		}
		rebootRequired, err := disks.InitializeGPT(ctx, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(logger, "initialize GPT", err, "disk", input.Body.DiskPath)
		}
		identity := disk.Serial
		if identity == "" {
			identity = disk.Path
		}
		if err := database.DeleteVolumesBySerial(ctx, identity); err != nil {
			logOperationError(logger, "remove volumes after GPT initialization", err, "disk", input.Body.DiskPath)
			return nil, err
		}
		return operation("initialized empty GPT", "", "", rebootRequired), nil
	})

	huma.Post(api, "/api/disks/reclaim", func(ctx context.Context, input *ReclaimDiskInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		disk, err := diskByPath(ctx, disks, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(logger, "reclaim disk", err, "disk", input.Body.DiskPath)
		}
		rebootRequired, err := disks.Reclaim(ctx, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(logger, "reclaim disk", err, "disk", input.Body.DiskPath)
		}
		identity := disk.Serial
		if identity == "" {
			identity = disk.Path
		}
		if err := database.DeleteVolumesBySerial(ctx, identity); err != nil {
			logOperationError(logger, "remove volumes after reclaiming disk", err, "disk", input.Body.DiskPath)
			return nil, err
		}
		return operation("reclaimed disk with empty GPT", "", "", rebootRequired), nil
	})

	huma.Post(api, "/api/system/reboot", func(ctx context.Context, input *RebootInput) (*OperationOutput, error) {
		if err := confirm("system", input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		if err := disks.Reboot(ctx); err != nil {
			return nil, operationError(logger, "restart system", err)
		}
		return operation("restarting system", "", "", false), nil
	})

	huma.Post(api, "/api/partitions", func(ctx context.Context, input *CreatePartitionInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		index, err := disks.CreatePartition(ctx, input.Body.DiskPath, input.Body.SizeBytes, input.Body.UseLargestFree, input.Body.Name)
		if err != nil {
			return nil, badRequest(err)
		}
		return operation(fmt.Sprintf("created partition %d", index), "", ""), nil
	})

	huma.Post(api, "/api/partitions/delete", func(ctx context.Context, input *DeletePartitionInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		disk, partition, err := partitionByNumber(ctx, disks, input.Body.DiskPath, input.Body.PartitionNumber)
		if err != nil {
			return nil, operationError(logger, "delete partition", err, "disk", input.Body.DiskPath, "partitionNumber", input.Body.PartitionNumber)
		}
		if err := disks.DeletePartition(ctx, disk.Path, input.Body.PartitionNumber); err != nil {
			return nil, operationError(logger, "delete partition", err, "disk", disk.Path, "partitionNumber", input.Body.PartitionNumber)
		}
		if err := database.DeleteVolumeIfExists(ctx, partition.UUID); err != nil {
			logOperationError(logger, "remove volume after partition deletion", err, "disk", disk.Path, "partition", partition.Path)
			return nil, err
		}
		return operation("deleted partition", "", ""), nil
	})

	huma.Post(api, "/api/partitions/format", func(ctx context.Context, input *FormatInput) (*OperationOutput, error) {
		if err := confirm(input.Body.PartitionPath, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		_, partition, err := disks.Partition(ctx, input.Body.PartitionPath)
		if err != nil {
			return nil, operationError(logger, "format partition", err, "partition", input.Body.PartitionPath, "filesystem", input.Body.FileSystem)
		}
		if err := disks.Format(ctx, input.Body.PartitionPath, input.Body.FileSystem); err != nil {
			return nil, operationError(logger, "format partition", err, "partition", input.Body.PartitionPath, "filesystem", input.Body.FileSystem)
		}
		if err := database.DeleteVolumeIfExists(ctx, partition.UUID); err != nil {
			logOperationError(logger, "remove volume after formatting", err, "partition", partition.Path)
			return nil, err
		}
		return operation("formatted partition", "", ""), nil
	})
	// Host-managed zoned disks cannot expose partitions to Linux. They use one
	// whole-disk F2FS volume instead.
	huma.Post(api, "/api/disks/format-f2fs", func(ctx context.Context, input *FormatWholeDiskInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		disk, err := diskByPath(ctx, disks, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(logger, "format host-managed disk", err, "disk", input.Body.DiskPath)
		}
		var previousUUID string
		for _, partition := range disk.Partitions {
			if partition.Path == disk.Path {
				previousUUID = partition.UUID
				break
			}
		}
		if err := disks.FormatWholeDisk(ctx, disk.Path); err != nil {
			return nil, operationError(logger, "format host-managed disk", err, "disk", disk.Path, "filesystem", "f2fs")
		}
		if err := database.DeleteVolumeIfExists(ctx, previousUUID); err != nil {
			logOperationError(logger, "remove volume after whole-disk format", err, "disk", disk.Path)
			return nil, err
		}
		return operation("formatted host-managed disk as f2fs", "", ""), nil
	})

	huma.Post(api, "/api/volumes/mount", func(ctx context.Context, input *MountInput) (*OperationOutput, error) {
		if err := confirm(input.Body.PartitionPath, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		managed, err := volumes.Mount(ctx, input.Body.PartitionPath)
		if err != nil {
			return nil, badRequest(err)
		}
		return operation("mounted volume", managed.MountPath, managed.UUID), nil
	})

	huma.Post(api, "/api/volumes/mount-path", func(ctx context.Context, input *MountPathInput) (*OperationOutput, error) {
		target := input.Body.PartitionPath
		if input.Body.PartitionUUID != "" {
			target = input.Body.PartitionUUID
		}
		if target == "" {
			return nil, badRequest(fmt.Errorf("partition path or UUID is required"))
		}
		if err := confirm(target, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		var managed store.Volume
		var err error
		if input.Body.PartitionUUID != "" {
			managed, err = volumes.ConfigureMissingMountPath(ctx, input.Body.PartitionUUID, input.Body.MountPath)
		} else {
			managed, err = volumes.ConfigureMountPath(ctx, input.Body.PartitionPath, input.Body.MountPath)
		}
		if err != nil {
			return nil, operationError(logger, "configure volume mount path", err, "partition", target, "mountPath", input.Body.MountPath)
		}
		return operation("configured volume mount path", managed.MountPath, managed.UUID), nil
	})

	huma.Post(api, "/api/volumes/unmount", func(ctx context.Context, input *UnmountInput) (*OperationOutput, error) {
		if err := confirm(input.Body.UUID, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		managed, err := volumes.Unmount(ctx, input.Body.UUID)
		if err != nil {
			return nil, badRequest(err)
		}
		return operation("unmounted volume", managed.MountPath, managed.UUID), nil
	})

	huma.Post(api, "/api/usb/mount", func(ctx context.Context, input *MountInput) (*OperationOutput, error) {
		if err := confirm(input.Body.PartitionPath, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		target, err := usbVolumes.Mount(ctx, input.Body.PartitionPath)
		if err != nil {
			return nil, badRequest(err)
		}
		return operation("mounted USB partition", target, ""), nil
	})

	huma.Post(api, "/api/usb/unmount", func(ctx context.Context, input *MountInput) (*OperationOutput, error) {
		if err := confirm(input.Body.PartitionPath, input.Body.Confirm); err != nil {
			return nil, badRequest(err)
		}
		target, err := usbVolumes.Unmount(ctx, input.Body.PartitionPath)
		if err != nil {
			return nil, badRequest(err)
		}
		return operation("unmounted USB partition", target, ""), nil
	})

	router.Handle("/*", frontend)
	return router
}

func mergeManagedVolumes(ctx context.Context, disks []storage.Disk, volumes []store.Volume, database *store.Store) ([]storage.Disk, error) {
	seenVolumes := make(map[string]bool, len(volumes))
	for diskIndex := range disks {
		for partitionIndex := range disks[diskIndex].Partitions {
			partition := &disks[diskIndex].Partitions[partitionIndex]
			if partition.UUID == "" {
				continue
			}
			volume, err := database.VolumeByUUID(ctx, partition.UUID)
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					continue
				}
				return nil, err
			}
			partition.MountPath = volume.MountPath
			partition.Registered = true
			seenVolumes[volume.UUID] = true
		}
	}
	for _, volume := range volumes {
		if seenVolumes[volume.UUID] {
			continue
		}
		disks = append(disks, storage.Disk{
			Name:      "registered-volume",
			Serial:    volume.DeviceSerial,
			Missing:   true,
			Protected: true,
			Partitions: []storage.Partition{{
				Name:       fmt.Sprintf("partition-%d", volume.PartitionNum),
				Number:     volume.PartitionNum,
				UUID:       volume.UUID,
				MountPath:  volume.MountPath,
				Registered: true,
				Missing:    true,
			}},
		})
	}
	return disks, nil
}

func confirm(target, value string) error {
	if target != value {
		return fmt.Errorf("confirmation must exactly match %s", target)
	}
	return nil
}

func badRequest(err error) error {
	return huma.Error400BadRequest(errorCode(err))
}

func operationError(logger *slog.Logger, operation string, err error, args ...any) error {
	if errorCode(err) == "internal_error" {
		logOperationError(logger, operation, err, args...)
	}
	return badRequest(err)
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

func diskByPath(ctx context.Context, disks *storage.Manager, path string) (storage.Disk, error) {
	found, err := disks.List(ctx)
	if err != nil {
		return storage.Disk{}, err
	}
	for _, disk := range found {
		if disk.Path == path {
			return disk, nil
		}
	}
	return storage.Disk{}, fmt.Errorf("%s is not a physical disk", path)
}

func partitionByNumber(ctx context.Context, disks *storage.Manager, diskPath string, number int) (storage.Disk, storage.Partition, error) {
	disk, err := diskByPath(ctx, disks, diskPath)
	if err != nil {
		return storage.Disk{}, storage.Partition{}, err
	}
	for _, partition := range disk.Partitions {
		if partition.Number == number {
			return disk, partition, nil
		}
	}
	return storage.Disk{}, storage.Partition{}, fmt.Errorf("partition %d does not exist on %s", number, diskPath)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]string{"code": errorCode(err)})
}

func errorCode(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "administrator is already configured"):
		return "auth_already_configured"
	case strings.Contains(message, "eligible local non-root"):
		return "auth_local_user_required"
	case strings.Contains(message, "system authentication failed"):
		return "auth_system_auth_failed"
	case strings.Contains(message, "project password must be at least"):
		return "auth_password_too_short"
	case strings.Contains(message, "administrator setup is required"):
		return "auth_setup_required"
	case strings.Contains(message, "invalid username or password"):
		return "auth_invalid_credentials"
	case strings.Contains(message, "session"):
		return "auth_required"
	case strings.Contains(message, "confirmation must exactly match"):
		return "confirmation_failed"
	case strings.Contains(message, "system disk"):
		return "system_disk_protected"
	case strings.Contains(message, "mounted disk") || strings.Contains(message, "mounted partition"):
		return "mounted_disk_protected"
	case strings.Contains(message, "not a physical disk partition"):
		return "invalid_partition"
	case strings.Contains(message, "not a physical disk"):
		return "invalid_disk"
	case strings.Contains(message, "partition path or UUID is required"):
		return "invalid_partition"
	case strings.Contains(message, "managed partition") && strings.Contains(message, "is present"):
		return "partition_present"
	case strings.Contains(message, "does not have a GPT"):
		return "invalid_partition_table"
	case strings.Contains(message, "managed volume") && strings.Contains(message, "not found"):
		return "managed_volume_not_found"
	case strings.Contains(message, "does not exist"):
		return "partition_not_found"
	case strings.Contains(message, "unsupported filesystem"):
		return "unsupported_filesystem"
	case strings.Contains(message, "mount path is not configured"):
		return "mount_path_not_configured"
	case strings.Contains(message, "already configured"):
		return "mount_path_in_use"
	case strings.Contains(message, "invalid mount path"):
		return "invalid_mount_path"
	case strings.Contains(message, "SMART") || strings.Contains(message, "smartctl"):
		return "smart_query_failed"
	case strings.Contains(message, "zoned partition size"):
		return "invalid_zoned_partition_size"
	case strings.Contains(message, "USB storage only supports") || strings.Contains(message, "USB storage is managed"):
		return "usb_mutation_not_supported"
	case strings.Contains(message, "not a USB partition"):
		return "not_usb_partition"
	case strings.Contains(message, "USB partition must use"):
		return "unsupported_filesystem"
	case strings.Contains(message, "USB device"):
		return "usb_capacity_exceeded"
	case strings.Contains(message, "must be formatted"):
		return "unformatted_partition"
	case strings.Contains(message, "not enough unallocated space"):
		return "insufficient_space"
	case strings.Contains(message, "mount target"):
		return "invalid_mount_target"
	case strings.Contains(message, "restart system"):
		return "reboot_failed"
	case strings.Contains(message, "does not contain a reclaimable storage stack"):
		return "reclaim_not_available"
	case strings.Contains(message, "dmsetup") || strings.Contains(message, "mdadm") || strings.Contains(message, "cryptsetup") || strings.Contains(message, "wipefs"):
		return "reclaim_tool_failed"
	default:
		return "internal_error"
	}
}

func logOperationError(logger *slog.Logger, operation string, err error, args ...any) {
	logger.Error("storage operation failed", append([]any{"operation", operation, "error", err}, args...)...)
}

func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic while serving request", "method", r.Method, "path", r.URL.Path, "panic", recovered, "stack", string(debug.Stack()))
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
