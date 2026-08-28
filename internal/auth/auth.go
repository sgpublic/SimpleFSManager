package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/msteinert/pam/v2"
	"github.com/sgpublic/simplefsmanager/internal/store"
	"golang.org/x/crypto/argon2"
)

const (
	cookieName      = "simplefsmanager_session"
	sessionLifetime = 24 * time.Hour
	argonTime       = 3
	argonMemory     = 64 * 1024
	argonThreads    = 4
	argonKeyLength  = 32
)

type Service struct {
	store *store.Store
}

type Status struct {
	SetupRequired bool   `json:"setupRequired"`
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
}

func New(database *store.Store) *Service {
	return &Service{store: database}
}

func (s *Service) Status(ctx context.Context, request *http.Request) (Status, error) {
	setupRequired, err := s.store.SetupRequired(ctx)
	if err != nil {
		return Status{}, err
	}
	if setupRequired {
		return Status{SetupRequired: true}, nil
	}
	user, err := s.User(ctx, request)
	if err != nil {
		return Status{}, nil
	}
	return Status{Authenticated: true, Username: user.Username}, nil
}

func (s *Service) Bootstrap(ctx context.Context, username, systemPassword, projectPassword string) (string, error) {
	setupRequired, err := s.store.SetupRequired(ctx)
	if err != nil {
		return "", err
	}
	if !setupRequired {
		return "", errors.New("administrator is already configured")
	}
	if err := eligibleLocalUser(username); err != nil {
		return "", err
	}
	if err := authenticatePAM(username, systemPassword); err != nil {
		return "", errors.New("system authentication failed")
	}
	if len(projectPassword) < 12 {
		return "", errors.New("project password must be at least 12 characters")
	}
	user, err := s.store.CreateAdministrator(ctx, username, hashPassword(projectPassword))
	if err != nil {
		return "", err
	}
	return s.createSession(ctx, user.ID)
}

func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	setupRequired, err := s.store.SetupRequired(ctx)
	if err != nil {
		return "", err
	}
	if setupRequired {
		return "", errors.New("administrator setup is required")
	}
	user, err := s.store.UserByUsername(ctx, username)
	if err != nil || !verifyPassword(user.PasswordHash, password) {
		return "", errors.New("invalid username or password")
	}
	return s.createSession(ctx, user.ID)
}

func (s *Service) User(ctx context.Context, request *http.Request) (store.User, error) {
	cookie, err := request.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return store.User{}, errors.New("missing session")
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
			_, _ = writer.Write([]byte(`{"code":"auth_required"}`))
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func SessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false,
	}
}

func ExpiredSessionCookie() *http.Cookie {
	return &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

func (s *Service) createSession(ctx context.Context, userID int64) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	if err := s.store.CreateSession(ctx, tokenHash(token), userID, time.Now().Add(sessionLifetime)); err != nil {
		return "", err
	}
	return token, nil
}

func eligibleLocalUser(username string) error {
	for _, line := range strings.Split(string(mustReadPasswd()), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 7 || fields[0] != username {
			continue
		}
		uid, err := strconv.Atoi(fields[2])
		if err != nil || uid < 1000 || uid == 65534 || fields[6] == "/usr/sbin/nologin" || fields[6] == "/bin/false" {
			return errors.New("administrator must be an eligible local non-root user")
		}
		return nil
	}
	return errors.New("administrator must be an eligible local non-root user")
}

func mustReadPasswd() []byte {
	contents, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil
	}
	return contents
}

func authenticatePAM(username, password string) error {
	transaction, err := pam.StartFunc("simplefsmanager", username, func(style pam.Style, _ string) (string, error) {
		switch style {
		case pam.PromptEchoOff:
			return password, nil
		case pam.PromptEchoOn:
			return username, nil
		default:
			return "", errors.New("unsupported PAM conversation")
		}
	})
	if err != nil {
		return err
	}
	defer transaction.End()
	if err := transaction.Authenticate(pam.DisallowNullAuthtok); err != nil {
		return err
	}
	return transaction.AcctMgmt(pam.DisallowNullAuthtok)
}

func hashPassword(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLength)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
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
		method == http.MethodPost && (path == "/api/auth/bootstrap" || path == "/api/auth/login")
}
