package store

import (
	"database/sql"
	"fmt"
)

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

const worldSchemaV5 = `CREATE INDEX IF NOT EXISTS idx_mr_blocked_by ON merge_requests(blocked_by);`

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
	if v >= 5 {
		return nil // already at latest version
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	if v < 1 {
		if _, err := tx.Exec(worldSchemaV1); err != nil {
			return fmt.Errorf("failed to create world schema v1: %w", err)
		}
	}
	if v < 2 {
		if _, err := tx.Exec(worldSchemaV2); err != nil {
			return fmt.Errorf("failed to create world schema v2: %w", err)
		}
	}
	if v < 3 {
		exists, err := columnExists(tx, "merge_requests", "blocked_by")
		if err != nil {
			return fmt.Errorf("failed to check merge_requests schema: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(worldSchemaV3); err != nil {
				return fmt.Errorf("failed to apply world schema v3: %w", err)
			}
		}
	}
	if v < 4 {
		if _, err := tx.Exec(worldSchemaV4); err != nil {
			return fmt.Errorf("failed to apply world schema v4: %w", err)
		}
	}
	if v < 5 {
		if _, err := tx.Exec(worldSchemaV5); err != nil {
			return fmt.Errorf("failed to apply world schema v5: %w", err)
		}
	}
	if v < 1 {
		if _, err := tx.Exec("INSERT INTO schema_version VALUES (5)"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	} else {
		if _, err := tx.Exec("UPDATE schema_version SET version = 5"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	}
	return tx.Commit()
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

const sphereSchemaV5 = `
CREATE TABLE IF NOT EXISTS worlds (
    name        TEXT PRIMARY KEY,
    source_repo TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
`

const sphereSchemaV6 = `
CREATE INDEX IF NOT EXISTS idx_agents_world_state ON agents(world, state);
CREATE INDEX IF NOT EXISTS idx_escalations_status ON escalations(status);
CREATE INDEX IF NOT EXISTS idx_caravan_items_world ON caravan_items(world);
`

const sphereSchemaV7 = `ALTER TABLE caravan_items ADD COLUMN phase INTEGER NOT NULL DEFAULT 0;`

// columnExists checks whether a column exists on a table using PRAGMA table_info.
func columnExists(db interface {
	QueryRow(string, ...interface{}) *sql.Row
}, table, column string) (bool, error) {
	// PRAGMA table_info returns one row per column. We can't parameterize
	// PRAGMA arguments, but table/column names come from our own schema
	// constants, not user input.
	rows, err := db.(interface {
		Query(string, ...interface{}) (*sql.Rows, error)
	}).Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// tableExists checks whether a table exists in the database.
func tableExists(db interface {
	QueryRow(string, ...interface{}) *sql.Row
}, name string) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) migrateSphere() error {
	v, err := s.schemaVersion()
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v >= 7 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	if v < 1 {
		if _, err := tx.Exec(sphereSchemaV1); err != nil {
			return fmt.Errorf("failed to create sphere schema v1: %w", err)
		}
	}
	if v < 2 {
		if _, err := tx.Exec(sphereSchemaV2); err != nil {
			return fmt.Errorf("failed to create sphere schema v2: %w", err)
		}
	}
	if v < 3 {
		if _, err := tx.Exec(sphereSchemaV3); err != nil {
			return fmt.Errorf("failed to create sphere schema v3: %w", err)
		}
	}
	if v < 4 {
		// Rename agents.hook_item → tether_item (if not already renamed).
		exists, err := columnExists(tx, "agents", "hook_item")
		if err != nil {
			return fmt.Errorf("V4 migration: failed to check column agents.hook_item: %w", err)
		}
		if exists {
			if _, err := tx.Exec(`ALTER TABLE agents RENAME COLUMN hook_item TO tether_item`); err != nil {
				return fmt.Errorf("failed to rename agents.hook_item: %w", err)
			}
		}
		// Rename agents.rig → world (if not already renamed).
		exists, err = columnExists(tx, "agents", "rig")
		if err != nil {
			return fmt.Errorf("V4 migration: failed to check column agents.rig: %w", err)
		}
		if exists {
			if _, err := tx.Exec(`ALTER TABLE agents RENAME COLUMN rig TO world`); err != nil {
				return fmt.Errorf("failed to rename agents.rig: %w", err)
			}
		}
		// Rename convoys → caravans (if not already renamed).
		exists, err = tableExists(tx, "convoys")
		if err != nil {
			return fmt.Errorf("V4 migration: failed to check table convoys: %w", err)
		}
		if exists {
			if _, err := tx.Exec(`ALTER TABLE convoys RENAME TO caravans`); err != nil {
				return fmt.Errorf("failed to rename convoys: %w", err)
			}
		}
		// Rename convoy_items → caravan_items (if not already renamed).
		exists, err = tableExists(tx, "convoy_items")
		if err != nil {
			return fmt.Errorf("V4 migration: failed to check table convoy_items: %w", err)
		}
		if exists {
			if _, err := tx.Exec(`ALTER TABLE convoy_items RENAME TO caravan_items`); err != nil {
				return fmt.Errorf("failed to rename convoy_items: %w", err)
			}
		}
		// Rename caravan_items.convoy_id → caravan_id (if not already renamed).
		exists, err = columnExists(tx, "caravan_items", "convoy_id")
		if err != nil {
			return fmt.Errorf("V4 migration: failed to check column caravan_items.convoy_id: %w", err)
		}
		if exists {
			if _, err := tx.Exec(`ALTER TABLE caravan_items RENAME COLUMN convoy_id TO caravan_id`); err != nil {
				return fmt.Errorf("failed to rename caravan_items.convoy_id: %w", err)
			}
		}
		// Rename caravan_items.rig → world (if not already renamed).
		exists, err = columnExists(tx, "caravan_items", "rig")
		if err != nil {
			return fmt.Errorf("V4 migration: failed to check column caravan_items.rig: %w", err)
		}
		if exists {
			if _, err := tx.Exec(`ALTER TABLE caravan_items RENAME COLUMN rig TO world`); err != nil {
				return fmt.Errorf("failed to rename caravan_items.rig: %w", err)
			}
		}
	}
	if v < 5 {
		if _, err := tx.Exec(sphereSchemaV5); err != nil {
			return fmt.Errorf("failed to apply sphere schema v5: %w", err)
		}
	}
	if v < 6 {
		if _, err := tx.Exec(sphereSchemaV6); err != nil {
			return fmt.Errorf("failed to apply sphere schema v6: %w", err)
		}
	}
	if v < 7 {
		exists, err := columnExists(tx, "caravan_items", "phase")
		if err != nil {
			return fmt.Errorf("failed to check caravan_items.phase column: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(sphereSchemaV7); err != nil {
				return fmt.Errorf("failed to apply sphere schema v7: %w", err)
			}
		}
	}
	if v < 1 {
		if _, err := tx.Exec("INSERT INTO schema_version VALUES (7)"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	} else {
		if _, err := tx.Exec("UPDATE schema_version SET version = 7"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	}
	return tx.Commit()
}
