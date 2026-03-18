package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// Current schema versions — the latest migration target for each database type.
const (
	CurrentWorldSchema  = 10
	CurrentSphereSchema = 14
)

const worldSchemaV1 = `
CREATE TABLE IF NOT EXISTS writs (
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
CREATE INDEX IF NOT EXISTS idx_writ_status ON writs(status);
CREATE INDEX IF NOT EXISTS idx_writ_assignee ON writs(assignee);
CREATE INDEX IF NOT EXISTS idx_writ_priority ON writs(priority);

CREATE TABLE IF NOT EXISTS labels (
    writ_id TEXT NOT NULL REFERENCES writs(id),
    label   TEXT NOT NULL,
    PRIMARY KEY (writ_id, label)
);
CREATE INDEX IF NOT EXISTS idx_labels_label ON labels(label);

CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
`

const worldSchemaV2 = `
CREATE TABLE IF NOT EXISTS merge_requests (
    id           TEXT PRIMARY KEY,
    writ_id      TEXT NOT NULL REFERENCES writs(id),
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
CREATE INDEX IF NOT EXISTS idx_mr_writ ON merge_requests(writ_id);
`

const worldSchemaV3 = `
ALTER TABLE merge_requests ADD COLUMN blocked_by TEXT;
`

const worldSchemaV4 = `
CREATE TABLE IF NOT EXISTS dependencies (
    from_id TEXT NOT NULL REFERENCES writs(id),
    to_id   TEXT NOT NULL REFERENCES writs(id),
    PRIMARY KEY (from_id, to_id)
);
CREATE INDEX IF NOT EXISTS idx_deps_from ON dependencies(from_id);
CREATE INDEX IF NOT EXISTS idx_deps_to ON dependencies(to_id);
`

const worldSchemaV5 = `CREATE INDEX IF NOT EXISTS idx_mr_blocked_by ON merge_requests(blocked_by);`

