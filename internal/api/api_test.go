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

	"github.com/danielgtaylor/huma/v2"
	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"github.com/sgpublic/simplefsmanager/internal/auth"
	"github.com/sgpublic/simplefsmanager/internal/storage"
	"github.com/sgpublic/simplefsmanager/internal/store"
	"github.com/sgpublic/simplefsmanager/internal/usb"
	"github.com/sgpublic/simplefsmanager/internal/volume"
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
	disks := mergeManagedVolumes(
		[]storage.Disk{{Partitions: []storage.Partition{{Path: "/dev/sdb1", UUID: present.UUID}}}},
		[]store.Volume{missing, present},
	)
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

func TestOperationErrorLogsOnlyInternalErrorsAndMapsStatus(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))

	internalErr := operationError(logger, "format partition", errors.New("mkfs.ext4 failed"), "partition", "/dev/sdb1")
	clientErr := operationError(logger, "format partition", apperr.New(http.StatusConflict, apperr.CodeMountedDiskProtected, "refusing to modify mounted partition /dev/sdb1"), "partition", "/dev/sdb1")

	if logs := strings.Count(output.String(), "operation failed"); logs != 1 {
		t.Fatalf("got %d storage failure logs, want 1: %s", logs, output.String())
	}
	if statusErr, ok := internalErr.(huma.StatusError); !ok || statusErr.GetStatus() != http.StatusInternalServerError {
		t.Fatalf("internalErr status = %#v, want 500", internalErr)
	}
	if statusErr, ok := clientErr.(huma.StatusError); !ok || statusErr.GetStatus() != http.StatusConflict {
		t.Fatalf("clientErr status = %#v, want 409", clientErr)
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
	volumes := volume.New(database, disks)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, volumes, usb.New(disks), http.NotFoundHandler())
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
	volumes := volume.New(database, disks)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, volumes, usb.New(disks), http.NotFoundHandler())
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
	volumes := volume.New(database, disks)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, volumes, usb.New(disks), http.NotFoundHandler())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/disks/smart", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("SMART details status = %d, want %d: %s", response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestRecovererLogsPanicWithoutLeakingDetails(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	handler := recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret internal pointer")
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/partitions/format", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "secret internal pointer") {
		t.Fatalf("recoverer leaked panic details to client: %s", response.Body.String())
	}
	for _, want := range []string{"panic while serving request", "method=POST", "path=/api/partitions/format", "panic=\"secret internal pointer\"", "stack="} {
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
	volumes := volume.New(database, disks)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, volumes, usb.New(disks), http.NotFoundHandler())
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

func TestLoginRateLimiting(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.CreateAdministrator(context.Background(), "admin", "invalid-hash"); err != nil {
		t.Fatal(err)
	}

	disks := storage.New(database.IsManagedUUID)
	volumes := volume.New(database, disks)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, volumes, usb.New(disks), http.NotFoundHandler())

	for attempt := 0; attempt < authRateLimitMax; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.RemoteAddr = "192.0.2.10:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	req.RemoteAddr = "192.0.2.10:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), apperr.CodeAuthRateLimited) {
		t.Fatalf("rate-limited body = %s, want code %s", rec.Body.String(), apperr.CodeAuthRateLimited)
	}
}

