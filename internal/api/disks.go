package api

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
)

type DiskListInput struct {
	Refresh bool `query:"refresh" doc:"Force an immediate disk and SMART refresh instead of serving the cached snapshot"`
}

type DiskListOutput struct {
	Body struct {
		Disks     []storage.Disk `json:"disks"`
		UpdatedAt string         `json:"updatedAt,omitempty"`
	}
}

type SmartInput struct {
	DiskPath string `query:"diskPath" minLength:"1"`
}

type SmartOutput struct {
	Body struct {
		Data json.RawMessage `json:"data"`
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

func (s *server) registerDiskRoutes(api huma.API) {
	huma.Get(api, "/api/disks", func(ctx context.Context, input *DiskListInput) (*DiskListOutput, error) {
		found, updatedAt, err := s.diskCache.Get(ctx, input.Refresh)
		if err != nil {
			logOperationError(s.logger, "list disks", err)
			return nil, err
		}
		volumes, err := s.db.Volumes(ctx)
		if err != nil {
			logOperationError(s.logger, "list managed volumes", err)
			return nil, err
		}
		output := &DiskListOutput{}
		output.Body.Disks = mergeManagedVolumes(found, volumes)
		if !updatedAt.IsZero() {
			output.Body.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		}
		return output, nil
	})

	huma.Get(api, "/api/disks/smart", func(ctx context.Context, input *SmartInput) (*SmartOutput, error) {
		data, err := s.disks.Smart(ctx, input.DiskPath)
		if err != nil {
			return nil, operationError(s.logger, "read SMART details", err, "disk", input.DiskPath)
		}
		output := &SmartOutput{}
		output.Body.Data = data
		return output, nil
	})

	huma.Post(api, "/api/disks/gpt", func(ctx context.Context, input *InitializeGPTInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "initialize GPT", err, "disk", input.Body.DiskPath)
		}
		disk, err := s.disks.DiskByPath(ctx, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(s.logger, "initialize GPT", err, "disk", input.Body.DiskPath)
		}
		rebootRequired, err := s.disks.InitializeGPT(ctx, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(s.logger, "initialize GPT", err, "disk", input.Body.DiskPath)
		}
		if err := s.db.DeleteVolumesBySerial(ctx, diskIdentity(disk)); err != nil {
			logOperationError(s.logger, "remove volumes after GPT initialization", err, "disk", input.Body.DiskPath)
			return nil, err
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "initialize GPT", "disk", input.Body.DiskPath, "rebootRequired", rebootRequired)
		return operation("initialized empty GPT", "", "", rebootRequired), nil
	})

	huma.Post(api, "/api/disks/reclaim", func(ctx context.Context, input *ReclaimDiskInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "reclaim disk", err, "disk", input.Body.DiskPath)
		}
		disk, err := s.disks.DiskByPath(ctx, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(s.logger, "reclaim disk", err, "disk", input.Body.DiskPath)
		}
		rebootRequired, err := s.disks.Reclaim(ctx, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(s.logger, "reclaim disk", err, "disk", input.Body.DiskPath)
		}
		if err := s.db.DeleteVolumesBySerial(ctx, diskIdentity(disk)); err != nil {
			logOperationError(s.logger, "remove volumes after reclaiming disk", err, "disk", input.Body.DiskPath)
			return nil, err
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "reclaim disk", "disk", input.Body.DiskPath, "rebootRequired", rebootRequired)
		return operation("reclaimed disk with empty GPT", "", "", rebootRequired), nil
	})

	huma.Post(api, "/api/system/reboot", func(ctx context.Context, input *RebootInput) (*OperationOutput, error) {
		if err := confirm("system", input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "restart system", err)
		}
		if err := s.disks.Reboot(ctx); err != nil {
			return nil, operationError(s.logger, "restart system", err)
		}
		logOperationSuccess(s.logger, "restart system")
		return operation("restarting system", "", "", false), nil
	})

	huma.Post(api, "/api/partitions", func(ctx context.Context, input *CreatePartitionInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "create partition", err, "disk", input.Body.DiskPath)
		}
		index, err := s.disks.CreatePartition(ctx, input.Body.DiskPath, input.Body.SizeBytes, input.Body.UseLargestFree, input.Body.Name)
		if err != nil {
			return nil, operationError(s.logger, "create partition", err, "disk", input.Body.DiskPath)
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "create partition", "disk", input.Body.DiskPath, "partitionNumber", index)
		return operation(fmt.Sprintf("created partition %d", index), "", ""), nil
	})

	huma.Post(api, "/api/partitions/delete", func(ctx context.Context, input *DeletePartitionInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "delete partition", err, "disk", input.Body.DiskPath)
		}
		disk, partition, err := s.disks.PartitionByNumber(ctx, input.Body.DiskPath, input.Body.PartitionNumber)
		if err != nil {
			return nil, operationError(s.logger, "delete partition", err, "disk", input.Body.DiskPath, "partitionNumber", input.Body.PartitionNumber)
		}
		if err := s.disks.DeletePartition(ctx, disk.Path, input.Body.PartitionNumber); err != nil {
			return nil, operationError(s.logger, "delete partition", err, "disk", disk.Path, "partitionNumber", input.Body.PartitionNumber)
		}
		if err := s.db.DeleteVolumeIfExists(ctx, partition.UUID); err != nil {
			logOperationError(s.logger, "remove volume after partition deletion", err, "disk", disk.Path, "partition", partition.Path)
			return nil, err
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "delete partition", "disk", disk.Path, "partition", partition.Path, "partitionNumber", input.Body.PartitionNumber)
		return operation("deleted partition", "", ""), nil
	})

	huma.Post(api, "/api/partitions/format", func(ctx context.Context, input *FormatInput) (*OperationOutput, error) {
		if err := confirm(input.Body.PartitionPath, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "format partition", err, "partition", input.Body.PartitionPath)
		}
		_, partition, err := s.disks.Partition(ctx, input.Body.PartitionPath)
		if err != nil {
			return nil, operationError(s.logger, "format partition", err, "partition", input.Body.PartitionPath, "filesystem", input.Body.FileSystem)
		}
		if err := s.disks.Format(ctx, input.Body.PartitionPath, input.Body.FileSystem); err != nil {
			return nil, operationError(s.logger, "format partition", err, "partition", input.Body.PartitionPath, "filesystem", input.Body.FileSystem)
		}
		if err := s.db.DeleteVolumeIfExists(ctx, partition.UUID); err != nil {
			logOperationError(s.logger, "remove volume after formatting", err, "partition", partition.Path)
			return nil, err
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "format partition", "partition", input.Body.PartitionPath, "filesystem", input.Body.FileSystem)
		return operation("formatted partition", "", ""), nil
	})

	// Host-managed zoned disks cannot expose partitions to Linux. They use one
	// whole-disk F2FS volume instead.
	huma.Post(api, "/api/disks/format-f2fs", func(ctx context.Context, input *FormatWholeDiskInput) (*OperationOutput, error) {
		if err := confirm(input.Body.DiskPath, input.Body.Confirm); err != nil {
			return nil, operationError(s.logger, "format host-managed disk", err, "disk", input.Body.DiskPath)
		}
		disk, err := s.disks.DiskByPath(ctx, input.Body.DiskPath)
		if err != nil {
			return nil, operationError(s.logger, "format host-managed disk", err, "disk", input.Body.DiskPath)
		}
		var previousUUID string
		for _, partition := range disk.Partitions {
			if partition.Path == disk.Path {
				previousUUID = partition.UUID
				break
			}
		}
		if err := s.disks.FormatWholeDisk(ctx, disk.Path); err != nil {
			return nil, operationError(s.logger, "format host-managed disk", err, "disk", disk.Path, "filesystem", "f2fs")
		}
		if err := s.db.DeleteVolumeIfExists(ctx, previousUUID); err != nil {
			logOperationError(s.logger, "remove volume after whole-disk format", err, "disk", disk.Path)
			return nil, err
		}
		s.invalidateDiskCache()
		logOperationSuccess(s.logger, "format host-managed disk", "disk", disk.Path, "filesystem", "f2fs")
		return operation("formatted host-managed disk as f2fs", "", ""), nil
	})
}

