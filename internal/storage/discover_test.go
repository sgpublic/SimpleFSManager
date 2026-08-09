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
