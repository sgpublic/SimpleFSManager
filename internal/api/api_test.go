package api

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
