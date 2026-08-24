package storage

import (
	"context"
	"fmt"
	"io"
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

func TestValidateMountPath(t *testing.T) {
	for _, path := range []string{"/vol1", "/mnt/archive", "/home/user/data"} {
		if err := ValidateMountPath(path); err != nil {
			t.Errorf("ValidateMountPath(%q) = %v", path, err)
		}
	}
	for _, path := range []string{"", "/", "relative", "/mnt/../etc", "/etc/data", "/proc/data"} {
		if err := ValidateMountPath(path); err == nil {
			t.Errorf("ValidateMountPath(%q) succeeded", path)
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

func TestGPTBackupStartUsesDefaultAndExistingGPTGeometry(t *testing.T) {
	const (
		diskSize  = 1024 * 1024 * 1024
		blockSize = 4096
	)
	start, err := gptBackupStart(diskSize, blockSize, &gpt.Table{})
	if err != nil {
		t.Fatal(err)
	}
	if want := uint64(diskSize - blockSize - gptPartitionArrayBytes); start != want {
		t.Fatalf("default backup start = %d, want %d", start, want)
	}

	table := &gpt.Table{LogicalSectorSize: blockSize}
	table.Resize(diskSize)
	start, err = gptBackupStart(diskSize, blockSize, table)
	if err != nil {
		t.Fatal(err)
	}
	if want := (table.LastDataSector() + 1) * blockSize; start != want {
		t.Fatalf("existing backup start = %d, want %d", start, want)
	}
}

func TestFinalZoneUsesLastPartialZone(t *testing.T) {
	start, length, err := finalZone(10*1024+100, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if start != 10*1024 || length != 100 {
		t.Fatalf("final zone = start %d, length %d; want 10240, 100", start, length)
	}
}

func TestZeroFillWritesSequentialAlignedChunks(t *testing.T) {
	writer := &sequentialWriter{next: 4096}
	if err := zeroFill(writer, 4096, 3*1024*1024, 4096); err != nil {
		t.Fatal(err)
	}
	if writer.next != 3*1024*1024+4096 {
		t.Fatalf("write end = %d, want %d", writer.next, 3*1024*1024+4096)
	}
	if writer.writes != 3 {
		t.Fatalf("writes = %d, want 3", writer.writes)
	}
}

func TestZeroFillRejectsUnalignedRange(t *testing.T) {
	if err := zeroFill(&sequentialWriter{}, 1, 4096, 4096); err == nil {
		t.Fatal("expected unaligned zero-fill to fail")
	}
}

type sequentialWriter struct {
	next   uint64
	writes int
}

func (w *sequentialWriter) WriteAt(data []byte, offset int64) (int, error) {
	if offset < 0 || uint64(offset) != w.next {
		return 0, fmt.Errorf("write offset %d, want %d", offset, w.next)
	}
	if len(data)%4096 != 0 {
		return 0, fmt.Errorf("write length %d is not aligned", len(data))
	}
	w.next += uint64(len(data))
	w.writes++
	return len(data), nil
}

var _ io.WriterAt = (*sequentialWriter)(nil)

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
