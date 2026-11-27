package storage

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

var ErrUdevUnavailable = errors.New("udev monitoring requires a libudev-enabled build")

const (
	smartMetadataTimeout = 5 * time.Second
	smartDetailTimeout   = 10 * time.Second
	smartConcurrency     = 4
)

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
	Path                      string           `json:"path"`
	Name                      string           `json:"name"`
	Number                    int              `json:"number"`
	SizeBytes                 uint64           `json:"sizeBytes"`
	FileSystem                string           `json:"fileSystem"`
	UUID                      string           `json:"uuid"`
	MountPath                 string           `json:"mountPath,omitempty"`
	Mountpoints               []string         `json:"mountpoints"`
	Usage                     *FileSystemUsage `json:"usage,omitempty"`
	Registered                bool             `json:"registered"`
	AutoMount                 bool             `json:"autoMount"`
	Missing                   bool             `json:"missing"`
	Zoned                     string           `json:"zoned,omitempty"`
	ZoneSizeBytes             uint64           `json:"zoneSizeBytes,omitempty"`
	ZoneWriteGranularityBytes uint64           `json:"zoneWriteGranularityBytes,omitempty"`
}

type Disk struct {
	Path                      string      `json:"path"`
	Name                      string      `json:"name"`
	Model                     string      `json:"model"`
	Serial                    string      `json:"serial"`
	SizeBytes                 uint64      `json:"sizeBytes"`
	Partitioning              string      `json:"partitioning"`
	Transport                 string      `json:"transport"`
	USB                       bool        `json:"usb"`
	Protected                 bool        `json:"protected"`
	System                    bool        `json:"system"`
	Reclaimable               bool        `json:"reclaimable"`
	TemperatureCelsius        *float64    `json:"temperatureCelsius,omitempty"`
	SmartHealth               *bool       `json:"smartHealth,omitempty"`
	SolidState                *bool       `json:"solidState,omitempty"`
	DataWrittenBytes          *uint64     `json:"dataWrittenBytes,omitempty"`
	PowerOnHours              *uint64     `json:"powerOnHours,omitempty"`
	Mountpoints               []string    `json:"mountpoints"`
	Partitions                []Partition `json:"partitions"`
	Missing                   bool        `json:"missing"`
	Zoned                     string      `json:"zoned,omitempty"`
	ZoneSizeBytes             uint64      `json:"zoneSizeBytes,omitempty"`
	ZoneWriteGranularityBytes uint64      `json:"zoneWriteGranularityBytes,omitempty"`
}

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type commandOutputRunner interface {
	RunOutput(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := execRunner{}.RunOutput(ctx, name, args...)
	if err != nil {
		return nil, fmt.Errorf("%s %v: %w: %s", name, args, err, output)
	}
	return output, nil
}

func (execRunner) RunOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Manager is the single boundary between the application and Linux storage
// interfaces. Mutating methods are serialized via opMu to avoid concurrent
// partition, formatting, and mount races.
type Manager struct {
	runner        commandRunner
	isManagedUUID func(context.Context, string) (bool, error)
	opMu          sync.Mutex
}

func New(isManagedUUID func(context.Context, string) (bool, error)) *Manager {
	return &Manager{runner: execRunner{}, isManagedUUID: isManagedUUID}
}
