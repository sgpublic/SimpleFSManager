package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/diskfs/go-diskfs"
	diskpkg "github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"golang.org/x/sys/unix"
)

var volumePath = regexp.MustCompile(`^/vol[1-9][0-9]*$`)
var usbPath = regexp.MustCompile(`^/usb[a-z][a-z]$`)

// InitializeGPT replaces the target disk's partition table with an empty GPT.
// The caller must obtain explicit user confirmation before calling this method.
func (m *Manager) InitializeGPT(ctx context.Context, diskPath string) error {
	if err := m.ensureUnusedDisk(ctx, diskPath); err != nil {
		return err
	}
	disk, err := diskfs.Open(diskPath)
	if err != nil {
		return fmt.Errorf("open disk for GPT initialization: %w", err)
	}
	table := &gpt.Table{
		LogicalSectorSize:  int(disk.LogicalBlocksize),
		PhysicalSectorSize: int(disk.PhysicalBlocksize),
		ProtectiveMBR:      true,
	}
	if err := disk.Partition(table); err != nil {
		disk.Close()
		return fmt.Errorf("write GPT: %w", err)
	}
	if err := disk.Close(); err != nil {
		return fmt.Errorf("close GPT device: %w", err)
	}
	return rereadPartitionTable(diskPath)
}

// CreatePartition allocates one Linux filesystem partition from an unused GPT
// disk. sizeBytes is rounded up to the device's logical sector size.
func (m *Manager) CreatePartition(ctx context.Context, diskPath string, sizeBytes uint64, name string) (int, error) {
	if sizeBytes == 0 {
		return 0, fmt.Errorf("partition size must be greater than zero")
	}
	if err := m.ensureUnusedDisk(ctx, diskPath); err != nil {
		return 0, err
	}
	disk, err := diskfs.Open(diskPath)
	if err != nil {
		return 0, fmt.Errorf("open disk for partition creation: %w", err)
	}
	table, err := gptTable(disk)
	if err != nil {
		disk.Close()
		return 0, err
	}

	sectorSize := uint64(disk.LogicalBlocksize)
	needed := (sizeBytes + sectorSize - 1) / sectorSize
	start, ok := nextPartitionStart(table, needed)
	if !ok {
		disk.Close()
		return 0, fmt.Errorf("not enough unallocated space for %d bytes", sizeBytes)
	}
	index := nextPartitionIndex(table)
	table.Partitions = append(table.Partitions, &gpt.Partition{
		Index: index,
		Start: start,
		End:   start + needed - 1,
		Type:  gpt.LinuxFilesystem,
		Name:  name,
	})
	if err := disk.Partition(table); err != nil {
		disk.Close()
		return 0, fmt.Errorf("write GPT partition: %w", err)
	}
	if err := disk.Close(); err != nil {
		return 0, fmt.Errorf("close partitioned disk: %w", err)
	}
	if err := rereadPartitionTable(diskPath); err != nil {
		return 0, err
	}
	return index, nil
}

// DeletePartition removes a GPT partition by its partition-table index.
func (m *Manager) DeletePartition(ctx context.Context, diskPath string, index int) error {
	if index < 1 {
		return fmt.Errorf("partition index must be positive")
	}
	if err := m.ensureUnusedDisk(ctx, diskPath); err != nil {
		return err
	}
	disk, err := diskfs.Open(diskPath)
	if err != nil {
		return fmt.Errorf("open disk for partition deletion: %w", err)
	}
	table, err := gptTable(disk)
	if err != nil {
		disk.Close()
		return err
	}
	partitions := make([]*gpt.Partition, 0, len(table.Partitions))
	found := false
	for _, partition := range table.Partitions {
		if partition.Index == index {
			found = true
			continue
		}
		partitions = append(partitions, partition)
	}
	if !found {
		disk.Close()
		return fmt.Errorf("GPT partition %d does not exist", index)
	}
	table.Partitions = partitions
	if err := disk.Partition(table); err != nil {
		disk.Close()
		return fmt.Errorf("write GPT partition deletion: %w", err)
	}
	if err := disk.Close(); err != nil {
		return fmt.Errorf("close partitioned disk: %w", err)
	}
	return rereadPartitionTable(diskPath)
}

