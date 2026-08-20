package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// Current schema versions — the latest migration target for each database type.
const (
	CurrentWorldSchema  = 19
	CurrentSphereSchema = 20
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
	return schemaVersion(s.db)
}

// schemaVersion reads the current schema version using the supplied querier
// (typically *sql.DB or *sql.Tx). Returns 0 for a fresh (empty) database.
func schemaVersion(q interface {
	QueryRow(string, ...interface{}) *sql.Row
}) (int, error) {
	var exists bool
	err := q.QueryRow(`SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='schema_version'`).Scan(&exists)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}
	var v int
	err = q.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&v)
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

// worldSchemaV11 adds cost_usd and duration_ms columns to token_usage.
const worldSchemaV11 = "" // migration handled procedurally below

// worldSchemaV12 adds runtime column to token_usage.
const worldSchemaV12 = "" // migration handled procedurally below

// worldSchemaV13 drops the agent_memories table (the legacy brief system that
// replaced it has since been retired in favor of Claude Code auto-memory).
const worldSchemaV13 = "" // migration handled procedurally below

// worldSchemaV14 adds account column to token_usage for budget attribution.
const worldSchemaV14 = "" // migration handled procedurally below

// worldSchemaV15 adds resolution_count column to merge_requests for bounding
// conflict resolution task cascades.
const worldSchemaV15 = "" // migration handled procedurally below

// worldSchemaV16 adds reasoning_tokens column to token_usage.
const worldSchemaV16 = "" // migration handled procedurally below

// worldSchemaV17 adds attempt_history column to merge_requests for storing
// per-attempt failure summaries as a JSON array.
const worldSchemaV17 = "" // migration handled procedurally below

// worldSchemaV18 adds failed_at column to merge_requests for recording the
// point-in-time when an MR first transitioned to the failed phase. This is
// distinct from updated_at, which is subsequently modified by sentinel patrol
// and recast operations, making it an unreliable failure timestamp.
const worldSchemaV18 = "" // migration handled procedurally below

// worldSchemaV19 adds notify_on_close column to writs — creator opt-in
// completion/failure mail, the writ-level sibling of the caravan notify
// feature (sol-4c09fa997137dc02, a different table and hook point). Set at
// `sol writ create --notify` time and immutable thereafter. Consumed by
// internal/forge/toolbox.go's two terminal-outcome hooks
// (MarkMerged/MarkMergedNoOp, MarkFailed) — see writ sol-9220d19c5623b74b.
const worldSchemaV19 = `ALTER TABLE writs ADD COLUMN notify_on_close INTEGER NOT NULL DEFAULT 0;`

