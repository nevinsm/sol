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

// WorldStore wraps a per-world SQLite database (writs, MRs, dependencies, ledger, history).
type WorldStore struct {
	db   *sql.DB
	path string
}

// SphereStore wraps the sphere-wide SQLite database (agents, messages, escalations, caravans, worlds).
type SphereStore struct {
	db   *sql.DB
	path string
}

// Store is a transitional shim that embeds both WorldStore and SphereStore.
// It is NOT a valid runtime object — a Store cannot simultaneously be world and sphere.
// The depth-0 db and path fields shadow the promoted fields from the embedded types,
// preserving backward compatibility for existing callers that access s.db directly.
// This type will be removed once all consumers have migrated to WorldStore or SphereStore.
type Store struct {
	db   *sql.DB // depth 0: shadows WorldStore.db and SphereStore.db
	path string  // depth 0: shadows WorldStore.path and SphereStore.path
	WorldStore
	SphereStore
}

// OpenWorldStore opens (or creates) a world database at $SOL_HOME/.store/{world}.db.
// Returns a *WorldStore scoped exclusively to the per-world schema.
func OpenWorldStore(world string) (*WorldStore, error) {
	path := filepath.Join(config.StoreDir(), world+".db")
	db, err := openDB(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open world database %q: %w", world, err)
	}
	ws := &WorldStore{db: db, path: path}
	if err := ws.migrateWorld(); err != nil {
		if closeErr := ws.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "store: close failed after migration error: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to migrate world database %q: %w", world, err)
	}
	return ws, nil
}

// OpenSphereStore opens (or creates) the sphere database at $SOL_HOME/.store/sphere.db.
// Returns a *SphereStore scoped exclusively to the sphere-wide schema.
func OpenSphereStore() (*SphereStore, error) {
	path := filepath.Join(config.StoreDir(), "sphere.db")
	db, err := openDB(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sphere database: %w", err)
	}
	ss := &SphereStore{db: db, path: path}
	if err := ss.migrateSphere(); err != nil {
		if closeErr := ss.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "store: close failed after migration error: %v\n", closeErr)
		}
		return nil, fmt.Errorf("failed to migrate sphere database: %w", err)
	}
	return ss, nil
}

// OpenWorld opens (or creates) a world database and returns the transitional *Store shim.
// Deprecated: prefer OpenWorldStore for new callers.
func OpenWorld(world string) (*Store, error) {
	ws, err := OpenWorldStore(world)
	if err != nil {
		return nil, err
	}
	return &Store{db: ws.db, path: ws.path, WorldStore: *ws}, nil
}

// OpenSphere opens (or creates) the sphere database and returns the transitional *Store shim.
// Deprecated: prefer OpenSphereStore for new callers.
func OpenSphere() (*Store, error) {
	ss, err := OpenSphereStore()
	if err != nil {
		return nil, err
	}
	return &Store{db: ss.db, path: ss.path, SphereStore: *ss}, nil
}

// OpenNoMigrate opens a database without running migrations. Useful for
// reading the schema version of a database before deciding whether to migrate.
func OpenNoMigrate(path string) (*Store, error) {
	return open(path)
}

// openDB opens a SQLite connection with WAL pragmas embedded in the DSN.
func openDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s?_pragma=journal_mode%%3DWAL&_pragma=busy_timeout%%3D5000&_pragma=foreign_keys%%3DON", path)
	return sql.Open("sqlite", dsn)
}

// open opens a database and returns a bare *Store shim (no migration, no embedded stores).
// Used internally by OpenNoMigrate and CloneWorldData.
func open(path string) (*Store, error) {
	db, err := openDB(path)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, path: path}, nil
}

// — WorldStore utility methods —

// Close closes the WorldStore database connection.
func (ws *WorldStore) Close() error {
	return ws.db.Close()
}

// DB returns the underlying *sql.DB for the world store.
func (ws *WorldStore) DB() *sql.DB {
	return ws.db
}

// Path returns the filesystem path to the world database file.
func (ws *WorldStore) Path() string {
	return ws.path
}

// Checkpoint forces a WAL checkpoint on the world database.
func (ws *WorldStore) Checkpoint() error {
	_, err := ws.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if err != nil {
		return fmt.Errorf("failed to checkpoint database %q: %w", ws.path, err)
	}
	return nil
}

// — SphereStore utility methods —

// Close closes the SphereStore database connection.
func (ss *SphereStore) Close() error {
	return ss.db.Close()
}

// DB returns the underlying *sql.DB for the sphere store.
func (ss *SphereStore) DB() *sql.DB {
	return ss.db
}

// Path returns the filesystem path to the sphere database file.
func (ss *SphereStore) Path() string {
	return ss.path
}

// Checkpoint forces a WAL checkpoint on the sphere database.
func (ss *SphereStore) Checkpoint() error {
	_, err := ss.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if err != nil {
		return fmt.Errorf("failed to checkpoint database %q: %w", ss.path, err)
	}
	return nil
}

// — Store shim utility methods (explicit definitions resolve promotion ambiguity) —

// Close closes the active database connection.
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
