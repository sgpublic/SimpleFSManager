package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/sgpublic/simplefsmanager/internal/apperr"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
}

func (s *Store) SetupRequired(ctx context.Context) (bool, error) {
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username <> ''`).Scan(&count); err != nil {
		return false, fmt.Errorf("check administrator setup: %w", err)
	}
	return count == 0, nil
}

func (s *Store) CreateAdministrator(ctx context.Context, username, passwordHash string) (User, error) {
	result, err := s.DB.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET username = excluded.username, password_hash = excluded.password_hash
		WHERE users.username = ''`, username, passwordHash)
	if err != nil {
		return User{}, fmt.Errorf("create administrator: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return User{}, fmt.Errorf("count created administrator: %w", err)
	}
	if affected != 1 {
		return User{}, apperr.New(http.StatusConflict, apperr.CodeAuthAlreadyConfigured, "administrator is already configured")
	}
	return User{ID: 1, Username: username, PasswordHash: passwordHash}, nil
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	if err := s.DB.QueryRowContext(ctx, `SELECT id, username, password_hash FROM users WHERE username = ?`, username).Scan(&user.ID, &user.Username, &user.PasswordHash); err != nil {
		if err == sql.ErrNoRows {
			return User{}, apperr.New(http.StatusUnauthorized, apperr.CodeAuthInvalidCredentials, "invalid username or password")
		}
		return User{}, fmt.Errorf("load administrator: %w", err)
	}
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`, tokenHash, userID, expiresAt.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) SessionUser(ctx context.Context, tokenHash string, now time.Time) (User, error) {
	var user User
	var expiresAt string
	if err := s.DB.QueryRowContext(ctx, `
		SELECT users.id, users.username, users.password_hash, sessions.expires_at
		FROM sessions JOIN users ON users.id = sessions.user_id
		WHERE sessions.id = ?`, tokenHash).Scan(&user.ID, &user.Username, &user.PasswordHash, &expiresAt); err != nil {
		if err == sql.ErrNoRows {
			return User{}, apperr.New(http.StatusUnauthorized, apperr.CodeAuthRequired, "invalid session")
		}
		return User{}, fmt.Errorf("load session: %w", err)
	}
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || !expires.After(now) {
		_ = s.DeleteSession(ctx, tokenHash)
		return User{}, apperr.New(http.StatusUnauthorized, apperr.CodeAuthRequired, "expired session")
	}
	return user, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes all sessions that expired at or before now.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
