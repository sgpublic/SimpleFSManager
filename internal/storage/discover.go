package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"golang.org/x/sys/unix"
)

var partitionNumber = regexp.MustCompile(`(?:p)?([0-9]+)$`)

var lsblkArgs = []string{
	"--json", "--bytes", "--zoned", "--output",
	"NAME,PATH,TYPE,SIZE,MODEL,SERIAL,TRAN,ROTA,PTTYPE,FSTYPE,UUID,MOUNTPOINTS,ZONED,ZONE-SZ,ZONE-WGRAN",
}

type lsblkOutput struct {
	BlockDevices []lsblkDevice `json:"blockdevices"`
}

type lsblkDevice struct {
	Name                 string        `json:"name"`
	Path                 string        `json:"path"`
	Type                 string        `json:"type"`
	Size                 uint64        `json:"size"`
	Model                string        `json:"model"`
	Serial               string        `json:"serial"`
	Transport            string        `json:"tran"`
	Rota                 *bool         `json:"rota"`
	PTType               string        `json:"pttype"`
	FSType               string        `json:"fstype"`
	UUID                 string        `json:"uuid"`
	Mountpoints          []*string     `json:"mountpoints"`
	Children             []lsblkDevice `json:"children"`
	Zoned                string        `json:"zoned"`
	ZoneSize             uint64        `json:"zone-sz"`
	ZoneWriteGranularity uint64        `json:"zone-wgran"`
}