func mergeManagedVolumes(disks []storage.Disk, volumes []store.Volume) []storage.Disk {
	byUUID := make(map[string]store.Volume, len(volumes))
	for _, volume := range volumes {
		byUUID[volume.UUID] = volume
	}
	seenVolumes := make(map[string]bool, len(volumes))
	for diskIndex := range disks {
		for partitionIndex := range disks[diskIndex].Partitions {
			partition := &disks[diskIndex].Partitions[partitionIndex]
			if partition.UUID == "" {
				continue
			}
			volume, ok := byUUID[partition.UUID]
			if !ok {
				continue
			}
			partition.MountPath = volume.MountPath
			partition.Registered = true
			partition.AutoMount = volume.AutoMount
			seenVolumes[volume.UUID] = true
		}
	}
	for _, volume := range volumes {
		if seenVolumes[volume.UUID] {
			continue
		}
		disks = append(disks, storage.Disk{
			Name:        "registered-volume",
			Serial:      volume.DeviceSerial,
			Missing:     true,
			Protected:   true,
			Mountpoints: []string{},
			Partitions: []storage.Partition{{
				Name:        fmt.Sprintf("partition-%d", volume.PartitionNum),
				Number:      volume.PartitionNum,
				UUID:        volume.UUID,
				MountPath:   volume.MountPath,
				Mountpoints: []string{},
				Registered:  true,
				AutoMount:   volume.AutoMount,
				Missing:     true,
			}},
		})
	}
	return disks
}

func diskIdentity(disk storage.Disk) string {
	if disk.Serial != "" {
		return disk.Serial
	}
	return disk.Path
}
