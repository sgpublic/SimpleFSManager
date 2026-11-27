package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"github.com/sgpublic/simplefsmanager/internal/store"
)

type MountInput struct {
	Body struct {
		PartitionPath string `json:"partitionPath" minLength:"1"`
		Confirm       string `json:"confirm" minLength:"1"`
	}
}

type MountPathInput struct {
	Body struct {
		PartitionPath string `json:"partitionPath,omitempty"`
		PartitionUUID string `json:"partitionUUID,omitempty"`
		MountPath     string `json:"mountPath" minLength:"1"`
		AutoMount     *bool  `json:"autoMount,omitempty"`
		Confirm       string `json:"confirm" minLength:"1"`
	}
}

type UnmountInput struct {
	Body struct {
		UUID    string `json:"uuid" minLength:"1"`
		Confirm string `json:"confirm" minLength:"1"`
	}
}

type AutoMountInput struct {
	Body struct {
		UUID      string `json:"uuid" minLength:"1"`
		AutoMount bool   `json:"autoMount"`
	}
}

func (s *server) registerVolumeRoutes(api huma.API) {
	huma.Post(api, "/api/volumes/mount", func(ctx context.Context, input *MountInput) (*OperationOutput, error) {
		if err := confirm(input.Body.PartitionPath, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "mount volume", err, "partition", input.Body.PartitionPath)
		}
		managed, err := s.volumes.Mount(ctx, input.Body.PartitionPath)
		if err != nil {
			return nil, operationError(s.logger, "mount volume", err, "partition", input.Body.PartitionPath)
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "mount volume", "partition", input.Body.PartitionPath, "mountPath", managed.MountPath, "uuid", managed.UUID)
		return operation("mounted volume", managed.MountPath, managed.UUID), nil
	})

	huma.Post(api, "/api/volumes/mount-path", func(ctx context.Context, input *MountPathInput) (*OperationOutput, error) {
		target := input.Body.PartitionPath
		if input.Body.PartitionUUID != "" {
			target = input.Body.PartitionUUID
		}
		if target == "" {
			return nil, operationError(s.logger, "configure volume mount path", apperr.New(http.StatusBadRequest, apperr.CodeInvalidPartition, "partition path or UUID is required"))
		}
		if err := confirm(target, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "configure volume mount path", err, "partition", target)
		}
		var managed store.Volume
		var err error
		if input.Body.PartitionUUID != "" {
			managed, err = s.volumes.ConfigureMissingMountPath(ctx, input.Body.PartitionUUID, input.Body.MountPath, input.Body.AutoMount)
		} else {
			managed, err = s.volumes.ConfigureMountPath(ctx, input.Body.PartitionPath, input.Body.MountPath, input.Body.AutoMount)
		}
		if err != nil {
			return nil, operationError(s.logger, "configure volume mount path", err, "partition", target, "mountPath", input.Body.MountPath)
		}
		logOperationSuccess(s.logger, "configure volume mount path", "partition", target, "mountPath", managed.MountPath, "uuid", managed.UUID, "autoMount", managed.AutoMount)
		return operation("configured volume mount path", managed.MountPath, managed.UUID), nil
	})

	huma.Post(api, "/api/volumes/auto-mount", func(ctx context.Context, input *AutoMountInput) (*OperationOutput, error) {
		managed, err := s.volumes.SetAutoMount(ctx, input.Body.UUID, input.Body.AutoMount)
		if err != nil {
			return nil, operationError(s.logger, "set volume auto-mount", err, "uuid", input.Body.UUID, "autoMount", input.Body.AutoMount)
		}
		logOperationSuccess(s.logger, "set volume auto-mount", "mountPath", managed.MountPath, "uuid", managed.UUID, "autoMount", managed.AutoMount)
		return operation("updated volume auto-mount", managed.MountPath, managed.UUID), nil
	})

	huma.Post(api, "/api/volumes/unmount", func(ctx context.Context, input *UnmountInput) (*OperationOutput, error) {
		if err := confirm(input.Body.UUID, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "unmount volume", err, "uuid", input.Body.UUID)
		}
		managed, err := s.volumes.Unmount(ctx, input.Body.UUID)
		if err != nil {
			return nil, operationError(s.logger, "unmount volume", err, "uuid", input.Body.UUID)
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "unmount volume", "mountPath", managed.MountPath, "uuid", managed.UUID)
		return operation("unmounted volume", managed.MountPath, managed.UUID), nil
	})
}
