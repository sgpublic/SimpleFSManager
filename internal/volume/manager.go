package volume

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
)

type storageBackend interface {
	ListTopology(context.Context) ([]storage.Disk, error)
	Partition(context.Context, string) (storage.Disk, storage.Partition, error)
	Mount(context.Context, string, string, string) error
	Unmount(string) error
}

type Manager struct {
	store      *store.Store
	storage    storageBackend
	recover    sync.Mutex
	mu         sync.Mutex
	suppressed map[string]bool
}

func New(store *store.Store, storage storageBackend) *Manager {
	return &Manager{
		store:      store,
		storage:    storage,
		suppressed: make(map[string]bool),
	}
}

// Mount mounts a configured internal volume without altering its persistent
// auto-mount preference.
func (m *Manager) Mount(ctx context.Context, partitionPath string) (store.Volume, error) {
	disk, partition, err := m.storage.Partition(ctx, partitionPath)
	if err != nil {
		return store.Volume{}, err
	}
	if disk.USB {
		return store.Volume{}, apperr.New(http.StatusBadRequest, apperr.CodeUSBMutationNotSupported, "USB storage is managed at /usbXY")
	}
	if partition.UUID == "" || partition.FileSystem == "" {
		return store.Volume{}, apperr.New(http.StatusBadRequest, apperr.CodeUnformattedPartition, fmt.Sprintf("%s must be formatted before mounting", partitionPath))
	}
	volume, err := m.store.VolumeByUUID(ctx, partition.UUID)
	if err != nil {
		if apperr.IsCode(err, apperr.CodeManagedVolumeNotFound) {
			return store.Volume{}, apperr.New(http.StatusBadRequest, apperr.CodeMountPathNotConfigured, fmt.Sprintf("mount path is not configured for %s", partitionPath))
		}
		return store.Volume{}, err
	}
	if err := m.storage.Mount(ctx, partitionPath, volume.MountPath, partition.FileSystem); err != nil {
		return store.Volume{}, err
	}
	m.clearSuppressed(volume.UUID)
	return volume, nil
}

func (m *Manager) ConfigureMountPath(ctx context.Context, partitionPath, mountPath string, autoMount *bool) (store.Volume, error) {
	disk, partition, err := m.storage.Partition(ctx, partitionPath)
	if err != nil {
		return store.Volume{}, err
	}
	if disk.USB {
		return store.Volume{}, apperr.New(http.StatusBadRequest, apperr.CodeUSBMutationNotSupported, "USB storage is managed at /usbXY")
	}
	if partition.UUID == "" || partition.FileSystem == "" {
		return store.Volume{}, apperr.New(http.StatusBadRequest, apperr.CodeUnformattedPartition, fmt.Sprintf("%s must be formatted before setting a mount path", partitionPath))
	}
	if len(partition.Mountpoints) > 0 {
		return store.Volume{}, apperr.New(http.StatusConflict, apperr.CodeMountedDiskProtected, fmt.Sprintf("refusing to change mount path for mounted partition %s", partitionPath))
	}
	if err := storage.ValidateMountPath(mountPath); err != nil {
		return store.Volume{}, err
	}
	return m.store.ConfigureVolume(ctx, partition.UUID, diskIdentity(disk), partition.Number, mountPath, autoMount)
}

func (m *Manager) ConfigureMissingMountPath(ctx context.Context, uuid, mountPath string, autoMount *bool) (store.Volume, error) {
	if err := storage.ValidateMountPath(mountPath); err != nil {
		return store.Volume{}, err
	}
	disks, err := m.storage.ListTopology(ctx)
	if err != nil {
		return store.Volume{}, err
	}
	for _, disk := range disks {
		for _, partition := range disk.Partitions {
			if partition.UUID == uuid {
				return store.Volume{}, apperr.New(http.StatusConflict, apperr.CodePartitionPresent, fmt.Sprintf("managed partition %s is present; use its device path", uuid))
			}
		}
	}
	volume, err := m.store.VolumeByUUID(ctx, uuid)
	if err != nil {
		return store.Volume{}, err
	}
	return m.store.ConfigureVolumePath(ctx, volume.UUID, mountPath, autoMount)
}

// Unmount unmounts a managed volume without changing its persistent auto-mount
// preference. It suppresses automatic re-mounting for this session until the
// volume is mounted again or the underlying device is detached and reattached.
func (m *Manager) Unmount(ctx context.Context, uuid string) (store.Volume, error) {
	volume, err := m.store.VolumeByUUID(ctx, uuid)
	if err != nil {
		return store.Volume{}, err
	}
	if err := m.storage.Unmount(volume.MountPath); err != nil {
		return store.Volume{}, err
	}
	m.markSuppressed(volume.UUID)
	return volume, nil
}

// SetAutoMount updates a managed volume's independent auto-mount setting.
func (m *Manager) SetAutoMount(ctx context.Context, uuid string, enabled bool) (store.Volume, error) {
	volume, err := m.store.SetVolumeAutoMount(ctx, uuid, enabled)
	if err != nil {
		return store.Volume{}, err
	}
	m.clearSuppressed(volume.UUID)
	return volume, nil
}

func (m *Manager) Recover(ctx context.Context) error {
	m.recover.Lock()
	defer m.recover.Unlock()

	volumes, err := m.store.AutoMountVolumes(ctx)
	if err != nil {
		return err
	}
	disks, err := m.storage.ListTopology(ctx)
	if err != nil {
		return err
	}
	partitionsByUUID := make(map[string]storage.Partition)
	for _, disk := range disks {
		if disk.USB {
			continue
		}
		for _, partition := range disk.Partitions {
			if partition.UUID != "" {
				partitionsByUUID[partition.UUID] = partition
			}
		}
	}

	// Clear manual-unmount suppression once a device is no longer attached so
	// hot-reattaching it later will auto-mount as configured.
	m.mu.Lock()
	for uuid := range m.suppressed {
		if _, present := partitionsByUUID[uuid]; !present {
			delete(m.suppressed, uuid)
		}
	}
	m.mu.Unlock()

	if len(volumes) == 0 {
		return nil
	}

	var failures []error
	for _, volume := range volumes {
		if m.isSuppressed(volume.UUID) {
			continue
		}
		partition, ok := partitionsByUUID[volume.UUID]
		if !ok {
			continue
		}
		if contains(partition.Mountpoints, volume.MountPath) || len(partition.Mountpoints) > 0 {
			continue
		}
		if err := m.storage.Mount(ctx, partition.Path, volume.MountPath, partition.FileSystem); err != nil {
			failures = append(failures, fmt.Errorf("restore %s: %w", volume.MountPath, err))
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) markSuppressed(uuid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.suppressed[uuid] = true
}

func (m *Manager) clearSuppressed(uuid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.suppressed, uuid)
}

func (m *Manager) isSuppressed(uuid string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.suppressed[uuid]
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
