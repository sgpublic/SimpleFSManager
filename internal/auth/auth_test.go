package auth

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"github.com/sgpublic/simplefsmanager/internal/store"
)

func TestPublicEndpoint(t *testing.T) {
	if !publicEndpoint(http.MethodGet, "/api/build-info") {
		t.Fatal("build-info endpoint must be public")
	}
	if publicEndpoint(http.MethodPost, "/api/build-info") {
		t.Fatal("build-info endpoint must not allow POST without authentication")
	}
	if publicEndpoint(http.MethodPost, "/api/auth/password") {
		t.Fatal("password endpoint must require authentication")
	}
}

func TestRateLimiterWindowAndReset(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limiter := NewRateLimiter(2, time.Minute)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("127.0.0.1") || !limiter.Allow("127.0.0.1") {
		t.Fatal("expected first two attempts to be allowed")
	}
	if limiter.Allow("127.0.0.1") {
		t.Fatal("expected third attempt within window to be rejected")
	}
	if !limiter.Allow("10.0.0.2") {
		t.Fatal("expected independent client IP to be allowed")
	}

	now = now.Add(61 * time.Second)
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("expected attempt after window expiry to be allowed")
	}

	limiter.Reset("127.0.0.1")
	if !limiter.Allow("127.0.0.1") || !limiter.Allow("127.0.0.1") {
		t.Fatal("expected reset to clear prior attempts")
	}
}

func TestSessionCookieForRequestRespectsTLSAndProxyHeaders(t *testing.T) {
	httpReq := httptest.NewRequest(http.MethodPost, "http://example.com/api/auth/login", nil)
	if cookie := SessionCookieForRequest(httpReq, "tok"); cookie.Secure {
		t.Fatal("plain HTTP request must not set Secure cookie")
	}

	proxyReq := httptest.NewRequest(http.MethodPost, "http://example.com/api/auth/login", nil)
	proxyReq.Header.Set("X-Forwarded-Proto", "https, http")
	if cookie := SessionCookieForRequest(proxyReq, "tok"); !cookie.Secure {
		t.Fatal("X-Forwarded-Proto=https must set Secure cookie")
	}

	tlsReq := httptest.NewRequest(http.MethodPost, "https://example.com/api/auth/login", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	if cookie := SessionCookieForRequest(tlsReq, "tok"); !cookie.Secure {
		t.Fatal("TLS request must set Secure cookie")
	}
}

func TestEnsureAdministratorGeneratesRandomPasswordAndIgnoresSubsequentEnv(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	svc := New(database)

	first, err := svc.EnsureAdministrator(ctx, BootstrapOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.Username != DefaultAdminUsername || len(first.GeneratedPassword) != generatedPasswordLength {
		t.Fatalf("first init = %#v", first)
	}

	if _, err := svc.Login(ctx, DefaultAdminUsername, first.GeneratedPassword); err != nil {
		t.Fatalf("login with generated password failed: %v", err)
	}

	// Second startup with env vars set must NOT overwrite existing credentials.
	second, err := svc.EnsureAdministrator(ctx, BootstrapOptions{
		Username: "other-admin",
		Password: "override-attempt-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.GeneratedPassword != "" || second.Username != DefaultAdminUsername {
		t.Fatalf("second init = %#v, want existing admin without changes", second)
	}
	if _, err := svc.Login(ctx, "other-admin", "override-attempt-password"); !apperr.IsCode(err, apperr.CodeAuthInvalidCredentials) {
		t.Fatalf("env override should be ignored on subsequent startup, got err=%v", err)
	}
	if _, err := svc.Login(ctx, DefaultAdminUsername, first.GeneratedPassword); err != nil {
		t.Fatalf("original generated password stopped working: %v", err)
	}
}

func TestEnsureAdministratorUsesEnvOnFirstStartup(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	svc := New(database)

	res, err := svc.EnsureAdministrator(ctx, BootstrapOptions{
		Username: "ops-admin",
		Password: "initial-env-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Username != "ops-admin" || res.GeneratedPassword != "" {
		t.Fatalf("env init = %#v", res)
	}
	if _, err := svc.Login(ctx, "ops-admin", "initial-env-password"); err != nil {
		t.Fatalf("login with env credentials failed: %v", err)
	}
}

func TestChangePasswordValidatesAndRevokesOldSessions(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	svc := New(database)
	if _, err := svc.EnsureAdministrator(ctx, BootstrapOptions{Password: "old-strong-password"}); err != nil {
		t.Fatal(err)
	}

	oldToken, err := svc.Login(ctx, DefaultAdminUsername, "old-strong-password")
	if err != nil {
		t.Fatal(err)
	}
	user, exists, err := database.Administrator(ctx)
	if err != nil || !exists {
		t.Fatalf("load admin: %v, exists=%t", err, exists)
	}

	if _, err := svc.ChangePassword(ctx, user, "wrong-password", "new-strong-password"); !apperr.IsCode(err, apperr.CodeAuthInvalidCredentials) {
		t.Fatalf("wrong current password err = %v, want %s", err, apperr.CodeAuthInvalidCredentials)
	}
	if _, err := svc.ChangePassword(ctx, user, "old-strong-password", "short"); !apperr.IsCode(err, apperr.CodeAuthPasswordTooShort) {
		t.Fatalf("short new password err = %v, want %s", err, apperr.CodeAuthPasswordTooShort)
	}

	newToken, err := svc.ChangePassword(ctx, user, "old-strong-password", "new-strong-password-123")
	if err != nil || newToken == "" || newToken == oldToken {
		t.Fatalf("ChangePassword failed: token=%q err=%v", newToken, err)
	}

	oldReq := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	oldReq.AddCookie(SessionCookie(oldToken))
	if _, err := svc.User(ctx, oldReq); err == nil {
		t.Fatal("expected old session to be revoked after password change")
	}

	newReq := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	newReq.AddCookie(SessionCookie(newToken))
	if _, err := svc.User(ctx, newReq); err != nil {
		t.Fatalf("expected new session to be valid: %v", err)
	}

	if _, err := svc.Login(ctx, DefaultAdminUsername, "old-strong-password"); !apperr.IsCode(err, apperr.CodeAuthInvalidCredentials) {
		t.Fatalf("old password should no longer work, got %v", err)
	}
	if _, err := svc.Login(ctx, DefaultAdminUsername, "new-strong-password-123"); err != nil {
		t.Fatalf("new password login failed: %v", err)
	}
}
