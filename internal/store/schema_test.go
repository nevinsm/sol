package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaVersionFreshDatabase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fresh.db")

	s, err := OpenNoMigrate(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Fatalf("expected version 0 for fresh database, got %d", v)
	}
}

func TestSchemaVersionAfterWorldMigration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	s := openWorldAt(t, filepath.Join(dir, "versiontest.db"))

	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != CurrentWorldSchema {
		t.Fatalf("expected version %d, got %d", CurrentWorldSchema, v)
	}
}

func TestSchemaVersionAfterSphereMigration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	s := openSphereAt(t, filepath.Join(dir, "sphere.db"))

	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != CurrentSphereSchema {
		t.Fatalf("expected version %d, got %d", CurrentSphereSchema, v)
	}
}

func TestOpenNoMigrateDoesNotMigrate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nomigrate.db")

	// Create a V1-only database manually.
	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(worldSchemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Open without migration.
	s2, err := OpenNoMigrate(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	v, err := s2.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("expected version 1 (no migration), got %d", v)
	}

	// Verify merge_requests table does NOT exist (V2 not applied).
	exists, err := tableExists(s2.db, "merge_requests")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected merge_requests table to not exist (no migration)")
	}
}

func TestBackupDatabase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a world database with some data.
	dbPath := filepath.Join(dir, "backuptest.db")
	s := openWorldAt(t, dbPath)
	_, err := s.CreateWrit("backup item", "test", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	backupPath, err := BackupDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Verify backup file exists.
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("backup file does not exist: %s", backupPath)
	}

	// Verify backup path format.
	if !strings.HasPrefix(backupPath, dbPath+".backup.") {
		t.Fatalf("unexpected backup path: %s", backupPath)
	}

	// Verify backup is a valid database with the same data.
	backupStore, err := OpenNoMigrate(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer backupStore.Close()

	v, err := backupStore.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != CurrentWorldSchema {
		t.Fatalf("expected backup version %d, got %d", CurrentWorldSchema, v)
	}
}

func TestBackupDatabaseNonexistent(t *testing.T) {
	t.Parallel()
	_, err := BackupDatabase("/nonexistent/path.db")
	if err == nil {
		t.Fatal("expected error for nonexistent database")
	}
}

// TestBackupDatabaseCapturesWALData verifies that BackupDatabase checkpoints the
// WAL before copying, so committed transactions are present in the backup even
// when an active store connection is open (preventing automatic checkpoint on close).
func TestBackupDatabaseCapturesWALData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Open a world store and write data. Keep the connection open to simulate
	// an active sol process that might prevent auto-checkpoint on close.
	dbPath := filepath.Join(dir, "walcapturetest.db")
	s := openWorldAt(t, dbPath)
	writID, err := s.CreateWrit("WAL data item", "code", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the connection open — do not close before backup.
	backupPath, err := BackupDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Open the backup and verify the committed writ is present.
	backupStore, err := OpenNoMigrate(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer backupStore.Close()

	w, err := backupStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("writ not found in backup (WAL data missing): %v", err)
	}
	if w.Title != "WAL data item" {
		t.Errorf("backup writ title = %q, want %q", w.Title, "WAL data item")
	}
}

func TestCurrentSchemaConstants(t *testing.T) {
	t.Parallel()
	// Verify constants are positive and match the expected values.
	if CurrentWorldSchema != 19 {
		t.Fatalf("CurrentWorldSchema = %d, expected 19", CurrentWorldSchema)
	}
	if CurrentSphereSchema != 20 {
		t.Fatalf("CurrentSphereSchema = %d, expected 20", CurrentSphereSchema)
	}
}

