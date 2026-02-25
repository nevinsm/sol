package store

import "fmt"

const rigSchemaV1 = `
CREATE TABLE IF NOT EXISTS work_items (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'open',
    priority    INTEGER NOT NULL DEFAULT 2,
    assignee    TEXT,
    parent_id   TEXT,
    created_by  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    closed_at   TEXT
);
CREATE INDEX IF NOT EXISTS idx_work_status ON work_items(status);
CREATE INDEX IF NOT EXISTS idx_work_assignee ON work_items(assignee);
CREATE INDEX IF NOT EXISTS idx_work_priority ON work_items(priority);

CREATE TABLE IF NOT EXISTS labels (
    work_item_id TEXT NOT NULL REFERENCES work_items(id),
    label        TEXT NOT NULL,
    PRIMARY KEY (work_item_id, label)
);
CREATE INDEX IF NOT EXISTS idx_labels_label ON labels(label);

CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
`

const townSchemaV1 = `
CREATE TABLE IF NOT EXISTS agents (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    rig         TEXT NOT NULL,
    role        TEXT NOT NULL,
    state       TEXT NOT NULL DEFAULT 'idle',
    hook_item   TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
`

func (s *Store) schemaVersion() (int, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='schema_version'`).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	var v int
	err = s.db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&v)
	if err != nil {
		return 0, nil // table exists but empty
	}
	return v, nil
}

func (s *Store) migrateRig() error {
	v, err := s.schemaVersion()
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v >= 1 {
		return nil
	}
	if _, err := s.db.Exec(rigSchemaV1); err != nil {
		return fmt.Errorf("failed to create rig schema: %w", err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version VALUES (1)"); err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
	}
	return nil
}

func (s *Store) migrateTown() error {
	v, err := s.schemaVersion()
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v >= 1 {
		return nil
	}
	if _, err := s.db.Exec(townSchemaV1); err != nil {
		return fmt.Errorf("failed to create town schema: %w", err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version VALUES (1)"); err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
	}
	return nil
}
