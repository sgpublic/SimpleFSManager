package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestConfigureVolumePreservesLegacyAndUpdatesMountPath(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	legacy, created, err := database.RegisterVolume(context.Background(), "legacy-uuid", "disk-serial", 1)
	if err != nil || !created || legacy.MountPath != "/vol1" {
		t.Fatalf("legacy registration = %#v, %t, %v", legacy, created, err)
	}
	configured, err := database.ConfigureVolume(context.Background(), legacy.UUID, legacy.DeviceSerial, legacy.PartitionNum, "/mnt/legacy")
	if err != nil {
		t.Fatal(err)
	}
	if configured.MountPath != "/mnt/legacy" || !configured.AutoMount || configured.MountNumber != legacy.MountNumber {
		t.Fatalf("configured volume = %#v", configured)
	}
}

func TestConfigureVolumeRejectsDuplicateMountPath(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, _, err := database.RegisterVolume(context.Background(), "uuid-1", "disk-1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ConfigureVolume(context.Background(), "uuid-2", "disk-2", 1, "/vol1"); err == nil {
		t.Fatal("expected duplicate mount path to be rejected")
	}
}

func TestConfigureVolumePathUpdatesOnlyExistingVolume(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	registered, _, err := database.RegisterVolume(context.Background(), "missing-uuid", "disk-serial", 2)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := database.ConfigureVolumePath(context.Background(), registered.UUID, "/mnt/missing")
	if err != nil || configured.MountPath != "/mnt/missing" {
		t.Fatalf("configured missing volume = %#v, %v", configured, err)
	}
	if _, err := database.ConfigureVolumePath(context.Background(), "not-registered", "/mnt/new"); err == nil {
		t.Fatal("expected an absent volume to be rejected")
	}
}

func TestSetVolumeAutoMount(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	volume, _, err := database.RegisterVolume(context.Background(), "uuid", "serial", 1)
	if err != nil {
		t.Fatal(err)
	}
	volume, err = database.SetVolumeAutoMount(context.Background(), volume.UUID, false)
	if err != nil || volume.AutoMount {
		t.Fatalf("disabled auto-mount volume = %#v, %v", volume, err)
	}
	volume, err = database.SetVolumeAutoMount(context.Background(), volume.UUID, true)
	if err != nil || !volume.AutoMount {
		t.Fatalf("enabled auto-mount volume = %#v, %v", volume, err)
	}
}
