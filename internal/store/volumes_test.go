package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func boolPtr(v bool) *bool { return &v }

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
	configured, err := database.ConfigureVolume(context.Background(), legacy.UUID, legacy.DeviceSerial, legacy.PartitionNum, "/mnt/legacy", nil)
	if err != nil {
		t.Fatal(err)
	}
	if configured.MountPath != "/mnt/legacy" || !configured.AutoMount || configured.MountNumber != legacy.MountNumber {
		t.Fatalf("configured volume = %#v", configured)
	}
}

func TestConfigureVolumeDefaultsAutoMountFalseAndPreservesExistingSetting(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	created, err := database.ConfigureVolume(context.Background(), "new-uuid", "disk-1", 1, "/mnt/new", nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.AutoMount {
		t.Fatalf("new volume autoMount = true, want false")
	}

	updated, err := database.ConfigureVolume(context.Background(), "new-uuid", "disk-1", 1, "/mnt/renamed", nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AutoMount || updated.MountPath != "/mnt/renamed" {
		t.Fatalf("updated volume = %#v, want mountPath=/mnt/renamed autoMount=false", updated)
	}

	enabled, err := database.ConfigureVolume(context.Background(), "new-uuid", "disk-1", 1, "/mnt/enabled", boolPtr(true))
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.AutoMount || enabled.MountPath != "/mnt/enabled" {
		t.Fatalf("enabled volume = %#v, want mountPath=/mnt/enabled autoMount=true", enabled)
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
	if _, err := database.ConfigureVolume(context.Background(), "uuid-2", "disk-2", 1, "/vol1", nil); err == nil {
		t.Fatal("expected duplicate mount path to be rejected")
	}
}

func TestConfigureVolumePathUpdatesOnlyExistingVolumeAndPreservesAutoMount(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	registered, _, err := database.RegisterVolume(context.Background(), "missing-uuid", "disk-serial", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SetVolumeAutoMount(context.Background(), registered.UUID, false); err != nil {
		t.Fatal(err)
	}
	configured, err := database.ConfigureVolumePath(context.Background(), registered.UUID, "/mnt/missing", nil)
	if err != nil || configured.MountPath != "/mnt/missing" || configured.AutoMount {
		t.Fatalf("configured missing volume = %#v, %v", configured, err)
	}
	if _, err := database.ConfigureVolumePath(context.Background(), "not-registered", "/mnt/new", nil); err == nil {
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

func TestDeleteExpiredSessions(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	user, err := database.CreateAdministrator(context.Background(), "admin", "hash")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := database.CreateSession(context.Background(), "expired-tok", user.ID, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateSession(context.Background(), "active-tok", user.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := database.DeleteExpiredSessions(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SessionUser(context.Background(), "expired-tok", now); err == nil {
		t.Fatal("expected expired session to be removed")
	}
	if got, err := database.SessionUser(context.Background(), "active-tok", now); err != nil || got.Username != "admin" {
		t.Fatalf("active session = %#v, %v", got, err)
	}
}
