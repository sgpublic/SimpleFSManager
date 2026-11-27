package volume

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
)

func boolPtr(v bool) *bool { return &v }

type fakeStorage struct {
	disks    []storage.Disk
	mountErr map[string]error
	mounted  []string
}

func (s *fakeStorage) ListTopology(context.Context) ([]storage.Disk, error) {
	return s.disks, nil
}

func (s *fakeStorage) Partition(_ context.Context, path string) (storage.Disk, storage.Partition, error) {
	for _, disk := range s.disks {
		for _, part := range disk.Partitions {
			if part.Path == path {
				return disk, part, nil
			}
		}
	}
	return storage.Disk{}, storage.Partition{}, apperr.New(http.StatusBadRequest, apperr.CodeInvalidPartition, "not found")
}

func (s *fakeStorage) Mount(_ context.Context, path, target, _ string) error {
	if err := s.mountErr[target]; err != nil {
		return err
	}
	s.mounted = append(s.mounted, path+"->"+target)
	return nil
}

func (s *fakeStorage) Unmount(string) error {
	return nil
}

func TestRecoverContinuesAfterSingleVolumeFailure(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.ConfigureVolume(context.Background(), "uuid-1", "s1", 1, "/mnt/bad", boolPtr(true)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ConfigureVolume(context.Background(), "uuid-2", "s2", 1, "/mnt/good", boolPtr(true)); err != nil {
		t.Fatal(err)
	}

	mountErr := errors.New("busy")
	backend := &fakeStorage{
		disks: []storage.Disk{
			{
				Path: "/dev/sdb",
				Partitions: []storage.Partition{
					{Path: "/dev/sdb1", Number: 1, UUID: "uuid-1", FileSystem: "ext4"},
					{Path: "/dev/sdb2", Number: 2, UUID: "uuid-2", FileSystem: "ext4"},
				},
			},
		},
		mountErr: map[string]error{"/mnt/bad": mountErr},
	}

	err = New(database, backend).Recover(context.Background())
	if !errors.Is(err, mountErr) {
		t.Fatalf("Recover error = %v, want wrapped %v", err, mountErr)
	}
	if len(backend.mounted) != 1 || backend.mounted[0] != "/dev/sdb2->/mnt/good" {
		t.Fatalf("mounted = %v, want [/dev/sdb2->/mnt/good]", backend.mounted)
	}
}

func TestMountAndUnmountDoNotAlterAutoMountSetting(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	if _, err := database.ConfigureVolume(ctx, "vol-uuid", "s1", 1, "/mnt/data", boolPtr(false)); err != nil {
		t.Fatal(err)
	}

	backend := &fakeStorage{
		disks: []storage.Disk{{
			Path: "/dev/sdb",
			Partitions: []storage.Partition{{
				Path:       "/dev/sdb1",
				Number:     1,
				UUID:       "vol-uuid",
				FileSystem: "ext4",
			}},
		}},
	}
	mgr := New(database, backend)

	// Mounting when auto-mount is false must keep auto-mount false.
	mounted, err := mgr.Mount(ctx, "/dev/sdb1")
	if err != nil {
		t.Fatal(err)
	}
	if mounted.AutoMount {
		t.Fatal("Mount unexpectedly enabled AutoMount")
	}
	persisted, err := database.VolumeByUUID(ctx, "vol-uuid")
	if err != nil || persisted.AutoMount {
		t.Fatalf("persisted volume after Mount = %#v, %v; want AutoMount=false", persisted, err)
	}

	// Independently enable auto-mount, then unmount: auto-mount must remain true.
	if _, err := mgr.SetAutoMount(ctx, "vol-uuid", true); err != nil {
		t.Fatal(err)
	}
	unmounted, err := mgr.Unmount(ctx, "vol-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if !unmounted.AutoMount {
		t.Fatal("Unmount unexpectedly disabled AutoMount")
	}
	persisted, err = database.VolumeByUUID(ctx, "vol-uuid")
	if err != nil || !persisted.AutoMount {
		t.Fatalf("persisted volume after Unmount = %#v, %v; want AutoMount=true", persisted, err)
	}

	// While still attached, a background Recover must not immediately remount
	// the manually unmounted volume.
	backend.mounted = nil
	if err := mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if len(backend.mounted) != 0 {
		t.Fatalf("Recover unexpectedly remounted manually unmounted volume: %v", backend.mounted)
	}

	// After the disk is detached and reattached, Recover should auto-mount again.
	savedDisks := backend.disks
	backend.disks = nil
	if err := mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	backend.disks = savedDisks
	if err := mgr.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if len(backend.mounted) != 1 || backend.mounted[0] != "/dev/sdb1->/mnt/data" {
		t.Fatalf("Recover after reattach mounted = %v, want [/dev/sdb1->/mnt/data]", backend.mounted)
	}
}

func TestMountReturnsStructuredErrorWhenPathNotConfigured(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	backend := &fakeStorage{
		disks: []storage.Disk{{
			Path: "/dev/sdb",
			Partitions: []storage.Partition{{
				Path:       "/dev/sdb1",
				Number:     1,
				UUID:       "unregistered-uuid",
				FileSystem: "ext4",
			}},
		}},
	}

	_, err = New(database, backend).Mount(context.Background(), "/dev/sdb1")
	if !apperr.IsCode(err, apperr.CodeMountPathNotConfigured) {
		t.Fatalf("Mount error = %v, want code %s", err, apperr.CodeMountPathNotConfigured)
	}
}
