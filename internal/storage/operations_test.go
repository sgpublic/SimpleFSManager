package storage

import (
	"testing"

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