const worldSchemaV6 = `
CREATE TABLE IF NOT EXISTS agent_history (
    id            TEXT PRIMARY KEY,
    agent_name    TEXT NOT NULL,
    writ_id       TEXT,
    action        TEXT NOT NULL,
    started_at    TEXT NOT NULL,
    ended_at      TEXT,
    summary       TEXT
);
CREATE INDEX IF NOT EXISTS idx_history_agent ON agent_history(agent_name);
CREATE INDEX IF NOT EXISTS idx_history_writ ON agent_history(writ_id);

CREATE TABLE IF NOT EXISTS token_usage (
    id                    TEXT PRIMARY KEY,
    history_id            TEXT NOT NULL REFERENCES agent_history(id),
    model                 TEXT NOT NULL,
    input_tokens          INTEGER NOT NULL DEFAULT 0,
    output_tokens         INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens     INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_token_history ON token_usage(history_id);
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

// SchemaVersion returns the current schema version stored in the database.
// Returns 0 for a fresh (empty) database.
func (s *baseStore) SchemaVersion() (int, error) {
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
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil // table exists but empty
		}
		return 0, err
	}
	return v, nil
}

const worldSchemaV7 = `
CREATE TABLE IF NOT EXISTS agent_memories (
    id         TEXT PRIMARY KEY,
    agent_name TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(agent_name, key)
);
CREATE INDEX IF NOT EXISTS idx_agent_memories_agent ON agent_memories(agent_name);
`

// worldSchemaV8 renames work_items → writs and work_item_id → writ_id
// across all tables that reference the old naming.
const worldSchemaV8 = "" // migration handled procedurally below

// worldSchemaV9 adds kind, metadata, and close_reason columns to writs.
const worldSchemaV9 = "" // migration handled procedurally below

// worldSchemaV10 renames created_by 'operator' → 'autarch' in writs.
const worldSchemaV10 = "" // migration handled procedurally below

func (s *WorldStore) migrateWorld() error {
	v, err := s.SchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v >= CurrentWorldSchema {
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
	if v < 6 {
		if _, err := tx.Exec(worldSchemaV6); err != nil {
			return fmt.Errorf("failed to apply world schema v6: %w", err)
		}
	}
	if v < 7 {
		if _, err := tx.Exec(worldSchemaV7); err != nil {
			return fmt.Errorf("failed to apply world schema v7: %w", err)
		}
	}
	if v < 8 {
		// Rename work_items → writs (only if old table still exists).
		oldExists, err := tableExists(tx, "work_items")
		if err != nil {
			return fmt.Errorf("V8 migration: failed to check table work_items: %w", err)
		}
		if oldExists {
			if _, err := tx.Exec(`ALTER TABLE work_items RENAME TO writs`); err != nil {
				return fmt.Errorf("failed to rename work_items to writs: %w", err)
			}
		}
		// Rename labels.work_item_id → writ_id.
		oldCol, err := columnExists(tx, "labels", "work_item_id")
		if err != nil {
			return fmt.Errorf("V8 migration: failed to check column labels.work_item_id: %w", err)
		}
		if oldCol {
			if _, err := tx.Exec(`ALTER TABLE labels RENAME COLUMN work_item_id TO writ_id`); err != nil {
				return fmt.Errorf("failed to rename labels.work_item_id: %w", err)
			}
		}
		// Rename merge_requests.work_item_id → writ_id.
		oldCol, err = columnExists(tx, "merge_requests", "work_item_id")
		if err != nil {
			return fmt.Errorf("V8 migration: failed to check column merge_requests.work_item_id: %w", err)
		}
		if oldCol {
			if _, err := tx.Exec(`ALTER TABLE merge_requests RENAME COLUMN work_item_id TO writ_id`); err != nil {
				return fmt.Errorf("failed to rename merge_requests.work_item_id: %w", err)
			}
		}
		// Rename agent_history.work_item_id → writ_id.
		oldCol, err = columnExists(tx, "agent_history", "work_item_id")
		if err != nil {
			return fmt.Errorf("V8 migration: failed to check column agent_history.work_item_id: %w", err)
		}
		if oldCol {
			if _, err := tx.Exec(`ALTER TABLE agent_history RENAME COLUMN work_item_id TO writ_id`); err != nil {
				return fmt.Errorf("failed to rename agent_history.work_item_id: %w", err)
			}
		}
	}
	if v < 9 {
		// Add kind column (NOT NULL DEFAULT 'code') — determines resolve path.
		exists, err := columnExists(tx, "writs", "kind")
		if err != nil {
			return fmt.Errorf("V9 migration: failed to check column writs.kind: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE writs ADD COLUMN kind TEXT NOT NULL DEFAULT 'code'`); err != nil {
				return fmt.Errorf("failed to add writs.kind column: %w", err)
			}
		}
		// Add metadata column (nullable JSON).
		exists, err = columnExists(tx, "writs", "metadata")
		if err != nil {
			return fmt.Errorf("V9 migration: failed to check column writs.metadata: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE writs ADD COLUMN metadata JSON`); err != nil {
				return fmt.Errorf("failed to add writs.metadata column: %w", err)
			}
		}
		// Add close_reason column (nullable).
		exists, err = columnExists(tx, "writs", "close_reason")
		if err != nil {
			return fmt.Errorf("V9 migration: failed to check column writs.close_reason: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE writs ADD COLUMN close_reason TEXT`); err != nil {
				return fmt.Errorf("failed to add writs.close_reason column: %w", err)
			}
		}
	}
	if v < 10 {
		// Rename identity: operator → autarch in writs.created_by.
		if _, err := tx.Exec(`UPDATE writs SET created_by = 'autarch' WHERE created_by = 'operator'`); err != nil {
			return fmt.Errorf("V10 migration: failed to rename operator → autarch in writs: %w", err)
		}
	}
	if _, err := tx.Exec("DELETE FROM schema_version"); err != nil {
		return fmt.Errorf("failed to clear schema version: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO schema_version VALUES (%d)", CurrentWorldSchema)); err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
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
    writ_id      TEXT NOT NULL,
    rig          TEXT NOT NULL,
    PRIMARY KEY (convoy_id, writ_id)
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

const sphereSchemaV8 = `
CREATE TABLE IF NOT EXISTS caravan_dependencies (
    from_id TEXT NOT NULL REFERENCES caravans(id),
    to_id   TEXT NOT NULL REFERENCES caravans(id),
    PRIMARY KEY (from_id, to_id)
);
CREATE INDEX IF NOT EXISTS idx_caravan_deps_from ON caravan_dependencies(from_id);
CREATE INDEX IF NOT EXISTS idx_caravan_deps_to ON caravan_dependencies(to_id);
`

const sphereSchemaV9 = "" // migration handled procedurally below — renames caravan_items.work_item_id → writ_id

