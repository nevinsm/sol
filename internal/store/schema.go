package store

import "fmt"

const worldSchemaV1 = `
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

const worldSchemaV2 = `
CREATE TABLE IF NOT EXISTS merge_requests (
    id           TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items(id),
    branch       TEXT NOT NULL,
    phase        TEXT NOT NULL DEFAULT 'ready',
    claimed_by   TEXT,
    claimed_at   TEXT,
    attempts     INTEGER NOT NULL DEFAULT 0,
    priority     INTEGER NOT NULL DEFAULT 2,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    merged_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_mr_phase ON merge_requests(phase);
CREATE INDEX IF NOT EXISTS idx_mr_work_item ON merge_requests(work_item_id);
`

const worldSchemaV3 = `
ALTER TABLE merge_requests ADD COLUMN blocked_by TEXT;
`

const worldSchemaV4 = `
CREATE TABLE IF NOT EXISTS dependencies (
    from_id TEXT NOT NULL REFERENCES work_items(id),
    to_id   TEXT NOT NULL REFERENCES work_items(id),
    PRIMARY KEY (from_id, to_id)
);
CREATE INDEX IF NOT EXISTS idx_deps_from ON dependencies(from_id);
CREATE INDEX IF NOT EXISTS idx_deps_to ON dependencies(to_id);
`

const sphereSchemaV1 = `
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

const sphereSchemaV2 = `
CREATE TABLE IF NOT EXISTS messages (
    id          TEXT PRIMARY KEY,
    sender      TEXT NOT NULL,
    recipient   TEXT NOT NULL,
    subject     TEXT NOT NULL,
    body        TEXT,
    priority    INTEGER NOT NULL DEFAULT 2,
    type        TEXT NOT NULL DEFAULT 'notification',
    thread_id   TEXT,
    delivery    TEXT NOT NULL DEFAULT 'pending',
    read        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    acked_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_messages_recipient ON messages(recipient, delivery);
CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(thread_id);

CREATE TABLE IF NOT EXISTS escalations (
    id           TEXT PRIMARY KEY,
    severity     TEXT NOT NULL,
    source       TEXT NOT NULL,
    description  TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open',
    acknowledged INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
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

func (s *Store) migrateWorld() error {
	v, err := s.schemaVersion()
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v < 1 {
		if _, err := s.db.Exec(worldSchemaV1); err != nil {
			return fmt.Errorf("failed to create world schema v1: %w", err)
		}
	}
	if v < 2 {
		if _, err := s.db.Exec(worldSchemaV2); err != nil {
			return fmt.Errorf("failed to create world schema v2: %w", err)
		}
	}
	if v < 3 {
		if _, err := s.db.Exec(worldSchemaV3); err != nil {
			return fmt.Errorf("failed to apply world schema v3: %w", err)
		}
	}
	if v < 4 {
		if _, err := s.db.Exec(worldSchemaV4); err != nil {
			return fmt.Errorf("failed to apply world schema v4: %w", err)
		}
	}
	if v < 1 {
		if _, err := s.db.Exec("INSERT INTO schema_version VALUES (4)"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	} else if v < 4 {
		if _, err := s.db.Exec("UPDATE schema_version SET version = 4"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	}
	return nil
}

const sphereSchemaV3 = `
CREATE TABLE IF NOT EXISTS convoys (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'open',
    owner      TEXT,
    created_at TEXT NOT NULL,
    closed_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_convoys_status ON convoys(status);

CREATE TABLE IF NOT EXISTS convoy_items (
    convoy_id    TEXT NOT NULL REFERENCES convoys(id),
    work_item_id TEXT NOT NULL,
    rig          TEXT NOT NULL,
    PRIMARY KEY (convoy_id, work_item_id)
);
CREATE INDEX IF NOT EXISTS idx_convoy_items_convoy ON convoy_items(convoy_id);
`

const sphereSchemaV4 = `
ALTER TABLE agents RENAME COLUMN hook_item TO tether_item;
ALTER TABLE agents RENAME COLUMN rig TO world;
ALTER TABLE convoys RENAME TO caravans;
ALTER TABLE convoy_items RENAME TO caravan_items;
ALTER TABLE caravan_items RENAME COLUMN convoy_id TO caravan_id;
ALTER TABLE caravan_items RENAME COLUMN rig TO world;
`

const sphereSchemaV5 = `
CREATE TABLE IF NOT EXISTS worlds (
    name        TEXT PRIMARY KEY,
    source_repo TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
`

func (s *Store) migrateSphere() error {
	v, err := s.schemaVersion()
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v < 1 {
		if _, err := s.db.Exec(sphereSchemaV1); err != nil {
			return fmt.Errorf("failed to create sphere schema v1: %w", err)
		}
	}
	if v < 2 {
		if _, err := s.db.Exec(sphereSchemaV2); err != nil {
			return fmt.Errorf("failed to create sphere schema v2: %w", err)
		}
	}
	if v < 3 {
		if _, err := s.db.Exec(sphereSchemaV3); err != nil {
			return fmt.Errorf("failed to create sphere schema v3: %w", err)
		}
	}
	if v < 4 {
		if _, err := s.db.Exec(sphereSchemaV4); err != nil {
			return fmt.Errorf("failed to apply sphere schema v4: %w", err)
		}
	}
	if v < 5 {
		if _, err := s.db.Exec(sphereSchemaV5); err != nil {
			return fmt.Errorf("failed to apply sphere schema v5: %w", err)
		}
	}
	if v < 1 {
		if _, err := s.db.Exec("INSERT INTO schema_version VALUES (5)"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	} else if v < 5 {
		if _, err := s.db.Exec("UPDATE schema_version SET version = 5"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	}
	return nil
}
