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
