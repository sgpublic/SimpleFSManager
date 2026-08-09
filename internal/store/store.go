package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store owns persistent application state. Disk discovery is deliberately kept
// outside this package so only user-managed state reaches SQLite.
type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping SQLite: %w", err)
	}

	store := &Store{DB: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) migrate() error {
	_, err := s.DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id),
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS volumes (
			id INTEGER PRIMARY KEY,
			uuid TEXT NOT NULL UNIQUE,
			device_serial TEXT NOT NULL,
			partition_number INTEGER NOT NULL,
			mount_number INTEGER NOT NULL UNIQUE,
			mount_path TEXT NOT NULL UNIQUE,
			auto_mount INTEGER NOT NULL DEFAULT 1 CHECK (auto_mount IN (0, 1)),
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("migrate SQLite: %w", err)
	}
	if err := s.ensureUsersUsername(); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique ON users(username) WHERE username <> ''`); err != nil {
		return fmt.Errorf("create username index: %w", err)
	}
	return nil
}

func (s *Store) ensureUsersUsername() error {
	rows, err := s.DB.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return fmt.Errorf("inspect users schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("read users schema: %w", err)
		}
		if name == "username" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate users schema: %w", err)
	}
	if _, err := s.DB.Exec(`ALTER TABLE users ADD COLUMN username TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add username column: %w", err)
	}
	return nil
}