type smartctlOutput struct {
	Device *struct {
		Protocol string `json:"protocol"`
	} `json:"device"`
	RotationRate     *int    `json:"rotation_rate"`
	LogicalBlockSize *uint64 `json:"logical_block_size"`
	Temperature      *struct {
		Current *float64 `json:"current"`
	} `json:"temperature"`
	SmartStatus *struct {
		Passed *bool `json:"passed"`
	} `json:"smart_status"`
	PowerOnTime *struct {
		Hours *uint64 `json:"hours"`
	} `json:"power_on_time"`
	NVMeSmart *struct {
		Temperature      *float64 `json:"temperature"`
		PowerOnHours     *uint64  `json:"power_on_hours"`
		DataUnitsWritten *uint64  `json:"data_units_written"`
	} `json:"nvme_smart_health_information_log"`
	Attributes *struct {
		Table []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Raw  struct {
				Value *float64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
	DeviceStatistics *struct {
		Pages []struct {
			Number int `json:"number"`
			Table  []struct {
				Name  string  `json:"name"`
				Value *uint64 `json:"value"`
			} `json:"table"`
		} `json:"pages"`
	} `json:"ata_device_statistics"`
}

type smartSummary struct {
	temperature      *float64
	health           *bool
	solidState       *bool
	dataWrittenBytes *uint64
	powerOnHours     *uint64
}

// List returns physical disks with topology, usage, and SMART health/temperature
// metadata populated concurrently across disks.
func (m *Manager) List(ctx context.Context) ([]Disk, error) {
	disks, err := m.ListTopology(ctx)
	if err != nil {
		return nil, err
	}
	m.populateSmartMetadata(ctx, disks)
	return disks, nil
}

// ListTopology returns physical disks and partitions without querying SMART,
// keeping internal validations and hotplug reconciliation fast.
func (m *Manager) ListTopology(ctx context.Context) ([]Disk, error) {
	rawDevices, err := m.listRawDevices(ctx)
	if err != nil {
		return nil, err
	}
	disks := make([]Disk, 0, len(rawDevices))
	for _, device := range rawDevices {
		if device.Type != "disk" {
			continue
		}
		disk, err := m.buildDisk(ctx, device)
		if err != nil {
			return nil, err
		}
		disks = append(disks, disk)
	}
	return disks, nil
}

func (m *Manager) listRawDevices(ctx context.Context) ([]lsblkDevice, error) {
	output, err := m.runner.Run(ctx, "lsblk", lsblkArgs...)
	if err != nil {
		return nil, fmt.Errorf("list block devices: %w", err)
	}
	var result lsblkOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("decode lsblk output: %w", err)
	}
	return result.BlockDevices, nil
}

func (m *Manager) buildDisk(ctx context.Context, device lsblkDevice) (Disk, error) {
	disk := Disk{
		Path:                      device.Path,
		Name:                      device.Name,
		Model:                     strings.TrimSpace(device.Model),
		Serial:                    strings.TrimSpace(device.Serial),
		Transport:                 strings.TrimSpace(device.Transport),
		USB:                       strings.EqualFold(strings.TrimSpace(device.Transport), "usb"),
		SizeBytes:                 device.Size,
		Partitioning:              device.PTType,
		Mountpoints:               mountpoints(device.Mountpoints),
		Partitions:                make([]Partition, 0),
		Zoned:                     device.Zoned,
		ZoneSizeBytes:             device.ZoneSize,
		ZoneWriteGranularityBytes: device.ZoneWriteGranularity,
	}
	if device.Rota != nil {
		ssd := !*device.Rota
		disk.SolidState = &ssd
	}
	disk.Protected = len(disk.Mountpoints) > 0
	// Host-managed disks are formatted and mounted as whole-disk volumes.
	disk.System = len(disk.Mountpoints) > 0 && !disk.USB && !isHostManaged(device)
	if isHostManaged(device) {
		partition := Partition{
			Path:                      device.Path,
			Name:                      device.Name,
			Number:                    0,
			SizeBytes:                 device.Size,
			FileSystem:                device.FSType,
			UUID:                      device.UUID,
			Mountpoints:               mountpoints(device.Mountpoints),
			Zoned:                     device.Zoned,
			ZoneSizeBytes:             device.ZoneSize,
			ZoneWriteGranularityBytes: device.ZoneWriteGranularity,
		}
		if partition.FileSystem == "" || partition.UUID == "" {
			m.fillBlockMetadata(ctx, &partition)
		}
		if len(partition.Mountpoints) > 0 {
			managed, err := m.managesUUID(ctx, partition.UUID)
			if err != nil {
				return Disk{}, err
			}
			disk.System = !managed
			partition.Usage = usage(partition.Mountpoints[0])
		}
		disk.Partitions = append(disk.Partitions, partition)
	}
	for _, child := range device.Children {
		if child.Type != "part" {
			continue
		}
		partition := Partition{
			Path:                      child.Path,
			Name:                      child.Name,
			Number:                    number(child.Name),
			SizeBytes:                 child.Size,
			FileSystem:                child.FSType,
			UUID:                      child.UUID,
			Mountpoints:               mountpoints(child.Mountpoints),
			Zoned:                     child.Zoned,
			ZoneSizeBytes:             child.ZoneSize,
			ZoneWriteGranularityBytes: child.ZoneWriteGranularity,
		}
		if partition.FileSystem == "" || partition.UUID == "" {
			m.fillBlockMetadata(ctx, &partition)
		}
		if len(partition.Mountpoints) > 0 {
			disk.Protected = true
			managed, err := m.managesUUID(ctx, partition.UUID)
			if err != nil {
				return Disk{}, err
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
	return disk, nil
}

func (m *Manager) populateSmartMetadata(ctx context.Context, disks []Disk) {
	if len(disks) == 0 {
		return
	}
	if _, ok := m.runner.(commandOutputRunner); !ok {
		return
	}
	sem := make(chan struct{}, smartConcurrency)
	var wg sync.WaitGroup
	for i := range disks {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			smartCtx, cancel := context.WithTimeout(ctx, smartMetadataTimeout)
			defer cancel()
			summary := m.smartMetadata(smartCtx, disks[idx].Path)
			disks[idx].TemperatureCelsius = summary.temperature
			disks[idx].SmartHealth = summary.health
			if summary.solidState != nil {
				disks[idx].SolidState = summary.solidState
			}
			disks[idx].DataWrittenBytes = summary.dataWrittenBytes
			disks[idx].PowerOnHours = summary.powerOnHours
		}(i)
	}
	wg.Wait()
}

func (m *Manager) smartMetadata(ctx context.Context, path string) smartSummary {
	runner, ok := m.runner.(commandOutputRunner)
	if !ok {
		return smartSummary{}
	}
	// smartctl can return a non-zero status for a failed SMART check while
	// still returning useful JSON, so parse its output even when it errors.
	output, _ := runner.RunOutput(ctx, "smartctl", "--json", "-a", path)
	var result smartctlOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return smartSummary{}
	}

	var summary smartSummary
	if result.Temperature != nil {
		summary.temperature = result.Temperature.Current
	}
	if summary.temperature == nil && result.NVMeSmart != nil && result.NVMeSmart.Temperature != nil {
		temp := *result.NVMeSmart.Temperature
		if temp > 200 {
			temp -= 273
		}
		summary.temperature = &temp
	}
	if summary.temperature == nil && result.Attributes != nil {
		for _, attribute := range result.Attributes.Table {
			if (attribute.ID == 190 || attribute.ID == 194) && attribute.Raw.Value != nil {
				summary.temperature = attribute.Raw.Value
				break
			}
		}
	}
	if result.SmartStatus != nil {
		summary.health = result.SmartStatus.Passed
	}

	if result.RotationRate != nil {
		ssd := *result.RotationRate == 0
		summary.solidState = &ssd
	} else if result.NVMeSmart != nil || (result.Device != nil && strings.EqualFold(result.Device.Protocol, "NVMe")) {
		ssd := true
		summary.solidState = &ssd
	}

	summary.powerOnHours = extractPowerOnHours(result)
	summary.dataWrittenBytes = extractDataWrittenBytes(result)
	return summary
}

func extractPowerOnHours(result smartctlOutput) *uint64 {
	if result.PowerOnTime != nil && result.PowerOnTime.Hours != nil {
		hours := *result.PowerOnTime.Hours
		return &hours
	}
	if result.NVMeSmart != nil && result.NVMeSmart.PowerOnHours != nil {
		hours := *result.NVMeSmart.PowerOnHours
		return &hours
	}
	if result.Attributes != nil {
		for _, attr := range result.Attributes.Table {
			if attr.ID == 9 && attr.Raw.Value != nil && *attr.Raw.Value >= 0 {
				hours := uint64(*attr.Raw.Value)
				return &hours
			}
		}
	}
	return nil
}

func extractDataWrittenBytes(result smartctlOutput) *uint64 {
	// NVMe reports data units written in units of 1,000 * 512 bytes (512,000 bytes).
	if result.NVMeSmart != nil && result.NVMeSmart.DataUnitsWritten != nil {
		bytes := *result.NVMeSmart.DataUnitsWritten * 512000
		return &bytes
	}
	blockSize := uint64(512)
	if result.LogicalBlockSize != nil && *result.LogicalBlockSize > 0 {
		blockSize = *result.LogicalBlockSize
	}
	if result.DeviceStatistics != nil {
		for _, page := range result.DeviceStatistics.Pages {
			for _, entry := range page.Table {
				if strings.EqualFold(entry.Name, "Logical Sectors Written") && entry.Value != nil {
					bytes := *entry.Value * blockSize
					return &bytes
				}
			}
		}
	}
	if result.Attributes != nil {
		var fallbackNAND *uint64
		for _, attr := range result.Attributes.Table {
			if attr.Raw.Value == nil || *attr.Raw.Value < 0 {
				continue
			}
			raw := uint64(*attr.Raw.Value)
			lowerName := strings.ToLower(attr.Name)
			switch attr.ID {
			case 241, 246:
				bytes := ataWrittenAttributeBytes(raw, lowerName, blockSize)
				return &bytes
			case 249:
				if strings.Contains(lowerName, "gib") || strings.Contains(lowerName, "gb") {
					bytes := raw * 1024 * 1024 * 1024
					fallbackNAND = &bytes
				}
			}
		}
		if fallbackNAND != nil {
			return fallbackNAND
		}
	}
	return nil
}

func ataWrittenAttributeBytes(raw uint64, lowerName string, blockSize uint64) uint64 {
	switch {
	case strings.Contains(lowerName, "32mib"):
		return raw * 32 * 1024 * 1024
	case strings.Contains(lowerName, "gib"):
		return raw * 1024 * 1024 * 1024
	case strings.Contains(lowerName, "gb"):
		return raw * 1000 * 1000 * 1000
	case strings.Contains(lowerName, "mib"):
		return raw * 1024 * 1024
	case strings.Contains(lowerName, "mb"):
		return raw * 1000 * 1000
	default:
		return raw * blockSize
	}
}

// Smart returns the complete smartctl JSON document for a physical disk.
// smartctl uses non-zero exit codes for some valid health states, so its exit
// status is ignored when the command output is valid JSON.
func (m *Manager) Smart(ctx context.Context, path string) ([]byte, error) {
	if _, err := m.device(ctx, path); err != nil {
		return nil, err
	}
	runner, ok := m.runner.(commandOutputRunner)
	if !ok {
		return nil, apperr.New(http.StatusBadGateway, apperr.CodeSmartQueryFailed, "SMART details are unavailable")
	}
	smartCtx, cancel := context.WithTimeout(ctx, smartDetailTimeout)
	defer cancel()
	output, commandErr := runner.RunOutput(smartCtx, "smartctl", "--json", "-a", path)
	if json.Valid(output) {
		return output, nil
	}
	if commandErr != nil {
		return nil, apperr.Wrap(http.StatusBadGateway, apperr.CodeSmartQueryFailed, fmt.Sprintf("read SMART data: %v", commandErr), commandErr)
	}
	return nil, apperr.New(http.StatusBadGateway, apperr.CodeSmartQueryFailed, "read SMART data: invalid smartctl JSON")
}

func (m *Manager) DiskByPath(ctx context.Context, diskPath string) (Disk, error) {
	disks, err := m.ListTopology(ctx)
	if err != nil {
		return Disk{}, err
	}
	for _, disk := range disks {
		if disk.Path == diskPath {
			return disk, nil
		}
	}
	return Disk{}, apperr.New(http.StatusBadRequest, apperr.CodeInvalidDisk, fmt.Sprintf("%s is not a physical disk", diskPath))
}

func (m *Manager) PartitionByNumber(ctx context.Context, diskPath string, number int) (Disk, Partition, error) {
	disk, err := m.DiskByPath(ctx, diskPath)
	if err != nil {
		return Disk{}, Partition{}, err
	}
	for _, partition := range disk.Partitions {
		if partition.Number == number {
			return disk, partition, nil
		}
	}
	return Disk{}, Partition{}, apperr.New(http.StatusNotFound, apperr.CodePartitionNotFound, fmt.Sprintf("partition %d does not exist on %s", number, diskPath))
}

func (m *Manager) diskWithRaw(ctx context.Context, diskPath string) (Disk, lsblkDevice, error) {
	rawDevices, err := m.listRawDevices(ctx)
	if err != nil {
		return Disk{}, lsblkDevice{}, err
	}
	for _, device := range rawDevices {
		if device.Type == "disk" && device.Path == diskPath {
			disk, err := m.buildDisk(ctx, device)
			if err != nil {
				return Disk{}, lsblkDevice{}, err
			}
			return disk, device, nil
		}
	}
	return Disk{}, lsblkDevice{}, apperr.New(http.StatusBadRequest, apperr.CodeInvalidDisk, fmt.Sprintf("%s is not a physical disk", diskPath))
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
	disks, err := m.ListTopology(ctx)
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
	return Disk{}, Partition{}, apperr.New(http.StatusBadRequest, apperr.CodeInvalidPartition, fmt.Sprintf("%s is not a physical disk partition", partitionPath))
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
