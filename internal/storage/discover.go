package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var partitionNumber = regexp.MustCompile(`(?:p)?([0-9]+)$`)

type lsblkOutput struct {
	BlockDevices []lsblkDevice `json:"blockdevices"`
}

type lsblkDevice struct {
	Name        string        `json:"name"`
	Path        string        `json:"path"`
	Type        string        `json:"type"`
	Size        uint64        `json:"size"`
	Model       string        `json:"model"`
	Serial      string        `json:"serial"`
	Transport   string        `json:"tran"`
	PTType      string        `json:"pttype"`
	FSType      string        `json:"fstype"`
	UUID        string        `json:"uuid"`
	Mountpoints []*string     `json:"mountpoints"`
	Children    []lsblkDevice `json:"children"`
}

type smartctlOutput struct {
	Temperature *struct {
		Current *float64 `json:"current"`
	} `json:"temperature"`
	SmartStatus *struct {
		Passed *bool `json:"passed"`
	} `json:"smart_status"`
	Attributes *struct {
		Table []struct {
			ID  int `json:"id"`
			Raw struct {
				Value *float64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
}

func (m *Manager) List(ctx context.Context) ([]Disk, error) {
	output, err := m.runner.Run(ctx, "lsblk", "--json", "--bytes", "--output", "NAME,PATH,TYPE,SIZE,MODEL,SERIAL,TRAN,PTTYPE,FSTYPE,UUID,MOUNTPOINTS")
	if err != nil {
		return nil, fmt.Errorf("list block devices: %w", err)
	}

	var result lsblkOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("decode lsblk output: %w", err)
	}

	disks := make([]Disk, 0, len(result.BlockDevices))
	for _, device := range result.BlockDevices {
		if device.Type != "disk" {
			continue
		}
		disk := Disk{
			Path:         device.Path,
			Name:         device.Name,
			Model:        strings.TrimSpace(device.Model),
			Serial:       strings.TrimSpace(device.Serial),
			Transport:    strings.TrimSpace(device.Transport),
			USB:          strings.EqualFold(strings.TrimSpace(device.Transport), "usb"),
			SizeBytes:    device.Size,
			Partitioning: device.PTType,
			Mountpoints:  mountpoints(device.Mountpoints),
			Partitions:   make([]Partition, 0),
		}
		disk.TemperatureCelsius, disk.SmartHealth = m.smartMetadata(ctx, disk.Path)
		disk.Protected = len(disk.Mountpoints) > 0
		// Whole-disk mounts have no partition UUID and cannot be managed volumes.
		disk.System = len(disk.Mountpoints) > 0 && !disk.USB
		for _, child := range device.Children {
			if child.Type != "part" {
				continue
			}
			partition := Partition{
				Path:        child.Path,
				Name:        child.Name,
				Number:      number(child.Name),
				SizeBytes:   child.Size,
				FileSystem:  child.FSType,
				UUID:        child.UUID,
				Mountpoints: mountpoints(child.Mountpoints),
			}
			if partition.FileSystem == "" || partition.UUID == "" {
				m.fillBlockMetadata(ctx, &partition)
			}
			if len(partition.Mountpoints) > 0 {
				disk.Protected = true
				managed, err := m.managesUUID(ctx, partition.UUID)
				if err != nil {
					return nil, err
				}
				disk.System = disk.System || (!disk.USB && !managed)
				partition.Usage = usage(partition.Mountpoints[0])
			}
			if hasMountedDescendant(child) {
				disk.Protected = true
				disk.System = disk.System || !disk.USB
			}
			disk.Partitions = append(disk.Partitions, partition)
		}
		disk.Reclaimable = hasStorageStack(device)
		disks = append(disks, disk)
	}
	return disks, nil
}

func (m *Manager) smartMetadata(ctx context.Context, path string) (*float64, *bool) {
	runner, ok := m.runner.(commandOutputRunner)
	if !ok {
		return nil, nil
	}
	// smartctl can return a non-zero status for a failed SMART check while
	// still returning useful JSON, so parse its output even when it errors.
	output, _ := runner.RunOutput(ctx, "smartctl", "--json", "-a", path)
	var result smartctlOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, nil
	}

	var temperature *float64
	if result.Temperature != nil {
		temperature = result.Temperature.Current
	}
	if temperature == nil && result.Attributes != nil {
		for _, attribute := range result.Attributes.Table {
			if (attribute.ID == 190 || attribute.ID == 194) && attribute.Raw.Value != nil {
				temperature = attribute.Raw.Value
				break
			}
		}
	}
	var health *bool
	if result.SmartStatus != nil {
		health = result.SmartStatus.Passed
	}
	return temperature, health
}

func hasMountedDescendant(device lsblkDevice) bool {
	for _, child := range device.Children {
		if len(mountpoints(child.Mountpoints)) > 0 || hasMountedDescendant(child) {
			return true
		}
	}
	return false
}

func hasStorageStack(device lsblkDevice) bool {
	for _, child := range device.Children {
		if strings.HasPrefix(child.Type, "raid") || child.Type == "lvm" || child.Type == "crypt" || hasStorageStack(child) {
			return true
		}
	}
	return false
}

func (m *Manager) managesUUID(ctx context.Context, uuid string) (bool, error) {
	if uuid == "" || m.isManagedUUID == nil {
		return false, nil
	}
	managed, err := m.isManagedUUID(ctx, uuid)
	if err != nil {
		return false, fmt.Errorf("check managed volume %s: %w", uuid, err)
	}
	return managed, nil
}

func (m *Manager) Partition(ctx context.Context, partitionPath string) (Disk, Partition, error) {
	disks, err := m.List(ctx)
	if err != nil {
		return Disk{}, Partition{}, err
	}
	for _, disk := range disks {
		for _, partition := range disk.Partitions {
			if partition.Path == partitionPath {
				return disk, partition, nil
			}
		}
	}
	return Disk{}, Partition{}, fmt.Errorf("%s is not a physical disk partition", partitionPath)
}

func number(name string) int {
	matches := partitionNumber.FindStringSubmatch(name)
	if len(matches) != 2 {
		return 0
	}
	number, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return number
}

func (m *Manager) fillBlockMetadata(ctx context.Context, partition *Partition) {
	output, err := m.runner.Run(ctx, "blkid", "--output", "export", partition.Path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "TYPE":
			if partition.FileSystem == "" {
				partition.FileSystem = value
			}
		case "UUID":
			if partition.UUID == "" {
				partition.UUID = value
			}
		}
	}
}

func mountpoints(values []*string) []string {
	points := make([]string, 0, len(values))
	for _, value := range values {
		if value != nil && *value != "" {
			points = append(points, *value)
		}
	}
	return points
}

func usage(path string) *FileSystemUsage {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil || stat.Bsize <= 0 {
		return nil
	}
	blockSize := uint64(stat.Bsize)
	return &FileSystemUsage{
		TotalBytes:     stat.Blocks * blockSize,
		UsedBytes:      (stat.Blocks - stat.Bfree) * blockSize,
		AvailableBytes: stat.Bavail * blockSize,
	}
}
