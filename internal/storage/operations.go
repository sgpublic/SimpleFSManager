package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/diskfs/go-diskfs"
	diskpkg "github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"golang.org/x/sys/unix"
)

var volumePath = regexp.MustCompile(`^/vol[1-9][0-9]*$`)
var usbPath = regexp.MustCompile(`^/usb[a-z][a-z]$`)

// ValidateMountPath accepts application-managed absolute directories while
// excluding paths that are part of the operating system's live namespace.
func ValidateMountPath(target string) error {
	if target == "" || !filepath.IsAbs(target) || filepath.Clean(target) != target || target == "/" {
		return apperr.New(http.StatusBadRequest, apperr.CodeInvalidMountPath, "mount path must be a clean absolute path other than /")
	}
	for _, prefix := range []string{"/boot", "/dev", "/etc", "/proc", "/run", "/sys", "/usr"} {
		if target == prefix || strings.HasPrefix(target, prefix+"/") {
			return apperr.New(http.StatusBadRequest, apperr.CodeInvalidMountPath, "mount path is reserved by the operating system")
		}
	}
	return nil
}

// InitializeGPT replaces the target disk's partition table with an empty GPT.
// The caller must obtain explicit user confirmation before calling this method.
func (m *Manager) InitializeGPT(ctx context.Context, diskPath string) (bool, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	disk, device, err := m.diskWithRaw(ctx, diskPath)
	if err != nil {
		return false, err
	}
	if err := validateUnusedDisk(disk); err != nil {
		return false, err
	}
	if isHostManaged(device) {
		return false, apperr.New(http.StatusBadRequest, apperr.CodeInvalidDisk, "host-managed zoned disks must be formatted as a whole-disk F2FS volume")
	}
	return m.initializeGPT(ctx, diskPath)
}

// Reclaim replaces an inactive RAID/LVM/crypt storage stack with an empty GPT.
// The stack must not have any mounted filesystems.
func (m *Manager) Reclaim(ctx context.Context, diskPath string) (bool, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	device, err := m.device(ctx, diskPath)
	if err != nil {
		return false, err
	}
	if strings.EqualFold(strings.TrimSpace(device.Transport), "usb") {
		return false, apperr.New(http.StatusBadRequest, apperr.CodeUSBMutationNotSupported, "USB storage only supports mount and unmount")
	}
	if len(mountpoints(device.Mountpoints)) > 0 || hasMountedDescendant(device) {
		return false, apperr.New(http.StatusConflict, apperr.CodeMountedDiskProtected, fmt.Sprintf("refusing to reclaim mounted disk %s", diskPath))
	}
	if !hasStorageStack(device) {
		return false, apperr.New(http.StatusBadRequest, apperr.CodeReclaimNotAvailable, fmt.Sprintf("disk %s does not contain a reclaimable storage stack", diskPath))
	}
	if isHostManaged(device) {
		return false, apperr.New(http.StatusBadRequest, apperr.CodeInvalidDisk, "host-managed zoned disks must be formatted as a whole-disk F2FS volume")
	}

	for _, child := range postorder(device) {
		switch {
		case child.Type == "lvm":
			if _, err := m.runner.Run(ctx, "dmsetup", "remove", "--retry", child.Path); err != nil {
				return false, apperr.Wrap(http.StatusInternalServerError, apperr.CodeReclaimToolFailed, fmt.Sprintf("deactivate logical volume %s: %v", child.Path, err), err)
			}
		case child.Type == "crypt":
			if _, err := m.runner.Run(ctx, "cryptsetup", "close", filepath.Base(child.Path)); err != nil {
				return false, apperr.Wrap(http.StatusInternalServerError, apperr.CodeReclaimToolFailed, fmt.Sprintf("close encrypted volume %s: %v", child.Path, err), err)
			}
		case strings.HasPrefix(child.Type, "raid"):
			if _, err := m.runner.Run(ctx, "mdadm", "--stop", child.Path); err != nil {
				return false, apperr.Wrap(http.StatusInternalServerError, apperr.CodeReclaimToolFailed, fmt.Sprintf("stop RAID device %s: %v", child.Path, err), err)
			}
		}
	}

	for _, child := range device.Children {
		if child.Type != "part" {
			continue
		}
		if hasRAIDDescendant(child) {
			if _, err := m.runner.Run(ctx, "mdadm", "--zero-superblock", "--force", child.Path); err != nil {
				return false, apperr.Wrap(http.StatusInternalServerError, apperr.CodeReclaimToolFailed, fmt.Sprintf("clear RAID signature from %s: %v", child.Path, err), err)
			}
		}
		if _, err := m.runner.Run(ctx, "wipefs", "--all", "--force", child.Path); err != nil {
			return false, apperr.Wrap(http.StatusInternalServerError, apperr.CodeReclaimToolFailed, fmt.Sprintf("clear signatures from %s: %v", child.Path, err), err)
		}
	}
	if _, err := m.runner.Run(ctx, "wipefs", "--all", "--force", diskPath); err != nil {
		return false, apperr.Wrap(http.StatusInternalServerError, apperr.CodeReclaimToolFailed, fmt.Sprintf("clear signatures from %s: %v", diskPath, err), err)
	}
	return m.initializeGPT(ctx, diskPath)
}