const sphereSchemaV10 = "" // migration handled procedurally below — renames agents.tether_item → active_writ

const sphereSchemaV11 = `ALTER TABLE escalations ADD COLUMN source_ref TEXT;`

const sphereSchemaV12 = `
ALTER TABLE escalations ADD COLUMN last_notified_at TEXT;
CREATE INDEX IF NOT EXISTS idx_escalations_source_ref ON escalations(source_ref)
    WHERE source_ref IS NOT NULL;
`

// sphereSchemaV13 renames owner 'operator' → 'autarch' in caravans.
const sphereSchemaV13 = "" // migration handled procedurally below

// sphereSchemaV14 renames role 'agent' → 'outpost' in agents.
const sphereSchemaV14 = "" // migration handled procedurally below

// columnExists checks whether a column exists on a table using PRAGMA table_info.
func columnExists(db interface {
	Query(string, ...interface{}) (*sql.Rows, error)
}, table, column string) (bool, error) {
	// PRAGMA table_info returns one row per column. We can't parameterize
	// PRAGMA arguments, but table/column names come from our own schema
	// constants, not user input.
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%q)", table))
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

func (s *SphereStore) migrateSphere() error {
	v, err := s.SchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v >= CurrentSphereSchema {
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
	if v < 8 {
		if _, err := tx.Exec(sphereSchemaV8); err != nil {
			return fmt.Errorf("failed to apply sphere schema v8: %w", err)
		}
	}
	if v < 9 {
		// Rename caravan_items.work_item_id → writ_id.
		oldCol, err := columnExists(tx, "caravan_items", "work_item_id")
		if err != nil {
			return fmt.Errorf("V9 migration: failed to check column caravan_items.work_item_id: %w", err)
		}
		if oldCol {
			if _, err := tx.Exec(`ALTER TABLE caravan_items RENAME COLUMN work_item_id TO writ_id`); err != nil {
				return fmt.Errorf("failed to rename caravan_items.work_item_id: %w", err)
			}
		}
	}
	if v < 10 {
		// Rename agents.tether_item → active_writ.
		oldCol, err := columnExists(tx, "agents", "tether_item")
		if err != nil {
			return fmt.Errorf("V10 migration: failed to check column agents.tether_item: %w", err)
		}
		if oldCol {
			if _, err := tx.Exec(`ALTER TABLE agents RENAME COLUMN tether_item TO active_writ`); err != nil {
				return fmt.Errorf("failed to rename agents.tether_item: %w", err)
			}
		}
	}
	if v < 11 {
		exists, err := columnExists(tx, "escalations", "source_ref")
		if err != nil {
			return fmt.Errorf("V11 migration: failed to check column escalations.source_ref: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(sphereSchemaV11); err != nil {
				return fmt.Errorf("failed to apply sphere schema v11: %w", err)
			}
		}
	}
	if v < 12 {
		exists, err := columnExists(tx, "escalations", "last_notified_at")
		if err != nil {
			return fmt.Errorf("V12 migration: failed to check column escalations.last_notified_at: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(sphereSchemaV12); err != nil {
				return fmt.Errorf("failed to apply sphere schema v12: %w", err)
			}
		}
	}
	if v < 13 {
		// Rename identity: operator → autarch in caravans.owner.
		// Guard: caravans table may not exist in minimal test databases
		// that start at a pre-V3 schema.
		caravansExist, err := tableExists(tx, "caravans")
		if err != nil {
			return fmt.Errorf("V13 migration: failed to check table caravans: %w", err)
		}
		if caravansExist {
			if _, err := tx.Exec(`UPDATE caravans SET owner = 'autarch' WHERE owner = 'operator'`); err != nil {
				return fmt.Errorf("V13 migration: failed to rename operator → autarch in caravans: %w", err)
			}
		}
	}
	if v < 14 {
		// Rename role: 'agent' → 'outpost' in agents.
		// Completes the outpost role rename — new code writes "outpost"
		// but existing records may still have "agent".
		if _, err := tx.Exec(`UPDATE agents SET role = 'outpost' WHERE role = 'agent'`); err != nil {
			return fmt.Errorf("V14 migration: failed to rename agent role to outpost: %w", err)
		}
	}
	if _, err := tx.Exec("DELETE FROM schema_version"); err != nil {
		return fmt.Errorf("failed to clear schema version: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO schema_version VALUES (%d)", CurrentSphereSchema)); err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
	}
	return tx.Commit()
}
