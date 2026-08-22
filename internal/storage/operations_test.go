package storage

import (
	"context"
	"fmt"
	"testing"

	diskpkg "github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/partition/gpt"
)

func TestNextPartitionStartUsesFirstAlignedGap(t *testing.T) {
	table := &gpt.Table{Partitions: []*gpt.Partition{
		{Index: 1, Start: 2048, End: 4095},
		{Index: 3, Start: 8192, End: 16383},
	}}
	start, ok := nextPartitionStart(table, 1024)
	if !ok || start != 4096 {
		t.Fatalf("start, ok = %d, %t; want 4096, true", start, ok)
	}
	if index := nextPartitionIndex(table); index != 2 {
		t.Fatalf("index = %d, want 2", index)
	}
}

func TestLargestPartitionGapUsesLargestAlignedRegion(t *testing.T) {
	table := &gpt.Table{Partitions: []*gpt.Partition{
		{Index: 1, Start: 2048, End: 4095},
		{Index: 2, Start: 12288, End: 16383},
	}}
	start, end, ok := largestPartitionGap(table)
	if !ok || start != 4096 || end != 12287 {
		t.Fatalf("start, end, ok = %d, %d, %t; want 4096, 12287, true", start, end, ok)
	}
}

func TestVolumePath(t *testing.T) {
	for _, path := range []string{"/vol1", "/vol12"} {
		if !volumePath.MatchString(path) {
			t.Errorf("expected valid volume path %q", path)
		}
	}
	for _, path := range []string{"/vol0", "/vol1/child", "/tmp/vol1"} {
		if volumePath.MatchString(path) {
			t.Errorf("expected invalid volume path %q", path)
		}
	}
}

func TestPostorderProcessesLeafStorageBeforeItsParents(t *testing.T) {
	devices := postorder(lsblkDevice{Children: []lsblkDevice{{Name: "sdb1", Type: "part", Children: []lsblkDevice{{Name: "md0", Type: "raid1", Children: []lsblkDevice{{Name: "legacy", Type: "lvm"}}}}}}})
	if len(devices) != 3 || devices[0].Name != "legacy" || devices[1].Name != "md0" || devices[2].Name != "sdb1" {
		t.Fatalf("postorder = %#v", devices)
	}
}

func TestHasRAIDDescendant(t *testing.T) {
	if !hasRAIDDescendant(lsblkDevice{Children: []lsblkDevice{{Type: "raid1"}}}) {
		t.Fatal("expected RAID descendant")
	}
	if hasRAIDDescendant(lsblkDevice{Children: []lsblkDevice{{Type: "lvm"}}}) {
		t.Fatal("did not expect RAID descendant")
	}
}

func TestRequiresRebootForDeferredPartitionTableRefresh(t *testing.T) {
	if !requiresReboot(fmt.Errorf("write partition table: %w", diskpkg.ErrReReadDeferred)) {
		t.Fatal("expected deferred partition table refresh to require a reboot")
	}
}

func TestZonedPartitionGapUsesZoneBoundaries(t *testing.T) {
	table := &gpt.Table{
		Partitions: []*gpt.Partition{
			{Index: 1, Start: 4096, End: 8191},
			{Index: 2, Start: 30000, End: 40959},
		},
	}
	start, end, ok := largestPartitionGapAligned(table, 8192, true)
	if !ok || start != 8192 || end != 24575 {
		t.Fatalf("start, end, ok = %d, %d, %t; want 8192, 24575, true", start, end, ok)
	}
}

func TestZonedManualPartitionSizeMustBeAligned(t *testing.T) {
	device := lsblkDevice{Zoned: "host-managed", ZoneSize: 1024 * 1024}
	if alignment := partitionAlignment(device, 512); alignment != 2048 {
		t.Fatalf("alignment = %d, want 2048 sectors", alignment)
	}
}

type recordingRunner struct {
	name string
	args []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "lsblk" {
		return []byte(`{"blockdevices":[{"name":"sdb","path":"/dev/sdb","type":"disk","mountpoints":[null],"children":[{"name":"sdb1","path":"/dev/sdb1","type":"part","mountpoints":[null]}]}]}`), nil
	}
	r.name = name
	r.args = append([]string(nil), args...)
	return nil, nil
}

func TestFormatSupportsBtrfsAndF2FS(t *testing.T) {
	for _, test := range []struct {
		filesystem string
		command    string
		args       []string
	}{
		{filesystem: "btrfs", command: "mkfs.btrfs", args: []string{"-f", "/dev/sdb1"}},
		{filesystem: "f2fs", command: "mkfs.f2fs", args: []string{"-f", "/dev/sdb1"}},
	} {
		runner := &recordingRunner{}
		manager := &Manager{runner: runner}
		if err := manager.Format(context.Background(), "/dev/sdb1", test.filesystem); err != nil {
			t.Fatal(err)
		}
		if runner.name != test.command || len(runner.args) != len(test.args) {
			t.Fatalf("format %s = %s %q, want %s %q", test.filesystem, runner.name, runner.args, test.command, test.args)
		}
		for index := range test.args {
			if runner.args[index] != test.args[index] {
				t.Fatalf("format %s args = %q, want %q", test.filesystem, runner.args, test.args)
			}
		}
	}
}
