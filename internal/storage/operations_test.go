package storage

import (
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