func TestWorldSchemaV9Migration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create a V8 database by hand: apply V1 schema and insert some data.
	dbPath := filepath.Join(dir, "v9test.db")
	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Create the V1 schema (writs table).
	if _, err := s.db.Exec(worldSchemaV1); err != nil {
		t.Fatalf("create V1 schema: %v", err)
	}
	// Apply V2-V7 schemas.
	if _, err := s.db.Exec(worldSchemaV2); err != nil {
		t.Fatalf("create V2 schema: %v", err)
	}
	if _, err := s.db.Exec(worldSchemaV4); err != nil {
		t.Fatalf("create V4 schema: %v", err)
	}
	if _, err := s.db.Exec(worldSchemaV6); err != nil {
		t.Fatalf("create V6 schema: %v", err)
	}
	if _, err := s.db.Exec(worldSchemaV7); err != nil {
		t.Fatalf("create V7 schema: %v", err)
	}
	// Set version to 8.
	if _, err := s.db.Exec("INSERT INTO schema_version VALUES (8)"); err != nil {
		t.Fatalf("set schema version: %v", err)
	}
	// Seed data: writs, labels, deps, history.
	now := "2025-01-15T10:00:00Z"
	if _, err := s.db.Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES ('sol-aaaa0001', 'Test writ 1', 'desc one', 'open', 2, 'operator', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("insert writ 1: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO writs (id, title, description, status, priority, assignee, created_by, created_at, updated_at, closed_at)
		 VALUES ('sol-aaaa0002', 'Test writ 2', 'desc two', 'closed', 1, 'Nova', 'operator', ?, ?, ?)`,
		now, now, now,
	); err != nil {
		t.Fatalf("insert writ 2: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO labels (writ_id, label) VALUES ('sol-aaaa0001', 'bug')`); err != nil {
		t.Fatalf("insert label: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO dependencies (from_id, to_id) VALUES ('sol-aaaa0001', 'sol-aaaa0002')`); err != nil {
		t.Fatalf("insert dependency: %v", err)
	}
	s.Close()

	// Re-open via openWorldAt — should migrate to the current schema (V9 adds
	// kind/metadata/close_reason, V10 renames operator → autarch, V11 adds
	// cost_usd/duration_ms, ..., V19 adds notify_on_close).
	s2 := openWorldAt(t, dbPath)

	// Check schema version.
	v, err := s2.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != CurrentWorldSchema {
		t.Fatalf("expected schema version %d, got %d", CurrentWorldSchema, v)
	}

	// Verify existing writs got default values for new columns.
	w1, err := s2.GetWrit("sol-aaaa0001")
	if err != nil {
		t.Fatalf("GetWrit(sol-aaaa0001): %v", err)
	}
	if w1.Kind != "code" {
		t.Errorf("writ 1 kind = %q, want %q", w1.Kind, "code")
	}
	if w1.Metadata != nil {
		t.Errorf("writ 1 metadata = %v, want nil", w1.Metadata)
	}
	if w1.CloseReason != "" {
		t.Errorf("writ 1 close_reason = %q, want empty", w1.CloseReason)
	}
	if len(w1.Labels) != 1 || w1.Labels[0] != "bug" {
		t.Errorf("writ 1 labels = %v, want [bug]", w1.Labels)
	}

	w2, err := s2.GetWrit("sol-aaaa0002")
	if err != nil {
		t.Fatalf("GetWrit(sol-aaaa0002): %v", err)
	}
	if w2.Kind != "code" {
		t.Errorf("writ 2 kind = %q, want %q", w2.Kind, "code")
	}
	if w2.Metadata != nil {
		t.Errorf("writ 2 metadata = %v, want nil", w2.Metadata)
	}
	if w2.CloseReason != "" {
		t.Errorf("writ 2 close_reason = %q, want empty", w2.CloseReason)
	}
	if w2.Status != "closed" {
		t.Errorf("writ 2 status = %q, want %q", w2.Status, "closed")
	}

	// Verify V10 migration renamed created_by from 'operator' to 'autarch'.
	if w1.CreatedBy != "autarch" {
		t.Errorf("writ 1 created_by = %q, want %q (V10 migration)", w1.CreatedBy, "autarch")
	}
	if w2.CreatedBy != "autarch" {
		t.Errorf("writ 2 created_by = %q, want %q (V10 migration)", w2.CreatedBy, "autarch")
	}

	// Verify dependencies survived migration.
	// Writ 1 depends on writ 2 which is closed → writ 1 should be ready.
	ready, err := s2.IsReady("sol-aaaa0001")
	if err != nil {
		t.Fatalf("IsReady: %v", err)
	}
	if !ready {
		t.Error("writ 1 should be ready (depends on closed writ 2)")
	}
}

// indexExists checks whether a sqlite index exists.
func indexExists(db interface {
	QueryRow(string, ...interface{}) *sql.Row
}, name string) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// TestSphereSchemaV19RescopesDedupKey exercises the migration this writ
// adds: a database that already has the old v16 pending-thread dedup index
// (idx_messages_pending_thread_unique) and a pre-existing pending threaded
// row — the shape any real sphere.db is in today — must migrate cleanly to
// v19: the old index is gone, the new dedup_key-scoped index exists, the
// pre-existing row is left with dedup_key NULL (no heuristic backfill —
// see the sphereSchemaV19 doc comment), and threaded conversation sends
// that used to hit the old constraint now succeed.
func TestSphereSchemaV19RescopesDedupKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, ".store", "sphere.db")

	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Build a v16 database by hand: v1 (agents/schema_version) + v2
	// (messages/escalations) + v16 (the old pending-thread dedup index).
	// v17-v19 only touch the messages table, so this minimal base is
	// enough to exercise those migration steps in isolation.
	for _, ddl := range []string{sphereSchemaV1, sphereSchemaV2, sphereSchemaV16} {
		if _, err := s.db.Exec(ddl); err != nil {
			t.Fatalf("apply schema: %v", err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO schema_version VALUES (16)`); err != nil {
		t.Fatal(err)
	}

	// Sanity: the old index is present pre-migration.
	oldIdxExists, err := indexExists(s.db, "idx_messages_pending_thread_unique")
	if err != nil {
		t.Fatal(err)
	}
	if !oldIdxExists {
		t.Fatal("expected idx_messages_pending_thread_unique to exist before migration")
	}

	// Insert a pending threaded message the way it would exist under the
	// v16 constraint — this row predates the dedup_key column entirely,
	// which is exactly the "cannot heuristically backfill" case the
	// sphereSchemaV19 doc comment describes.
	if _, err := s.db.Exec(
		`INSERT INTO messages (id, sender, recipient, subject, body, priority, type, thread_id, delivery, read, created_at)
		 VALUES ('msg-preexisting', 'consul', 'autarch', 'Escalation', 'body', 1, 'notification', 'esc:pre', 'pending', 0, '2026-08-01T00:00:00Z')`,
	); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen — migrateSphere runs v17, v18, v19 in one transaction.
	s2 := openSphereAt(t, dbPath)

	newIdxExists, err := indexExists(s2.db, "idx_messages_pending_dedup_unique")
	if err != nil {
		t.Fatal(err)
	}
	if !newIdxExists {
		t.Fatal("expected idx_messages_pending_dedup_unique to exist after migration")
	}
	oldIdxExists, err = indexExists(s2.db, "idx_messages_pending_thread_unique")
	if err != nil {
		t.Fatal(err)
	}
	if oldIdxExists {
		t.Fatal("expected idx_messages_pending_thread_unique to be dropped after migration")
	}

	// The pre-existing row must not be backfilled.
	var dedupKey sql.NullString
	if err := s2.db.QueryRow(`SELECT dedup_key FROM messages WHERE id = 'msg-preexisting'`).Scan(&dedupKey); err != nil {
		t.Fatal(err)
	}
	if dedupKey.Valid {
		t.Fatalf("expected dedup_key to remain NULL on pre-existing row, got %q", dedupKey.String)
	}

	// The exact repro this writ fixes: a second message into the same
	// thread while the first ('msg-preexisting') is still pending must
	// now succeed.
	if _, err := s2.SendMessageWithThread("agent2", "autarch", "reply", "body2", 2, "notification", "esc:pre"); err != nil {
		t.Fatalf("second send into pending thread should succeed post-migration: %v", err)
	}

	// Escalation dedup must still work for the dedicated dedup_key path:
	// the first SendMessageWithThreadIfAbsent for a fresh threadID
	// inserts, and a second call with the same threadID is deduped.
	id1, inserted1, err := s2.SendMessageWithThreadIfAbsent("consul", "autarch", "Escalation", "body", 1, "notification", "esc:post-migration")
	if err != nil {
		t.Fatal(err)
	}
	if !inserted1 || id1 == "" {
		t.Fatalf("expected first SendMessageWithThreadIfAbsent to insert, got inserted=%v id=%q", inserted1, id1)
	}
	id2, inserted2, err := s2.SendMessageWithThreadIfAbsent("consul", "autarch", "Escalation", "body", 1, "notification", "esc:post-migration")
	if err != nil {
		t.Fatal(err)
	}
	if inserted2 || id2 != "" {
		t.Fatalf("expected second SendMessageWithThreadIfAbsent with the same threadID to be deduped, got inserted=%v id=%q", inserted2, id2)
	}
}
