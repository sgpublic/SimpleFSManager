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

func TestPasswordHashVerifyAndLogin(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	encoded, err := hashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPassword(encoded, "correct-horse-battery-staple") {
		t.Fatal("expected valid password to verify")
	}
	if verifyPassword(encoded, "wrong-password") {
		t.Fatal("expected wrong password to fail verification")
	}

	if _, err := database.CreateAdministrator(context.Background(), "admin", encoded); err != nil {
		t.Fatal(err)
	}
	svc := New(database)
	if _, err := svc.Login(context.Background(), "unknown", "wrong-password"); !apperr.IsCode(err, apperr.CodeAuthInvalidCredentials) {
		t.Fatalf("unknown user error = %v, want %s", err, apperr.CodeAuthInvalidCredentials)
	}
	if _, err := svc.Login(context.Background(), "admin", "wrong-password"); !apperr.IsCode(err, apperr.CodeAuthInvalidCredentials) {
		t.Fatalf("wrong password error = %v, want %s", err, apperr.CodeAuthInvalidCredentials)
	}
	token, err := svc.Login(context.Background(), "admin", "correct-horse-battery-staple")
	if err != nil || token == "" {
		t.Fatalf("valid login failed: token=%q err=%v", token, err)
	}
}