func TestVolumeAutoMountRouteUpdatesSettingIndependently(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	user, err := database.CreateAdministrator(ctx, "test-user", "password-hash")
	if err != nil {
		t.Fatal(err)
	}
	token := "test-session"
	tokenSum := sha256.Sum256([]byte(token))
	if err := database.CreateSession(ctx, base64.RawURLEncoding.EncodeToString(tokenSum[:]), user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ConfigureVolume(ctx, "vol-1", "serial-1", 1, "/mnt/vol1", nil); err != nil {
		t.Fatal(err)
	}

	disks := storage.New(database.IsManagedUUID)
	volumes := volume.New(database, disks)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, volumes, usb.New(disks), http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodPost, "/api/volumes/auto-mount", strings.NewReader(`{"uuid":"vol-1","autoMount":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(auth.SessionCookie(token))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auto-mount enable status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	vol, err := database.VolumeByUUID(ctx, "vol-1")
	if err != nil || !vol.AutoMount {
		t.Fatalf("volume after enable = %#v, %v; want AutoMount=true", vol, err)
	}

	merged := mergeManagedVolumes(nil, []store.Volume{vol})
	if len(merged) != 1 || !merged[0].Partitions[0].AutoMount {
		t.Fatalf("merged missing volume AutoMount = %#v, want true", merged)
	}
}

func TestDisksRouteUsesCacheAndSupportsForceRefresh(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	user, err := database.CreateAdministrator(ctx, "test-user", "password-hash")
	if err != nil {
		t.Fatal(err)
	}
	token := "test-session"
	tokenSum := sha256.Sum256([]byte(token))
	if err := database.CreateSession(ctx, base64.RawURLEncoding.EncodeToString(tokenSum[:]), user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	calls := 0
	diskCache := storage.NewDiskCache(func(context.Context) ([]storage.Disk, error) {
		calls++
		return []storage.Disk{{
			Path:        "/dev/sdb",
			Name:        "sdb",
			SizeBytes:   uint64(calls) * 1000,
			Mountpoints: []string{},
			Partitions:  []storage.Partition{},
		}}, nil
	})
	disks := storage.New(database.IsManagedUUID)
	volumes := volume.New(database, disks)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, volumes, usb.New(disks), http.NotFoundHandler(), diskCache)

	getDisks := func(url string) (uint64, string) {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.AddCookie(auth.SessionCookie(token))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d: %s", url, rec.Code, rec.Body.String())
		}
		var body struct {
			Disks     []storage.Disk `json:"disks"`
			UpdatedAt string         `json:"updatedAt"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Disks) != 1 || body.UpdatedAt == "" {
			t.Fatalf("GET %s body = %#v", url, body)
		}
		return body.Disks[0].SizeBytes, body.UpdatedAt
	}

	size1, _ := getDisks("/api/disks")
	size2, _ := getDisks("/api/disks")
	if size1 != 1000 || size2 != 1000 || calls != 1 {
		t.Fatalf("cached GETs = (%d, %d), calls = %d; want (1000, 1000), 1", size1, size2, calls)
	}

	size3, _ := getDisks("/api/disks?refresh=true")
	if size3 != 2000 || calls != 2 {
		t.Fatalf("forced GET = %d, calls = %d; want 2000, 2", size3, calls)
	}
}

func TestChangePasswordRoute(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	authSvc := auth.New(database)
	if _, err := authSvc.EnsureAdministrator(ctx, auth.BootstrapOptions{Password: "initial-password-123"}); err != nil {
		t.Fatal(err)
	}
	oldToken, err := authSvc.Login(ctx, auth.DefaultAdminUsername, "initial-password-123")
	if err != nil {
		t.Fatal(err)
	}

	disks := storage.New(database.IsManagedUUID)
	volumes := volume.New(database, disks)
	handler := New(database, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), disks, volumes, usb.New(disks), http.NotFoundHandler())

	// Unauthenticated request must fail with 401.
	unauthRec := httptest.NewRecorder()
	handler.ServeHTTP(unauthRec, httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(`{"currentPassword":"initial-password-123","newPassword":"updated-password-456"}`)))
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", unauthRec.Code)
	}

	// Authenticated request with valid current password succeeds and rotates session.
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(`{"currentPassword":"initial-password-123","newPassword":"updated-password-456"}`))
	req.AddCookie(auth.SessionCookie(oldToken))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change password status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	var newCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "simplefsmanager_session" {
			newCookie = cookie
		}
	}
	if newCookie == nil || newCookie.Value == "" || newCookie.Value == oldToken {
		t.Fatalf("expected rotated session cookie, got %#v", newCookie)
	}

	// Login with updated password succeeds.
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"updated-password-456"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login with new password status = %d, want 200: %s", loginRec.Code, loginRec.Body.String())
	}
}