// CreatePartition allocates one Linux filesystem partition from an unused GPT
// disk. On regular devices sizeBytes is rounded up to the logical sector size;
// zoned devices require an exact zone-size multiple. When useLargestFree is
// true, it fills the largest contiguous range of complete zones.
func (m *Manager) CreatePartition(ctx context.Context, diskPath string, sizeBytes uint64, useLargestFree bool, name string) (int, error) {
	if sizeBytes == 0 && !useLargestFree {
		return 0, apperr.New(http.StatusBadRequest, apperr.CodeInsufficientSpace, "partition size must be greater than zero")
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()

	diskInfo, device, err := m.diskWithRaw(ctx, diskPath)
	if err != nil {
		return 0, err
	}
	if err := validateUnusedDisk(diskInfo); err != nil {
		return 0, err
	}
	if isHostManaged(device) {
		return 0, apperr.New(http.StatusBadRequest, apperr.CodeInvalidPartitionTable, "host-managed zoned disks do not support partitions; format the whole disk as F2FS")
	}

	disk, err := diskfs.Open(diskPath)
	if err != nil {
		return 0, fmt.Errorf("open disk for partition creation: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = disk.Close()
		}
	}()

	table, err := gptTable(disk)
	if err != nil {
		return 0, err
	}

	alignment := partitionAlignment(device, uint64(disk.LogicalBlocksize))
	var start, end uint64
	if useLargestFree {
		var ok bool
		start, end, ok = largestPartitionGapAligned(table, alignment, isZoned(device))
		if !ok {
			return 0, apperr.New(http.StatusBadRequest, apperr.CodeInsufficientSpace, "not enough unallocated space")
		}
	} else {
		sectorSize := uint64(disk.LogicalBlocksize)
		if isZoned(device) && sizeBytes%(device.ZoneSize) != 0 {
			return 0, apperr.New(http.StatusBadRequest, apperr.CodeInvalidZonedPartSize, fmt.Sprintf("zoned partition size must be a multiple of %d bytes", device.ZoneSize))
		}
		needed := (sizeBytes + sectorSize - 1) / sectorSize
		if needed == 0 {
			return 0, apperr.New(http.StatusBadRequest, apperr.CodeInsufficientSpace, "partition size must be greater than zero")
		}
		if isZoned(device) {
			needed = alignSector(needed, alignment)
		}
		var ok bool
		start, ok = nextPartitionStart(table, needed, alignment)
		if !ok {
			return 0, apperr.New(http.StatusBadRequest, apperr.CodeInsufficientSpace, fmt.Sprintf("not enough unallocated space for %d bytes", sizeBytes))
		}
		end = start + needed - 1
	}
	index := nextPartitionIndex(table)
	table.Partitions = append(table.Partitions, &gpt.Partition{
		Index: index,
		Start: start,
		End:   end,
		Type:  gpt.LinuxFilesystem,
		Name:  name,
	})
	if err := disk.Partition(table); err != nil {
		return 0, fmt.Errorf("write GPT partition: %w", err)
	}
	closed = true
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
		return apperr.New(http.StatusBadRequest, apperr.CodeInvalidPartition, "partition index must be positive")
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()

	diskInfo, device, err := m.diskWithRaw(ctx, diskPath)
	if err != nil {
		return err
	}
	if err := validateUnusedDisk(diskInfo); err != nil {
		return err
	}
	if isHostManaged(device) {
		return apperr.New(http.StatusBadRequest, apperr.CodeInvalidPartitionTable, "host-managed zoned disks do not support partitions")
	}

	disk, err := diskfs.Open(diskPath)
	if err != nil {
		return fmt.Errorf("open disk for partition deletion: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = disk.Close()
		}
	}()

	table, err := gptTable(disk)
	if err != nil {
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
		return apperr.New(http.StatusNotFound, apperr.CodePartitionNotFound, fmt.Sprintf("GPT partition %d does not exist", index))
	}
	table.Partitions = partitions
	if err := disk.Partition(table); err != nil {
		return fmt.Errorf("write GPT partition deletion: %w", err)
	}
	closed = true
	if err := disk.Close(); err != nil {
		return fmt.Errorf("close partitioned disk: %w", err)
	}
	return rereadPartitionTable(diskPath)
}

func (m *Manager) Format(ctx context.Context, partitionPath, filesystem string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	disk, partition, err := m.Partition(ctx, partitionPath)
	if err != nil {
		return err
	}
	if err := validateUnusedPartition(disk, partition, false); err != nil {
		return err
	}
	if disk.Path == partition.Path && isHostManaged(lsblkDevice{Zoned: disk.Zoned, ZoneSize: disk.ZoneSizeBytes}) {
		return apperr.New(http.StatusBadRequest, apperr.CodeInvalidDisk, "host-managed zoned disks must be formatted through the whole-disk F2FS operation")
	}
	var command string
	var args []string
	switch filesystem {
	case "ext4":
		command, args = "mkfs.ext4", []string{"-F", partitionPath}
	case "xfs":
		command, args = "mkfs.xfs", []string{"-f", partitionPath}
	case "btrfs":
		command, args = "mkfs.btrfs", []string{"-f", partitionPath}
	case "f2fs":
		command, args = "mkfs.f2fs", []string{"-f", partitionPath}
	default:
		return apperr.New(http.StatusBadRequest, apperr.CodeUnsupportedFilesystem, fmt.Sprintf("unsupported filesystem %q", filesystem))
	}
	if _, err := m.runner.Run(ctx, command, args...); err != nil {
		return fmt.Errorf("format %s as %s: %w", partitionPath, filesystem, err)
	}
	return nil
}

// FormatWholeDisk formats a host-managed zoned disk as one F2FS volume.
func (m *Manager) FormatWholeDisk(ctx context.Context, diskPath string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	disk, device, err := m.diskWithRaw(ctx, diskPath)
	if err != nil {
		return err
	}
	if err := validateUnusedDisk(disk); err != nil {
		return err
	}
	if !isHostManaged(device) {
		return apperr.New(http.StatusBadRequest, apperr.CodeInvalidDisk, "whole-disk formatting is only supported for host-managed zoned disks")
	}
	if _, err := m.runner.Run(ctx, "mkfs.f2fs", "-f", "-m", diskPath); err != nil {
		return fmt.Errorf("format host-managed disk %s as f2fs: %w", diskPath, err)
	}
	return nil
}

func (m *Manager) Mount(ctx context.Context, partitionPath, target, filesystem string) error {
	if !managedFilesystem(filesystem) {
		return apperr.New(http.StatusBadRequest, apperr.CodeUnsupportedFilesystem, fmt.Sprintf("unsupported filesystem %q", filesystem))
	}
	if !usbPath.MatchString(target) {
		if err := ValidateMountPath(target); err != nil {
			return err
		}
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()

	disk, partition, err := m.Partition(ctx, partitionPath)
	if err != nil {
		return err
	}
	if err := validateUnusedPartition(disk, partition, true); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Clean(target), 0o755); err != nil {
		return apperr.Wrap(http.StatusBadRequest, apperr.CodeInvalidMountTarget, fmt.Sprintf("create mount target: %v", err), err)
	}
	if err := unix.Mount(partitionPath, target, filesystem, unix.MS_NODEV|unix.MS_NOSUID, ""); err != nil {
		return fmt.Errorf("mount %s at %s: %w", partitionPath, target, err)
	}
	return nil
}

func (m *Manager) Unmount(target string) error {
	if !usbPath.MatchString(target) {
		if err := ValidateMountPath(target); err != nil {
			return err
		}
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()

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

func (m *Manager) device(ctx context.Context, diskPath string) (lsblkDevice, error) {
	rawDevices, err := m.listRawDevices(ctx)
	if err != nil {
		return lsblkDevice{}, err
	}
	for _, device := range rawDevices {
		if device.Type == "disk" && device.Path == diskPath {
			return device, nil
		}
	}
	return lsblkDevice{}, apperr.New(http.StatusBadRequest, apperr.CodeInvalidDisk, fmt.Sprintf("%s is not a physical disk", diskPath))
}

func postorder(device lsblkDevice) []lsblkDevice {
	devices := make([]lsblkDevice, 0)
	for _, child := range device.Children {
		devices = append(devices, postorder(child)...)
		devices = append(devices, child)
	}
	return devices
}

func hasRAIDDescendant(device lsblkDevice) bool {
	for _, child := range device.Children {
		if strings.HasPrefix(child.Type, "raid") || hasRAIDDescendant(child) {
			return true
		}
	}
	return false
}

func (m *Manager) Reboot(ctx context.Context) error {
	if _, err := m.runner.Run(ctx, "systemctl", "reboot"); err != nil {
		return apperr.Wrap(http.StatusInternalServerError, apperr.CodeRebootFailed, fmt.Sprintf("restart system: %v", err), err)
	}
	return nil
}

func (m *Manager) initializeGPT(_ context.Context, diskPath string) (bool, error) {
	disk, err := diskfs.Open(diskPath)
	if err != nil {
		return false, fmt.Errorf("open disk for GPT initialization: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = disk.Close()
		}
	}()

	table := &gpt.Table{
		LogicalSectorSize:  int(disk.LogicalBlocksize),
		PhysicalSectorSize: int(disk.PhysicalBlocksize),
		ProtectiveMBR:      true,
	}
	if err := disk.Partition(table); err != nil {
		if requiresReboot(err) {
			return true, nil
		}
		return false, fmt.Errorf("write GPT: %w", err)
	}
	closed = true
	if err := disk.Close(); err != nil {
		return false, fmt.Errorf("close GPT device: %w", err)
	}
	if err := rereadPartitionTable(diskPath); err != nil {
		if requiresReboot(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func requiresReboot(err error) bool {
	return errors.Is(err, diskpkg.ErrReReadDeferred) || errors.Is(err, unix.EBUSY)
}

func gptTable(device *diskpkg.Disk) (*gpt.Table, error) {
	table, err := device.GetPartitionTable()
	if err != nil {
		return nil, fmt.Errorf("read partition table: %w", err)
	}
	gptTable, ok := table.(*gpt.Table)
	if !ok {
		return nil, apperr.New(http.StatusBadRequest, apperr.CodeInvalidPartitionTable, "disk does not have a GPT partition table")
	}
	return gptTable, nil
}

func nextPartitionStart(table *gpt.Table, needed uint64, alignments ...uint64) (uint64, bool) {
	alignment := uint64(2048)
	if len(alignments) > 0 {
		alignment = alignments[0]
	}
	partitions := append([]*gpt.Partition(nil), table.Partitions...)
	sort.Slice(partitions, func(i, j int) bool { return partitions[i].Start < partitions[j].Start })
	start := alignSector(2048, alignment)
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

func largestPartitionGap(table *gpt.Table, alignments ...uint64) (uint64, uint64, bool) {
	alignment := uint64(2048)
	if len(alignments) > 0 {
		alignment = alignments[0]
	}
	return largestPartitionGapAligned(table, alignment, false)
}

func largestPartitionGapAligned(table *gpt.Table, alignment uint64, zoneAligned bool) (uint64, uint64, bool) {
	partitions := append([]*gpt.Partition(nil), table.Partitions...)
	sort.Slice(partitions, func(i, j int) bool { return partitions[i].Start < partitions[j].Start })
	start := alignSector(2048, alignment)
	var largestStart, largestEnd uint64
	for _, partition := range partitions {
		if partition.Start > start {
			end := partition.Start - 1
			if zoneAligned {
				alignedEnd := alignSectorDown(partition.Start, alignment)
				if alignedEnd == 0 {
					continue
				}
				end = alignedEnd - 1
			}
			if end < start {
				continue
			}
			if largestEnd < largestStart || end-start > largestEnd-largestStart {
				largestStart, largestEnd = start, end
			}
		}
		if partition.End >= start {
			start = alignSector(partition.End+1, alignment)
		}
	}
	lastBoundary := table.LastDataSector() + 1
	if zoneAligned {
		lastBoundary = alignSectorDown(lastBoundary, alignment)
	}
	if lastBoundary > 0 {
		lastEnd := lastBoundary - 1
		if start <= lastEnd && (largestEnd < largestStart || lastEnd-start > largestEnd-largestStart) {
			largestStart, largestEnd = start, lastEnd
		}
	}
	return largestStart, largestEnd, largestEnd >= largestStart
}

func alignSector(value, alignment uint64) uint64 {
	return (value + alignment - 1) / alignment * alignment
}

func alignSectorDown(value, alignment uint64) uint64 {
	return value / alignment * alignment
}

func partitionAlignment(device lsblkDevice, sectorSize uint64) uint64 {
	if !isZoned(device) || sectorSize == 0 {
		return 2048
	}
	return device.ZoneSize / sectorSize
}

func isZoned(device lsblkDevice) bool {
	return device.Zoned != "none" && device.ZoneSize > 0
}

func isHostManaged(device lsblkDevice) bool {
	return strings.EqualFold(strings.TrimSpace(device.Zoned), "host-managed") && device.ZoneSize > 0
}

func managedFilesystem(filesystem string) bool {
	return filesystem == "ext4" || filesystem == "xfs" || filesystem == "btrfs" || filesystem == "f2fs"
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

func validateUnusedDisk(disk Disk) error {
	if disk.USB {
		return apperr.New(http.StatusBadRequest, apperr.CodeUSBMutationNotSupported, "USB storage only supports mount and unmount")
	}
	if disk.System {
		return apperr.New(http.StatusConflict, apperr.CodeSystemDiskProtected, fmt.Sprintf("refusing to modify system disk %s", disk.Path))
	}
	if disk.Protected {
		return apperr.New(http.StatusConflict, apperr.CodeMountedDiskProtected, fmt.Sprintf("refusing to modify mounted disk %s", disk.Path))
	}
	return nil
}

func validateUnusedPartition(disk Disk, partition Partition, allowUSB bool) error {
	if disk.USB && !allowUSB {
		return apperr.New(http.StatusBadRequest, apperr.CodeUSBMutationNotSupported, "USB storage only supports mount and unmount")
	}
	if disk.System {
		return apperr.New(http.StatusConflict, apperr.CodeSystemDiskProtected, fmt.Sprintf("refusing to modify system disk %s", disk.Path))
	}
	if len(partition.Mountpoints) > 0 {
		return apperr.New(http.StatusConflict, apperr.CodeMountedDiskProtected, fmt.Sprintf("refusing to modify mounted partition %s", partition.Path))
	}
	return nil
}
