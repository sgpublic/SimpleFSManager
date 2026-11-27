package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
)

func (s *server) registerUSBRoutes(api huma.API) {
	huma.Post(api, "/api/usb/mount", func(ctx context.Context, input *MountInput) (*OperationOutput, error) {
		if err := confirm(input.Body.PartitionPath, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "mount USB partition", err, "partition", input.Body.PartitionPath)
		}
		target, err := s.usbVolumes.Mount(ctx, input.Body.PartitionPath)
		if err != nil {
			return nil, operationError(s.logger, "mount USB partition", err, "partition", input.Body.PartitionPath)
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "mount USB partition", "partition", input.Body.PartitionPath, "mountPath", target)
		return operation("mounted USB partition", target, ""), nil
	})

	huma.Post(api, "/api/usb/unmount", func(ctx context.Context, input *MountInput) (*OperationOutput, error) {
		if err := confirm(input.Body.PartitionPath, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "unmount USB partition", err, "partition", input.Body.PartitionPath)
		}
		target, err := s.usbVolumes.Unmount(ctx, input.Body.PartitionPath)
		if err != nil {
			return nil, operationError(s.logger, "unmount USB partition", err, "partition", input.Body.PartitionPath)
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "unmount USB partition", "partition", input.Body.PartitionPath, "mountPath", target)
		return operation("unmounted USB partition", target, ""), nil
	})
}
