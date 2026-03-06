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

// Store wraps a SQLite database connection.
type Store struct {
	db   *sql.DB
	path string
}

// OpenWorld opens (or creates) a world database at $SOL_HOME/.store/{world}.db.
func OpenWorld(world string) (*Store, error) {
	path := filepath.Join(config.StoreDir(), world+".db")
	s, err := open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open world database %q: %w", world, err)
	}
	if err := s.migrateWorld(); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "store: close failed after migration error: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to migrate world database %q: %w", world, err)
	}
	return s, nil
}

// OpenSphere opens (or creates) the sphere database at $SOL_HOME/.store/sphere.db.
func OpenSphere() (*Store, error) {
	path := filepath.Join(config.StoreDir(), "sphere.db")
	s, err := open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sphere database: %w", err)
	}
	if err := s.migrateSphere(); err != nil {
		if closeErr := s.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "store: close failed after migration error: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to migrate sphere database: %w", err)
	}
	return s, nil
}

func open(path string) (*Store, error) {
	// Embed pragmas in the DSN so they apply to every connection in the pool.
	dsn := fmt.Sprintf("%s?_pragma=journal_mode%%3DWAL&_pragma=busy_timeout%%3D5000&_pragma=foreign_keys%%3DON", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for read-only queries.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Path returns the filesystem path to the database file.
func (s *Store) Path() string {
	return s.path
}

// Checkpoint forces a WAL checkpoint, flushing all WAL data into the main
// database file. Uses TRUNCATE mode to also remove the WAL file afterward.
func (s *Store) Checkpoint() error {
	_, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if err != nil {
		return fmt.Errorf("failed to checkpoint database %q: %w", s.path, err)
	}
	return nil
}
