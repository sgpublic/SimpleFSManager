package volume

import (
	"context"
	"fmt"
	"sync"

	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
)

type Manager struct {
	store   *store.Store
	storage *storage.Manager
	recover sync.Mutex
}

func New(store *store.Store, storage *storage.Manager) *Manager {
	return &Manager{store: store, storage: storage}
}

func (m *Manager) Mount(ctx context.Context, partitionPath string) (store.Volume, error) {
	disk, partition, err := m.storage.Partition(ctx, partitionPath)
	if err != nil {
		return store.Volume{}, err
	}
	if disk.USB {
		return store.Volume{}, fmt.Errorf("USB storage is managed at /usbXY")
	}
	if partition.UUID == "" || partition.FileSystem == "" {
		return store.Volume{}, fmt.Errorf("%s must be formatted before mounting", partitionPath)
	}
	volume, err := m.store.VolumeByUUID(ctx, partition.UUID)
	if err != nil {
		return store.Volume{}, fmt.Errorf("mount path is not configured for %s", partitionPath)
	}
	if err := m.storage.Mount(ctx, partitionPath, volume.MountPath, partition.FileSystem); err != nil {
		return store.Volume{}, err
	}
	return m.store.SetVolumeAutoMount(ctx, volume.UUID, true)
}

func (m *Manager) ConfigureMountPath(ctx context.Context, partitionPath, mountPath string) (store.Volume, error) {
	disk, partition, err := m.storage.Partition(ctx, partitionPath)
	if err != nil {
		return store.Volume{}, err
	}
	if disk.USB {
		return store.Volume{}, fmt.Errorf("USB storage is managed at /usbXY")
	}
	if partition.UUID == "" || partition.FileSystem == "" {
		return store.Volume{}, fmt.Errorf("%s must be formatted before setting a mount path", partitionPath)
	}
	if len(partition.Mountpoints) > 0 {
		return store.Volume{}, fmt.Errorf("refusing to change mount path for mounted partition %s", partitionPath)
	}
	if err := storage.ValidateMountPath(mountPath); err != nil {
		return store.Volume{}, fmt.Errorf("invalid mount path: %w", err)
	}
	return m.store.ConfigureVolume(ctx, partition.UUID, diskIdentity(disk), partition.Number, mountPath)
}

func (m *Manager) ConfigureMissingMountPath(ctx context.Context, uuid, mountPath string) (store.Volume, error) {
	if err := storage.ValidateMountPath(mountPath); err != nil {
		return store.Volume{}, fmt.Errorf("invalid mount path: %w", err)
	}
	disks, err := m.storage.List(ctx)
	if err != nil {
		return store.Volume{}, err
	}
	for _, disk := range disks {
		for _, partition := range disk.Partitions {
			if partition.UUID == uuid {
				return store.Volume{}, fmt.Errorf("managed partition %s is present; use its device path", uuid)
			}
		}
	}
	volume, err := m.store.VolumeByUUID(ctx, uuid)
	if err != nil {
		return store.Volume{}, err
	}
	return m.store.ConfigureVolumePath(ctx, volume.UUID, mountPath)
}

func (m *Manager) Unmount(ctx context.Context, uuid string) (store.Volume, error) {
	volume, err := m.store.VolumeByUUID(ctx, uuid)
	if err != nil {
		return store.Volume{}, err
	}
	if err := m.storage.Unmount(volume.MountPath); err != nil {
		return store.Volume{}, err
	}
	return m.store.SetVolumeAutoMount(ctx, volume.UUID, false)
}

func (m *Manager) Recover(ctx context.Context) error {
	m.recover.Lock()
	defer m.recover.Unlock()

	volumes, err := m.store.AutoMountVolumes(ctx)
	if err != nil {
		return err
	}
	disks, err := m.storage.List(ctx)
	if err != nil {
		return err
	}
	for _, volume := range volumes {
		for _, disk := range disks {
			if disk.USB {
				continue
			}
			for _, partition := range disk.Partitions {
				if partition.UUID != volume.UUID {
					continue
				}
				if contains(partition.Mountpoints, volume.MountPath) {
					break
				}
				if len(partition.Mountpoints) > 0 {
					return fmt.Errorf("managed volume %s is mounted outside %s", volume.UUID, volume.MountPath)
				}
				if err := m.storage.Mount(ctx, partition.Path, volume.MountPath, partition.FileSystem); err != nil {
					return fmt.Errorf("restore %s: %w", volume.MountPath, err)
				}
			}
		}
	}
	return nil
}

func diskIdentity(disk storage.Disk) string {
	if disk.Serial != "" {
		return disk.Serial
	}
	return disk.Path
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
