package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalidTransition is returned when a phase transition is not allowed.
var ErrInvalidTransition = errors.New("invalid phase transition")

// baseStore wraps a SQLite database connection. Used internally by WorldStore and SphereStore.
type baseStore struct {
	db   *sql.DB
	path string
}

// WorldStore wraps a world-scoped SQLite database connection.
type WorldStore struct {
	baseStore
}

// SphereStore wraps a sphere-scoped SQLite database connection.
type SphereStore struct {
	baseStore
}

// OpenWorld opens (or creates) a world database at $SOL_HOME/.store/{world}.db.
func OpenWorld(world string) (*WorldStore, error) {
	path := filepath.Join(config.StoreDir(), world+".db")
	base, err := open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open world database %q: %w", world, err)
	}
	s := &WorldStore{baseStore: *base}
	if err := s.migrateWorld(); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "store: close failed after migration error: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to migrate world database %q: %w", world, err)
	}
	return s, nil
}

// OpenSphere opens (or creates) the sphere database at $SOL_HOME/.store/sphere.db.
func OpenSphere() (*SphereStore, error) {
	path := filepath.Join(config.StoreDir(), "sphere.db")
	base, err := open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sphere database: %w", err)
	}
	s := &SphereStore{baseStore: *base}
	if err := s.migrateSphere(); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "store: close failed after migration error: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to migrate sphere database: %w", err)
	}
	return s, nil
}

// OpenNoMigrate opens a database without running migrations. Useful for
// reading the schema version of a database before deciding whether to migrate.
func OpenNoMigrate(path string) (*WorldStore, error) {
	base, err := open(path)
	if err != nil {
		return nil, err
	}
	return &WorldStore{baseStore: *base}, nil
}

func open(path string) (*baseStore, error) {
	// Ensure the parent directory exists before opening (SQLite cannot create directories).
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}
	// Embed pragmas in the DSN so they apply to every connection in the pool.
	dsn := fmt.Sprintf("%s?_pragma=journal_mode%%3DWAL&_pragma=busy_timeout%%3D5000&_pragma=foreign_keys%%3DON", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return &baseStore{db: db, path: path}, nil
}

// Close closes the database connection.
func (s *baseStore) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for read-only queries.
func (s *baseStore) DB() *sql.DB {
	return s.db
}

// Path returns the filesystem path to the database file.
func (s *baseStore) Path() string {
	return s.path
}

// Checkpoint forces a WAL checkpoint, flushing all WAL data into the main
// database file. Uses TRUNCATE mode to also remove the WAL file afterward.
func (s *baseStore) Checkpoint() error {
	_, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if err != nil {
		return fmt.Errorf("failed to checkpoint database %q: %w", s.path, err)
	}
	return nil
}
