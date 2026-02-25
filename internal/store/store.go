package store

import (
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/nevinsm/gt/internal/config"
	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database connection.
type Store struct {
	db   *sql.DB
	path string
}

// OpenRig opens (or creates) a rig database at $GT_HOME/.store/{rig}.db.
func OpenRig(rig string) (*Store, error) {
	path := filepath.Join(config.StoreDir(), rig+".db")
	s, err := open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open rig database %q: %w", rig, err)
	}
	if err := s.migrateRig(); err != nil {
		s.Close()
		return nil, fmt.Errorf("failed to migrate rig database %q: %w", rig, err)
	}
	return s, nil
}

// OpenTown opens (or creates) the town database at $GT_HOME/.store/town.db.
func OpenTown() (*Store, error) {
	path := filepath.Join(config.StoreDir(), "town.db")
	s, err := open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open town database: %w", err)
	}
	if err := s.migrateTown(); err != nil {
		s.Close()
		return nil, fmt.Errorf("failed to migrate town database: %w", err)
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
