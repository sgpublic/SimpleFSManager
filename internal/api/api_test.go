package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sgpublic/simplefsmanager/internal/auth"
	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
	"github.com/sgpublic/simplefsmanager/internal/usb"
)

func TestLogOperationErrorIncludesOperationContext(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))

	logOperationError(logger, "format partition", errors.New("mkfs.ext4 failed"), "partition", "/dev/sdb1", "filesystem", "ext4")

	log := output.String()
	for _, want := range []string{"operation failed", "operation=\"format partition\"", "error=\"mkfs.ext4 failed\"", "partition=/dev/sdb1", "filesystem=ext4"} {
		if !strings.Contains(log, want) {
			t.Errorf("log %q does not contain %q", log, want)
		}
	}
	if strings.Contains(log, "stack=") {
		t.Errorf("ordinary error log unexpectedly contains a stack: %q", log)
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

	if logs := strings.Count(output.String(), "operation failed"); logs != 1 {
		t.Fatalf("got %d storage failure logs, want 1: %s", logs, output.String())
	}
}

func TestLogOperationSuccessIncludesOperationContext(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))

	logOperationSuccess(logger, "mount volume", "partition", "/dev/sdb1", "uuid", "volume-uuid")

	for _, want := range []string{"operation completed", "operation=\"mount volume\"", "partition=/dev/sdb1", "uuid=volume-uuid"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("log %q does not contain %q", output.String(), want)
		}
	}
}

func TestRequestErrorLoggerLogsClientAndServerErrors(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	handler := requestErrorLogger(logger)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/partitions", nil))

	for _, want := range []string{"request failed", "method=POST", "path=/api/partitions", "status=400"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("log %q does not contain %q", output.String(), want)
		}
	}
}

func TestMountPathOpenAPIAllowsUUIDWithoutPartitionPath(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	disks := storage.New(database.IsManagedUUID)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, usb.New(disks), http.NotFoundHandler())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("OpenAPI status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}

	var document struct {
		Paths map[string]struct {
			Post struct {
				RequestBody struct {
					Content map[string]struct {
						Schema struct {
							Required []string `json:"required"`
						} `json:"schema"`
					} `json:"content"`
				} `json:"requestBody"`
			} `json:"post"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	for _, field := range document.Paths["/api/volumes/mount-path"].Post.RequestBody.Content["application/json"].Schema.Required {
		if field == "partitionPath" || field == "partitionUUID" {
			t.Fatalf("mount-path API unexpectedly requires %q", field)
		}
	}
}

func TestSmartDetailsRouteRequiresDiskPath(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	user, err := database.CreateAdministrator(context.Background(), "test-user", "password-hash")
	if err != nil {
		t.Fatal(err)
	}
	token := "test-session"
	tokenSum := sha256.Sum256([]byte(token))
	if err := database.CreateSession(context.Background(), base64.RawURLEncoding.EncodeToString(tokenSum[:]), user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	disks := storage.New(database.IsManagedUUID)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, usb.New(disks), http.NotFoundHandler())
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/disks/smart", nil)
	request.AddCookie(auth.SessionCookie(token))
	handler.ServeHTTP(response, request)

	if response.Code == http.StatusNotFound {
		t.Fatalf("SMART details route returned 404 instead of validating diskPath")
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("SMART details status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestSmartDetailsRouteRequiresAuthentication(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	disks := storage.New(database.IsManagedUUID)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, usb.New(disks), http.NotFoundHandler())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/disks/smart", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("SMART details status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
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

func TestWriteErrorIncludesCodeAndMessage(t *testing.T) {
	response := httptest.NewRecorder()
	writeError(response, http.StatusInternalServerError, errors.New("lsblk failed: permission denied"))

	if body := response.Body.String(); !strings.Contains(body, `"code":"internal_error"`) || !strings.Contains(body, `"message":"lsblk failed: permission denied"`) {
		t.Fatalf("error response = %s", body)
	}
}

func TestUnauthenticatedStatusDoesNotExposeAdministratorUsername(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.CreateAdministrator(context.Background(), "bound-user", "password-hash"); err != nil {
		t.Fatal(err)
	}

	disks := storage.New(database.IsManagedUUID)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, usb.New(disks), http.NotFoundHandler())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	var status map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if _, exists := status["username"]; exists {
		t.Fatalf("unauthenticated status exposed username: %s", response.Body.String())
	}
}
