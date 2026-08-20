package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type fakeRunner struct {
	run func(string, ...string) ([]byte, error)
}

func TestListEncodesEmptyCollectionsAsArrays(t *testing.T) {
	manager := &Manager{runner: fakeRunner{run: func(name string, _ ...string) ([]byte, error) {
		if name != "lsblk" {
			return nil, fmt.Errorf("unexpected command %s", name)
		}
		return []byte(`{"blockdevices":[{"name":"sdc","path":"/dev/sdc","type":"disk","size":1000,"mountpoints":[null]}]}`), nil
	}}}

	disks, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(disks)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"partitions":null`) || strings.Contains(string(encoded), `"mountpoints":null`) {
		t.Fatalf("collections must be arrays: %s", encoded)
	}
}

func TestListReportsUsageForMountedPartition(t *testing.T) {
	mountpoint := t.TempDir()
	manager := &Manager{runner: fakeRunner{run: func(name string, _ ...string) ([]byte, error) {
		if name != "lsblk" {
			return nil, fmt.Errorf("unexpected command %s", name)
		}
		return []byte(fmt.Sprintf(`{"blockdevices":[{"name":"sdc","path":"/dev/sdc","type":"disk","size":1000,"mountpoints":[null],"children":[{"name":"sdc1","path":"/dev/sdc1","type":"part","size":900,"mountpoints":[%q]}]}]}`, mountpoint)), nil
	}}}

	disks, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	usage := disks[0].Partitions[0].Usage
	if usage == nil || usage.TotalBytes == 0 || usage.UsedBytes > usage.TotalBytes {
		t.Fatalf("usage = %#v", usage)
	}
}

func (r fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	return r.run(name, args...)
}

func TestListReturnsPhysicalDisksAndFallsBackToBlkid(t *testing.T) {
	manager := &Manager{runner: fakeRunner{run: func(name string, args ...string) ([]byte, error) {
		switch name {
		case "lsblk":
			return []byte(`{
              "blockdevices": [
                {"name":"loop0","path":"/dev/loop0","type":"loop","size":1},
                {"name":"sdb","path":"/dev/sdb","type":"disk","size":1000,"model":" Test disk ","serial":" S1 ","pttype":"gpt","mountpoints":[null],"children":[
                  {"name":"sdb1","path":"/dev/sdb1","type":"part","size":900,"fstype":"","uuid":"","mountpoints":["/vol1"]}
                ]}
              ]
            }`), nil
		case "blkid":
			if len(args) != 3 || args[2] != "/dev/sdb1" {
				t.Fatalf("blkid args = %q", args)
			}
			return []byte("TYPE=ext4\nUUID=volume-uuid\n"), nil
		default:
			return nil, fmt.Errorf("unexpected command %s", name)
		}
	}}}

	disks, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 1 {
		t.Fatalf("disks = %d, want 1", len(disks))
	}
	disk := disks[0]
	if disk.Model != "Test disk" || disk.Serial != "S1" || !disk.Protected {
		t.Fatalf("disk = %#v", disk)
	}
	if len(disk.Partitions) != 1 {
		t.Fatalf("partitions = %d, want 1", len(disk.Partitions))
	}
	partition := disk.Partitions[0]
	if partition.FileSystem != "ext4" || partition.UUID != "volume-uuid" {
		t.Fatalf("partition = %#v", partition)
	}
}

func TestListMarksInactiveRAIDAndLVMStackReclaimable(t *testing.T) {
	manager := &Manager{runner: fakeRunner{run: func(name string, _ ...string) ([]byte, error) {
		if name != "lsblk" {
			return nil, fmt.Errorf("unexpected command %s", name)
		}
		return []byte(`{"blockdevices":[{"name":"nvme1n1","path":"/dev/nvme1n1","type":"disk","size":1000,"mountpoints":[null],"children":[{"name":"nvme1n1p1","path":"/dev/nvme1n1p1","type":"part","size":900,"mountpoints":[null],"children":[{"name":"md127","path":"/dev/md127","type":"raid1","size":900,"mountpoints":[null],"children":[{"name":"legacy","path":"/dev/mapper/legacy","type":"lvm","size":900,"mountpoints":[null]}]}]}]}]}`), nil
	}}}

	disks, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 1 || !disks[0].Reclaimable || disks[0].Protected || disks[0].System {
		t.Fatalf("disk = %#v", disks)
	}
}

func TestListProtectsMountedStorageStack(t *testing.T) {
	manager := &Manager{runner: fakeRunner{run: func(name string, _ ...string) ([]byte, error) {
		if name != "lsblk" {
			return nil, fmt.Errorf("unexpected command %s", name)
		}
		return []byte(`{"blockdevices":[{"name":"sdb","path":"/dev/sdb","type":"disk","size":1000,"mountpoints":[null],"children":[{"name":"sdb1","path":"/dev/sdb1","type":"part","size":900,"mountpoints":[null],"children":[{"name":"md0","path":"/dev/md0","type":"raid1","size":900,"mountpoints":[null],"children":[{"name":"system","path":"/dev/mapper/system","type":"lvm","size":900,"mountpoints":["/"]}]}]}]}]}`), nil
	}}}

	disks, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 1 || !disks[0].Reclaimable || !disks[0].Protected || !disks[0].System {
		t.Fatalf("disk = %#v", disks)
	}
}

type smartMetadataRunner struct {
	lsblkOutput []byte
	smartOutput []byte
	smartErr    error
}

func (r smartMetadataRunner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	if name != "lsblk" {
		return nil, fmt.Errorf("unexpected command %s", name)
	}
	return r.lsblkOutput, nil
}

func (r smartMetadataRunner) RunOutput(_ context.Context, name string, _ ...string) ([]byte, error) {
	if name != "smartctl" {
		return nil, fmt.Errorf("unexpected command %s", name)
	}
	return r.smartOutput, r.smartErr
}

func TestListReadsSMARTMetadataEvenWhenSmartctlReportsFailure(t *testing.T) {
	temperature := 62.0
	manager := &Manager{runner: smartMetadataRunner{
		lsblkOutput: []byte(`{"blockdevices":[{"name":"sdb","path":"/dev/sdb","type":"disk","size":1000,"mountpoints":[null]}]}`),
		smartOutput: []byte(fmt.Sprintf(`{"temperature":{"current":%[1]f},"smart_status":{"passed":false}}`, temperature)),
		smartErr:    fmt.Errorf("smartctl exited with status 8"),
	}}

	disks, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if disks[0].TemperatureCelsius == nil || *disks[0].TemperatureCelsius != temperature {
		t.Fatalf("temperature = %#v, want %v", disks[0].TemperatureCelsius, temperature)
	}
	if disks[0].SmartHealth == nil || *disks[0].SmartHealth {
		t.Fatalf("smart health = %#v, want false", disks[0].SmartHealth)
	}
}

func TestListFallsBackToATAAttributeTemperature(t *testing.T) {
	manager := &Manager{runner: smartMetadataRunner{
		lsblkOutput: []byte(`{"blockdevices":[{"name":"sdb","path":"/dev/sdb","type":"disk","size":1000,"mountpoints":[null]}]}`),
		smartOutput: []byte(`{"ata_smart_attributes":{"table":[{"id":194,"raw":{"value":37}}]}}`),
	}}

	disks, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if disks[0].TemperatureCelsius == nil || *disks[0].TemperatureCelsius != 37 {
		t.Fatalf("temperature = %#v, want 37", disks[0].TemperatureCelsius)
	}
}
