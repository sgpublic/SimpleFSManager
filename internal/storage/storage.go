package storage

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

var ErrUdevUnavailable = errors.New("udev monitoring requires a libudev-enabled build")

type UdevEvent struct {
	Action  string
	Devnode string
	Devtype string
}

type FileSystemUsage struct {
	TotalBytes     uint64 `json:"totalBytes"`
	UsedBytes      uint64 `json:"usedBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type Partition struct {
	Path        string           `json:"path"`
	Name        string           `json:"name"`
	Number      int              `json:"number"`
	SizeBytes   uint64           `json:"sizeBytes"`
	FileSystem  string           `json:"fileSystem"`
	UUID        string           `json:"uuid"`
	Mountpoints []string         `json:"mountpoints"`
	Usage       *FileSystemUsage `json:"usage,omitempty"`
}

type Disk struct {
	Path         string      `json:"path"`
	Name         string      `json:"name"`
	Model        string      `json:"model"`
	Serial       string      `json:"serial"`
	SizeBytes    uint64      `json:"sizeBytes"`
	Partitioning string      `json:"partitioning"`
	Transport    string      `json:"transport"`
	USB          bool        `json:"usb"`
	Protected    bool        `json:"protected"`
	System       bool        `json:"system"`
	Reclaimable  bool        `json:"reclaimable"`
	Mountpoints  []string    `json:"mountpoints"`
	Partitions   []Partition `json:"partitions"`
}

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %v: %w: %s", name, args, err, output)
	}
	return output, nil
}

// Manager is the single boundary between the application and Linux storage
// interfaces. Its mutating methods are intentionally not exposed by the API yet.
type Manager struct {
	runner        commandRunner
	isManagedUUID func(context.Context, string) (bool, error)
}

func New(isManagedUUID func(context.Context, string) (bool, error)) *Manager {
	return &Manager{runner: execRunner{}, isManagedUUID: isManagedUUID}
}