func (m *Manager) Format(ctx context.Context, partitionPath, filesystem string) error {
	if err := m.ensureUnusedPartition(ctx, partitionPath, false); err != nil {
		return err
	}
	var command string
	var args []string
	switch filesystem {
	case "ext4":
		command, args = "mkfs.ext4", []string{"-F", partitionPath}
	case "xfs":
		command, args = "mkfs.xfs", []string{"-f", partitionPath}
	default:
		return fmt.Errorf("unsupported filesystem %q", filesystem)
	}
	if _, err := m.runner.Run(ctx, command, args...); err != nil {
		return fmt.Errorf("format %s as %s: %w", partitionPath, filesystem, err)
	}
	return nil
}

func (m *Manager) Mount(ctx context.Context, partitionPath, target, filesystem string) error {
	if filesystem != "ext4" && filesystem != "xfs" {
		return fmt.Errorf("unsupported filesystem %q", filesystem)
	}
	if !volumePath.MatchString(target) && !usbPath.MatchString(target) {
		return fmt.Errorf("mount target must be /volN or /usbXY")
	}
	if err := m.ensureUnusedPartition(ctx, partitionPath, true); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Clean(target), 0o755); err != nil {
		return fmt.Errorf("create mount target: %w", err)
	}
	if err := unix.Mount(partitionPath, target, filesystem, unix.MS_NODEV|unix.MS_NOSUID, ""); err != nil {
		return fmt.Errorf("mount %s at %s: %w", partitionPath, target, err)
	}
	return nil
}

func (m *Manager) Unmount(target string) error {
	if !volumePath.MatchString(target) && !usbPath.MatchString(target) {
		return fmt.Errorf("mount target must be /volN or /usbXY")
	}
	if err := unix.Unmount(target, 0); err != nil {
		return fmt.Errorf("unmount %s: %w", target, err)
	}
	return nil
}

func rereadPartitionTable(diskPath string) error {
	device, err := os.Open(diskPath)
	if err != nil {
		return fmt.Errorf("open disk for partition reread: %w", err)
	}
	defer device.Close()
	if err := unix.IoctlSetInt(int(device.Fd()), unix.BLKRRPART, 0); err != nil {
		return fmt.Errorf("reread partition table: %w", err)
	}
	return nil
}

func gptTable(device *diskpkg.Disk) (*gpt.Table, error) {
	table, err := device.GetPartitionTable()
	if err != nil {
		return nil, fmt.Errorf("read partition table: %w", err)
	}
	gptTable, ok := table.(*gpt.Table)
	if !ok {
		return nil, fmt.Errorf("disk does not have a GPT partition table")
	}
	return gptTable, nil
}

func nextPartitionStart(table *gpt.Table, needed uint64) (uint64, bool) {
	const alignment = uint64(2048)
	partitions := append([]*gpt.Partition(nil), table.Partitions...)
	sort.Slice(partitions, func(i, j int) bool { return partitions[i].Start < partitions[j].Start })
	start := alignment
	for _, partition := range partitions {
		if start+needed-1 < partition.Start {
			return start, true
		}
		if partition.End >= start {
			start = alignSector(partition.End+1, alignment)
		}
	}
	return start, start+needed-1 <= table.LastDataSector()
}

func alignSector(value, alignment uint64) uint64 {
	return (value + alignment - 1) / alignment * alignment
}

func nextPartitionIndex(table *gpt.Table) int {
	used := make(map[int]bool, len(table.Partitions))
	for _, partition := range table.Partitions {
		used[partition.Index] = true
	}
	for index := 1; ; index++ {
		if !used[index] {
			return index
		}
	}
}

func (m *Manager) ensureUnusedDisk(ctx context.Context, diskPath string) error {
	disks, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, disk := range disks {
		if disk.Path == diskPath {
			if disk.USB {
				return fmt.Errorf("USB storage only supports mount and unmount")
			}
			if disk.Protected {
				return fmt.Errorf("refusing to modify mounted disk %s", diskPath)
			}
			return nil
		}
	}
	return fmt.Errorf("%s is not a physical disk", diskPath)
}

func (m *Manager) ensureUnusedPartition(ctx context.Context, partitionPath string, allowUSB bool) error {
	disks, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, disk := range disks {
		for _, partition := range disk.Partitions {
			if partition.Path == partitionPath {
				if disk.USB && !allowUSB {
					return fmt.Errorf("USB storage only supports mount and unmount")
				}
				if disk.System {
					return fmt.Errorf("refusing to modify system disk %s", disk.Path)
				}
				if len(partition.Mountpoints) > 0 {
					return fmt.Errorf("refusing to modify mounted partition %s", partitionPath)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("%s is not a physical disk partition", partitionPath)
}