func (s *WorldStore) migrateWorld() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	// Read the schema version inside the transaction so it reflects a
	// consistent snapshot for the duration of the migration.
	v, err := schemaVersion(tx)
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v >= CurrentWorldSchema {
		return nil // already at latest version
	}

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
	if v < 11 {
		// Add cost_usd and duration_ms columns to token_usage.
		exists, err := columnExists(tx, "token_usage", "cost_usd")
		if err != nil {
			return fmt.Errorf("V11 migration: failed to check column token_usage.cost_usd: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE token_usage ADD COLUMN cost_usd REAL`); err != nil {
				return fmt.Errorf("failed to add token_usage.cost_usd column: %w", err)
			}
		}
		exists, err = columnExists(tx, "token_usage", "duration_ms")
		if err != nil {
			return fmt.Errorf("V11 migration: failed to check column token_usage.duration_ms: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE token_usage ADD COLUMN duration_ms INTEGER`); err != nil {
				return fmt.Errorf("failed to add token_usage.duration_ms column: %w", err)
			}
		}
	}
	if v < 12 {
		// Add runtime column to token_usage (nullable TEXT).
		exists, err := columnExists(tx, "token_usage", "runtime")
		if err != nil {
			return fmt.Errorf("V12 migration: failed to check column token_usage.runtime: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE token_usage ADD COLUMN runtime TEXT`); err != nil {
				return fmt.Errorf("failed to add token_usage.runtime column: %w", err)
			}
		}
	}
	if v < 13 {
		// Drop agent_memories table (the legacy brief system that replaced it
		// has since been retired in favor of Claude Code auto-memory).
		if _, err := tx.Exec(`DROP TABLE IF EXISTS agent_memories`); err != nil {
			return fmt.Errorf("V13 migration: failed to drop agent_memories table: %w", err)
		}
	}
	if v < 14 {
		// Add account column to token_usage for budget attribution.
		exists, err := columnExists(tx, "token_usage", "account")
		if err != nil {
			return fmt.Errorf("V14 migration: failed to check column token_usage.account: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE token_usage ADD COLUMN account TEXT`); err != nil {
				return fmt.Errorf("failed to add token_usage.account column: %w", err)
			}
		}
	}
	if v < 15 {
		// Add resolution_count column to merge_requests for bounding conflict
		// resolution task cascades.
		exists, err := columnExists(tx, "merge_requests", "resolution_count")
		if err != nil {
			return fmt.Errorf("V15 migration: failed to check column merge_requests.resolution_count: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE merge_requests ADD COLUMN resolution_count INTEGER NOT NULL DEFAULT 0`); err != nil {
				return fmt.Errorf("failed to add merge_requests.resolution_count column: %w", err)
			}
		}
	}
	if v < 16 {
		// Add reasoning_tokens column to token_usage.
		exists, err := columnExists(tx, "token_usage", "reasoning_tokens")
		if err != nil {
			return fmt.Errorf("V16 migration: failed to check column token_usage.reasoning_tokens: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE token_usage ADD COLUMN reasoning_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
				return fmt.Errorf("failed to add token_usage.reasoning_tokens column: %w", err)
			}
		}
	}
	if v < 17 {
		// Add attempt_history column to merge_requests for storing per-attempt
		// failure summaries as a JSON array of strings.
		exists, err := columnExists(tx, "merge_requests", "attempt_history")
		if err != nil {
			return fmt.Errorf("V17 migration: failed to check column merge_requests.attempt_history: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE merge_requests ADD COLUMN attempt_history TEXT DEFAULT ''`); err != nil {
				return fmt.Errorf("failed to add merge_requests.attempt_history column: %w", err)
			}
		}
	}
	if v < 18 {
		// Add failed_at column to merge_requests so the trace viewer can show
		// the point-in-time failure timestamp rather than the mutable updated_at.
		// COALESCE(failed_at, ?) in UpdateMergeRequestPhase and ReleaseStaleClaims
		// ensures existing rows keep NULL (unknown first-failure time) until they
		// next transition to failed, at which point the column is set once and frozen.
		exists, err := columnExists(tx, "merge_requests", "failed_at")
		if err != nil {
			return fmt.Errorf("V18 migration: failed to check column merge_requests.failed_at: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(`ALTER TABLE merge_requests ADD COLUMN failed_at TEXT`); err != nil {
				return fmt.Errorf("failed to add merge_requests.failed_at column: %w", err)
			}
		}
	}
	if v < 19 {
		// Add notify_on_close column so `sol writ create --notify` writs can
		// be gated for creator completion/failure mail (forge toolbox hooks).
		exists, err := columnExists(tx, "writs", "notify_on_close")
		if err != nil {
			return fmt.Errorf("V19 migration: failed to check column writs.notify_on_close: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(worldSchemaV19); err != nil {
				return fmt.Errorf("failed to add writs.notify_on_close column: %w", err)
			}
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

// sphereSchemaV15 adds the migrations_applied table that tracks which
// internal/migrate migrations have been applied to this sphere. See ADR-
// notes in internal/migrate/migrate.go.
const sphereSchemaV15 = `
CREATE TABLE IF NOT EXISTS migrations_applied (
    name        TEXT PRIMARY KEY,
    version     TEXT NOT NULL,
    applied_at  TEXT NOT NULL,
    summary     TEXT NOT NULL,
    details     TEXT NOT NULL
);
`

// sphereSchemaV16 adds a partial UNIQUE index on (thread_id) for pending
// messages with non-empty thread_ids. This makes thread-based dedup
// (used by escalation notifications) an atomic database constraint rather
// than relying on an in-process check that races under multi-process
// deployments (defensive — multi-consul deployments would otherwise
// produce duplicate notifications).
//
// Before creating the index we delete any pre-existing duplicate pending
// messages with the same non-empty thread_id, keeping the row with the
// smallest rowid (first inserted) per thread_id. Today only one consul
// instance exists so duplicates should not exist in practice — the
// dedupe pass exists so the migration cannot fail on imported /
// corrupted databases.
//
// SUPERSEDED by sphereSchemaV19: idx_messages_pending_thread_unique bound
// every threaded pending message, not just escalation notifications, which
// meant any second pending message in a thread (a reply before the first
// message was acked) hit the UNIQUE constraint — threaded conversations
// were structurally unable to hold more than one pending message. V19
// drops this index and replaces it with a dedicated dedup_key column
// scoped to the escalation-notification path only. This constant is kept
// verbatim (forward-only schema history — see docs/conventions/state-
// mutation.md) so it still applies correctly to databases migrating up
// from below v16.
const sphereSchemaV16 = `
DELETE FROM messages
WHERE rowid NOT IN (
    SELECT MIN(rowid) FROM messages
    WHERE delivery = 'pending' AND thread_id IS NOT NULL AND thread_id != ''
    GROUP BY thread_id
)
AND delivery = 'pending'
AND thread_id IS NOT NULL
AND thread_id != '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_pending_thread_unique
    ON messages(thread_id)
    WHERE delivery = 'pending' AND thread_id != '';
`

// sphereSchemaV17 adds a via TEXT column to messages, recording the SOL_VIA
// origin channel (ADR-0043 decision 1) for messages sent by external
// automation. NOT NULL DEFAULT '' backfills existing rows and matches the
// convention of the other messages columns (delivery, type) so callers can
// scan directly into a string without a sql.NullString detour.
const sphereSchemaV17 = `ALTER TABLE messages ADD COLUMN via TEXT NOT NULL DEFAULT '';`

// sphereSchemaV18 adds an archived_at TEXT column to messages, nullable
// (unset = not archived), stamped/cleared on every message in a thread by
// ArchiveThread/UnarchiveThread. Nullable (not NOT NULL DEFAULT '' like via)
// because "archived" is a tri-state-shaped concept expressed as a timestamp
// — matches the existing acked_at column's convention, not via's flag-like
// one. The index supports the "exclude archived by default" filter Inbox
// and CountPending apply on every call.
const sphereSchemaV18 = `
ALTER TABLE messages ADD COLUMN archived_at TEXT;
CREATE INDEX IF NOT EXISTS idx_messages_archived ON messages(archived_at);
`

// sphereSchemaV19 rescopes pending-message dedup off thread_id and onto a
// dedicated dedup_key column.
//
// idx_messages_pending_thread_unique (v16) was meant to dedup escalation
// notifications — its only intentional consumer is
// SendMessageWithThreadIfAbsent — but it bound thread_id for every pending
// message regardless of sender, so any second pending message in a thread
// (a reply before the first was acked) hit the UNIQUE constraint. Since
// delivery only leaves 'pending' on ack/dismiss, this made threaded
// conversations structurally unable to hold more than one outstanding
// message.
//
// dedup_key is nullable and left NULL by every send path except
// SendMessageWithThreadIfAbsent, which sets it to the threadID. The new
// partial index only constrains rows with a non-NULL dedup_key, so
// ordinary threaded conversation messages (dedup_key IS NULL) coexist
// freely while pending, and escalation-notification dedup keeps its
// atomic-constraint guarantee.
//
// Backfill: existing pending rows (written before this column existed) are
// left with dedup_key NULL rather than heuristically backfilled — a
// pending row's thread_id alone can't distinguish an escalation
// notification from conversation mail. Consequence: immediately after this
// migration, an escalation notification thread that already had a pending
// row from before the migration could receive one additional duplicate
// notification (the old row has dedup_key NULL, so a fresh
// SendMessageWithThreadIfAbsent call is no longer deduped against it).
// This requires a race between two notifiers for the same escalation,
// which cannot happen today — only one consul instance runs at a time.
const sphereSchemaV19 = `
ALTER TABLE messages ADD COLUMN dedup_key TEXT;
DROP INDEX IF EXISTS idx_messages_pending_thread_unique;
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_pending_dedup_unique
    ON messages(dedup_key)
    WHERE delivery = 'pending' AND dedup_key IS NOT NULL;
`

// sphereSchemaV20 adds a notify_on_close INTEGER column to caravans,
// defaulting to 0 (disabled). NOT NULL DEFAULT 0 backfills existing rows so
// pre-existing caravans keep today's behavior (no completion mail) unless
// explicitly opted in via `sol caravan create --notify`. When set,
// TryCloseCaravan mails the caravan's owner on auto-close (opt-in
// completion mail, decided with the autarch 2026-08-20).
const sphereSchemaV20 = `ALTER TABLE caravans ADD COLUMN notify_on_close INTEGER NOT NULL DEFAULT 0;`

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
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	// Read the schema version inside the transaction so it reflects a
	// consistent snapshot for the duration of the migration.
	v, err := schemaVersion(tx)
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v >= CurrentSphereSchema {
		return nil
	}

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
	if v < 15 {
		if _, err := tx.Exec(sphereSchemaV15); err != nil {
			return fmt.Errorf("failed to apply sphere schema v15: %w", err)
		}
	}
	if v < 16 {
		// Guard: messages table is created in V2; only run the V16 dedupe
		// + index step if it exists. This protects pre-V2 minimal test
		// databases (and the V1-only OpenNoMigrate fixture).
		messagesExist, err := tableExists(tx, "messages")
		if err != nil {
			return fmt.Errorf("V16 migration: failed to check table messages: %w", err)
		}
		if messagesExist {
			if _, err := tx.Exec(sphereSchemaV16); err != nil {
				return fmt.Errorf("failed to apply sphere schema v16: %w", err)
			}
		}
	}
	if v < 17 {
		// Guard: messages table may not exist in minimal test databases
		// (see V16 above for the same reasoning), and the column may
		// already be present if this migration was interrupted after the
		// ALTER TABLE but before schema_version was updated.
		messagesExist, err := tableExists(tx, "messages")
		if err != nil {
			return fmt.Errorf("V17 migration: failed to check table messages: %w", err)
		}
		if messagesExist {
			exists, err := columnExists(tx, "messages", "via")
			if err != nil {
				return fmt.Errorf("V17 migration: failed to check column messages.via: %w", err)
			}
			if !exists {
				if _, err := tx.Exec(sphereSchemaV17); err != nil {
					return fmt.Errorf("failed to apply sphere schema v17: %w", err)
				}
			}
		}
	}
	if v < 18 {
		// Guard: same reasoning as V16/V17 above — messages table may not
		// exist in minimal test databases, and the column may already be
		// present if this migration was interrupted after the ALTER TABLE
		// but before schema_version was updated.
		messagesExist, err := tableExists(tx, "messages")
		if err != nil {
			return fmt.Errorf("V18 migration: failed to check table messages: %w", err)
		}
		if messagesExist {
			exists, err := columnExists(tx, "messages", "archived_at")
			if err != nil {
				return fmt.Errorf("V18 migration: failed to check column messages.archived_at: %w", err)
			}
			if !exists {
				if _, err := tx.Exec(sphereSchemaV18); err != nil {
					return fmt.Errorf("failed to apply sphere schema v18: %w", err)
				}
			}
		}
	}
	if v < 19 {
		// Guard: same reasoning as V16/V17/V18 above — messages table may
		// not exist in minimal test databases, and the column may already
		// be present if this migration was interrupted after the ALTER
		// TABLE but before schema_version was updated.
		messagesExist, err := tableExists(tx, "messages")
		if err != nil {
			return fmt.Errorf("V19 migration: failed to check table messages: %w", err)
		}
		if messagesExist {
			exists, err := columnExists(tx, "messages", "dedup_key")
			if err != nil {
				return fmt.Errorf("V19 migration: failed to check column messages.dedup_key: %w", err)
			}
			if !exists {
				if _, err := tx.Exec(sphereSchemaV19); err != nil {
					return fmt.Errorf("failed to apply sphere schema v19: %w", err)
				}
			}
		}
	}
	if v < 20 {
		// Guard: caravans table may not exist in minimal test databases
		// (same reasoning as V16-V19 above), and the column may already be
		// present if this migration was interrupted after the ALTER TABLE
		// but before schema_version was updated.
		caravansExist, err := tableExists(tx, "caravans")
		if err != nil {
			return fmt.Errorf("V20 migration: failed to check table caravans: %w", err)
		}
		if caravansExist {
			exists, err := columnExists(tx, "caravans", "notify_on_close")
			if err != nil {
				return fmt.Errorf("V20 migration: failed to check column caravans.notify_on_close: %w", err)
			}
			if !exists {
				if _, err := tx.Exec(sphereSchemaV20); err != nil {
					return fmt.Errorf("failed to apply sphere schema v20: %w", err)
				}
			}
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
