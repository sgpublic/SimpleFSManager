package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
)

func TestLogOperationErrorIncludesOperationContext(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))

	logOperationError(logger, "format partition", errors.New("mkfs.ext4 failed"), "partition", "/dev/sdb1", "filesystem", "ext4")

	log := output.String()
	for _, want := range []string{"storage operation failed", "operation=\"format partition\"", "error=\"mkfs.ext4 failed\"", "partition=/dev/sdb1", "filesystem=ext4"} {
		if !strings.Contains(log, want) {
			t.Errorf("log %q does not contain %q", log, want)
		}
	}
}

func TestMergeManagedVolumesAddsMissingRegisteredPartition(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	missing, _, err := database.RegisterVolume(context.Background(), "missing-uuid", "disk-serial", 2)
	if err != nil {
		t.Fatal(err)
	}
	present, _, err := database.RegisterVolume(context.Background(), "present-uuid", "disk-serial", 1)
	if err != nil {
		t.Fatal(err)
	}
	disks, err := mergeManagedVolumes(context.Background(), []storage.Disk{{Partitions: []storage.Partition{{Path: "/dev/sdb1", UUID: present.UUID}}}}, []store.Volume{missing, present}, database)
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 2 || !disks[0].Partitions[0].Registered || disks[0].Partitions[0].Missing {
		t.Fatalf("present partition = %#v", disks)
	}
	if !disks[1].Missing || !disks[1].Protected || len(disks[1].Partitions) != 1 {
		t.Fatalf("missing disk = %#v", disks[1])
	}
	partition := disks[1].Partitions[0]
	if !partition.Missing || !partition.Registered || partition.UUID != missing.UUID || partition.MountPath != missing.MountPath {
		t.Fatalf("missing partition = %#v", partition)
	}
}

func TestOperationErrorLogsOnlyInternalErrors(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))

	operationError(logger, "format partition", errors.New("mkfs.ext4 failed"), "partition", "/dev/sdb1")
	operationError(logger, "format partition", errors.New("refusing to modify mounted partition /dev/sdb1"), "partition", "/dev/sdb1")

	if logs := strings.Count(output.String(), "storage operation failed"); logs != 1 {
		t.Fatalf("got %d storage failure logs, want 1: %s", logs, output.String())
	}
}

func TestRecovererLogsPanic(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	handler := recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("unexpected failure")
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/partitions/format", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusInternalServerError)
	}
	for _, want := range []string{"panic while serving request", "method=POST", "path=/api/partitions/format", "panic=\"unexpected failure\"", "stack="} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("log %q does not contain %q", output.String(), want)
		}
	}
}
