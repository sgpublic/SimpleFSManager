package usb

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/sgpublic/simplefsmanager/internal/storage"
	"golang.org/x/sys/unix"
)

// Manager assigns transient letters to attached USB disks. Its mapping exists
// only while devices are present, so a replacement device reuses the first
// letter released by a removed device.
type Manager struct {
	storage     *storage.Manager
	mu          sync.Mutex
	letters     map[string]rune
	suppressed  map[string]map[int]bool
	initialized bool
}

func New(storage *storage.Manager) *Manager {
	return &Manager{storage: storage, letters: make(map[string]rune), suppressed: make(map[string]map[int]bool)}
}

// Reconcile assigns an insertion-order letter to addedDiskPath, releases
// missing disks, and mounts all supported USB partitions.
func (m *Manager) Reconcile(ctx context.Context, addedDiskPath string) error {
	disks, err := m.storage.List(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	present := make(map[string]storage.Disk)
	for _, disk := range disks {
		if disk.USB {
			present[disk.Path] = disk
		}
	}
	for path, letter := range m.letters {
		if _, ok := present[path]; ok {
			continue
		}
		m.detach(letter)
		delete(m.letters, path)
		delete(m.suppressed, path)
	}
	if !m.initialized {
		paths := make([]string, 0, len(present))
		for path := range present {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			if err := m.assign(path); err != nil {
				return err
			}
		}
		m.initialized = true
	} else if disk, ok := present[addedDiskPath]; ok && disk.USB {
		if err := m.assign(disk.Path); err != nil {
			return err
		}
	}
	paths := make([]string, 0, len(present))
	for path := range present {
		if _, assigned := m.letters[path]; !assigned {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := m.assign(path); err != nil {
			return err
		}
	}

	var failures []error
	for path, letter := range m.letters {
		disk := present[path]
		if err := m.mountDisk(ctx, disk, letter); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) Mount(ctx context.Context, partitionPath string) (string, error) {
	disk, partition, err := m.storage.Partition(ctx, partitionPath)
	if err != nil {
		return "", err
	}
	if !disk.USB {
		return "", fmt.Errorf("%s is not a USB partition", partitionPath)
	}
	if partition.FileSystem != "ext4" && partition.FileSystem != "xfs" {
		return "", fmt.Errorf("USB partition must use ext4 or xfs")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	letter, ok := m.letters[disk.Path]
	if !ok {
		return "", fmt.Errorf("USB device has not been assigned a mount letter")
	}
	target, err := usbTarget(letter, partition.Number)
	if err != nil {
		return "", err
	}
	if err := m.storage.Mount(ctx, partition.Path, target, partition.FileSystem); err != nil {
		return "", err
	}
	delete(m.suppressed[disk.Path], partition.Number)
	return target, nil
}

func (m *Manager) Unmount(ctx context.Context, partitionPath string) (string, error) {
	disk, partition, err := m.storage.Partition(ctx, partitionPath)
	if err != nil {
		return "", err
	}
	if !disk.USB {
		return "", fmt.Errorf("%s is not a USB partition", partitionPath)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	letter, ok := m.letters[disk.Path]
	if !ok {
		return "", fmt.Errorf("USB device has not been assigned a mount letter")
	}
	target, err := usbTarget(letter, partition.Number)
	if err != nil {
		return "", err
	}
	if err := m.storage.Unmount(target); err != nil {
		return "", err
	}
	if m.suppressed[disk.Path] == nil {
		m.suppressed[disk.Path] = make(map[int]bool)
	}
	m.suppressed[disk.Path][partition.Number] = true
	return target, nil
}

func (m *Manager) assign(path string) error {
	if _, exists := m.letters[path]; exists {
		return nil
	}
	used := make(map[rune]bool, len(m.letters))
	for _, letter := range m.letters {
		used[letter] = true
	}
	for letter := 'a'; letter <= 'z'; letter++ {
		if !used[letter] {
			m.letters[path] = letter
			return nil
		}
	}
	return fmt.Errorf("too many USB devices")
}

func (m *Manager) mountDisk(ctx context.Context, disk storage.Disk, letter rune) error {
	partitions := append([]storage.Partition(nil), disk.Partitions...)
	sort.Slice(partitions, func(i, j int) bool { return partitions[i].Number < partitions[j].Number })
	var failures []error
	for _, partition := range partitions {
		if m.suppressed[disk.Path][partition.Number] || len(partition.Mountpoints) > 0 || (partition.FileSystem != "ext4" && partition.FileSystem != "xfs") {
			continue
		}
		target, err := usbTarget(letter, partition.Number)
		if err == nil {
			err = m.storage.Mount(ctx, partition.Path, target, partition.FileSystem)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("mount %s: %w", partition.Path, err))
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) detach(letter rune) {
	for partition := 'a'; partition <= 'z'; partition++ {
		_ = unix.Unmount(fmt.Sprintf("/usb%c%c", letter, partition), unix.MNT_DETACH)
	}
}

func usbTarget(device rune, partitionNumber int) (string, error) {
	if partitionNumber < 1 || partitionNumber > 26 {
		return "", fmt.Errorf("USB device supports at most 26 partitions")
	}
	return fmt.Sprintf("/usb%c%c", device, rune('a'+partitionNumber-1)), nil
}
