package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"github.com/sgpublic/simplefsmanager/internal/store"
	"golang.org/x/crypto/argon2"
)

const (
	DefaultAdminUsername    = "admin"
	cookieName              = "simplefsmanager_session"
	sessionLifetime         = 24 * time.Hour
	minPasswordLength       = 12
	generatedPasswordLength = 16
	// Unambiguous alphanumeric charset (excludes 0/O, 1/l/I) for readable console output.
	passwordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	argonTime        = 3
	argonMemory      = 64 * 1024
	argonThreads     = 4
	argonKeyLength   = 32
)

// dummyPasswordHash is verified when a login targets an unknown username so
// failed lookups spend the same Argon2id time as invalid password attempts.
var dummyPasswordHash = func() string {
	salt := []byte("simplefsmanager!")
	key := argon2.IDKey([]byte("dummy-password-for-timing"), salt, argonTime, argonMemory, argonThreads, argonKeyLength)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}()

type Service struct {
	store *store.Store
}

type Status struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
}

// BootstrapOptions configures initial administrator creation on first startup.
// Both fields are ignored once an administrator already exists in the database.
type BootstrapOptions struct {
	Username string
	Password string
}

type InitResult struct {
	Username          string
	GeneratedPassword string
	Created           bool
}

func New(database *store.Store) *Service {
	return &Service{store: database}
}

// EnsureAdministrator creates the initial administrator account if none exists
// in the database. Subsequent startups ignore opts and keep existing credentials.
func (s *Service) EnsureAdministrator(ctx context.Context, opts BootstrapOptions) (InitResult, error) {
	existing, exists, err := s.store.Administrator(ctx)
	if err != nil {
		return InitResult{}, err
	}
	if exists {
		return InitResult{Username: existing.Username, Created: false}, nil
	}

	username := strings.TrimSpace(opts.Username)
	if username == "" {
		username = DefaultAdminUsername
	}
	password := opts.Password
	var generated string
	if password == "" {
		generated, err = generatePassword(generatedPasswordLength)
		if err != nil {
			return InitResult{}, err
		}
		password = generated
	}

	encodedHash, err := hashPassword(password)
	if err != nil {
		return InitResult{}, err
	}
	if _, err := s.store.CreateAdministrator(ctx, username, encodedHash); err != nil {
		return InitResult{}, err
	}
	return InitResult{
		Username:          username,
		GeneratedPassword: generated,
		Created:           true,
	}, nil
}

func (s *Service) Status(ctx context.Context, request *http.Request) (Status, error) {
	user, err := s.User(ctx, request)
	if err != nil {
		return Status{}, nil
	}
	return Status{Authenticated: true, Username: user.Username}, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	user, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		if apperr.IsCode(err, apperr.CodeAuthInvalidCredentials) {
			_ = verifyPassword(dummyPasswordHash, password)
			return "", err
		}
		return "", err
	}
	if !verifyPassword(user.PasswordHash, password) {
		return "", apperr.New(http.StatusUnauthorized, apperr.CodeAuthInvalidCredentials, "invalid username or password")
	}
	return s.createSession(ctx, user.ID)
}

// ChangePassword verifies currentPassword, updates the administrator password,
// invalidates all previous sessions, and returns a fresh session token.
func (s *Service) ChangePassword(ctx context.Context, user store.User, currentPassword, newPassword string) (string, error) {
	if !verifyPassword(user.PasswordHash, currentPassword) {
		return "", apperr.New(http.StatusUnauthorized, apperr.CodeAuthInvalidCredentials, "current password is invalid")
	}
	if len(newPassword) < minPasswordLength {
		return "", apperr.WithParams(
			http.StatusBadRequest,
			apperr.CodeAuthPasswordTooShort,
			fmt.Sprintf("password must be at least %d characters", minPasswordLength),
			map[string]any{"minLength": minPasswordLength},
		)
	}
	encodedHash, err := hashPassword(newPassword)
	if err != nil {
		return "", err
	}
	if err := s.store.UpdateAdministratorPassword(ctx, user.ID, encodedHash); err != nil {
		return "", err
	}
	if err := s.store.DeleteAllSessions(ctx); err != nil {
		return "", err
	}
	return s.createSession(ctx, user.ID)
}

func (s *Service) User(ctx context.Context, request *http.Request) (store.User, error) {
	cookie, err := request.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return store.User{}, apperr.New(http.StatusUnauthorized, apperr.CodeAuthRequired, "missing session")
	}
	return s.store.SessionUser(ctx, tokenHash(cookie.Value), time.Now())
}

func (s *Service) Logout(ctx context.Context, request *http.Request) {
	cookie, err := request.Cookie(cookieName)
	if err == nil && cookie.Value != "" {
		_ = s.store.DeleteSession(ctx, tokenHash(cookie.Value))
	}
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.Path, "/api/") || publicEndpoint(request.Method, request.URL.Path) {
			next.ServeHTTP(writer, request)
			return
		}
		if _, err := s.User(request.Context(), request); err != nil {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"code":"auth_required","message":"sign in is required"}`))
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func SessionCookie(token string) *http.Cookie {
	return sessionCookie(token, false)
}

func SessionCookieForRequest(request *http.Request, token string) *http.Cookie {
	return sessionCookie(token, isSecureRequest(request))
}

func sessionCookie(token string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	}
}

func ExpiredSessionCookie() *http.Cookie {
	return expiredSessionCookie(false)
}

func ExpiredSessionCookieForRequest(request *http.Request) *http.Cookie {
	return expiredSessionCookie(isSecureRequest(request))
}

func expiredSessionCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
	}
}

func isSecureRequest(request *http.Request) bool {
	if request == nil {
		return false
	}
	if request.TLS != nil {
		return true
	}
	proto := request.Header.Get("X-Forwarded-Proto")
	if first, _, ok := strings.Cut(proto, ","); ok {
		proto = first
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

func (s *Service) createSession(ctx context.Context, userID int64) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	now := time.Now()
	if err := s.store.CreateSession(ctx, tokenHash(token), userID, now.Add(sessionLifetime)); err != nil {
		return "", err
	}
	_ = s.store.DeleteExpiredSessions(ctx, now)
	return token, nil
}

func generatePassword(length int) (string, error) {
	max := big.NewInt(int64(len(passwordAlphabet)))
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate initial password: %w", err)
		}
		result[i] = passwordAlphabet[n.Int64()]
	}
	return string(result), nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLength)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return false
	}
	var memory uint32
	var timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	if memory < 8*uint32(threads) || timeCost == 0 || threads == 0 || len(expected) == 0 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func publicEndpoint(method, path string) bool {
	return method == http.MethodGet && (path == "/api/health" || path == "/api/build-info" || path == "/api/auth/status") ||
		method == http.MethodPost && path == "/api/auth/login"
}
