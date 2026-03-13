package store

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// setupWorld creates a temporary world store for testing.
func setupWorld(t *testing.T) *WorldStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// setupSphere creates a temporary sphere store for testing.
func setupSphere(t *testing.T) *SphereStore {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSchemaCreation(t *testing.T) {
	s := setupWorld(t)

	// Verify writs table exists.
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='writs'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected writs table, got count=%d", count)
	}

	// Verify labels table exists.
	err = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='labels'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected labels table, got count=%d", count)
	}

	// Verify merge_requests table exists (V2).
	err = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='merge_requests'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected merge_requests table, got count=%d", count)
	}

	// Verify indexes exist.
	for _, idx := range []string{"idx_writ_status", "idx_writ_assignee", "idx_writ_priority", "idx_labels_label", "idx_mr_phase", "idx_mr_writ", "idx_mr_blocked_by"} {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected index %s, got count=%d", idx, count)
		}
	}

	// Verify schema version.
	var version int
	err = s.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != CurrentWorldSchema {
		t.Fatalf("expected schema version %d, got %d", CurrentWorldSchema, version)
	}
}

func TestSphereSchemaCreation(t *testing.T) {
	s := setupSphere(t)

	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agents'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected agents table, got count=%d", count)
	}
}

func TestMigrateSphereV5(t *testing.T) {
	s := setupSphere(t)

	// Verify messages table exists.
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected messages table, got count=%d", count)
	}

	// Verify escalations table exists.
	err = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='escalations'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected escalations table, got count=%d", count)
	}

	// Verify schema_version is latest.
	var version int
	err = s.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != CurrentSphereSchema {
		t.Fatalf("expected schema version %d, got %d", CurrentSphereSchema, version)
	}

	// Verify indexes exist.
	for _, idx := range []string{"idx_messages_recipient", "idx_messages_thread", "idx_agents_world_state", "idx_escalations_status", "idx_caravan_items_world"} {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected index %s, got count=%d", idx, count)
		}
	}
}

func TestMigrateSphereV1ToLatest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	// Simulate a V1-only sphere database.
	dbPath := filepath.Join(dir, ".store", "sphere.db")
	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(sphereSchemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	// Create an agent at V1.
	_, err = s.db.Exec(
		`INSERT INTO agents (id, name, rig, role, state, created_at, updated_at)
		 VALUES ('haven/Toast', 'Toast', 'haven', 'agent', 'idle', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen through OpenSphere — should migrate to V2.
	s2, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// Verify messages table exists.
	var count int
	err = s2.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected messages table, got count=%d", count)
	}

	// Verify existing agents are untouched.
	agent, err := s2.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "Toast" {
		t.Fatalf("expected agent name 'Toast', got %q", agent.Name)
	}

	// Verify schema_version is latest (all sphere migrations applied).
	var version int
	err = s2.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != CurrentSphereSchema {
		t.Fatalf("expected schema version %d, got %d", CurrentSphereSchema, version)
	}

	// Verify phase column was added to caravan_items.
	phaseExists, err := columnExists(s2.db, "caravan_items", "phase")
	if err != nil {
		t.Fatalf("failed to check phase column: %v", err)
	}
	if !phaseExists {
		t.Fatal("expected phase column on caravan_items after V1→latest migration")
	}

	// Verify active_writ column exists (V10 migration renamed tether_item).
	activeWritExists, err := columnExists(s2.db, "agents", "active_writ")
	if err != nil {
		t.Fatalf("failed to check active_writ column: %v", err)
	}
	if !activeWritExists {
		t.Fatal("expected active_writ column on agents after V1→latest migration")
	}
}

func TestMigrateSphereV9ToV10(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	// Simulate a V9 sphere database with tether_item column.
	dbPath := filepath.Join(dir, ".store", "sphere.db")
	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Create agents table with tether_item (as it existed before V10),
	// and escalations table (as it existed before V11).
	_, err = s.db.Exec(`
		CREATE TABLE agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			world TEXT NOT NULL,
			role TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'idle',
			tether_item TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE escalations (
			id           TEXT PRIMARY KEY,
			severity     TEXT NOT NULL,
			source       TEXT NOT NULL,
			description  TEXT NOT NULL,
			status       TEXT NOT NULL DEFAULT 'open',
			acknowledged INTEGER NOT NULL DEFAULT 0,
			created_at   TEXT NOT NULL,
			updated_at   TEXT NOT NULL
		);
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version VALUES (9);
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Seed agents with tether_item values.
	_, err = s.db.Exec(`
		INSERT INTO agents (id, name, world, role, state, tether_item, created_at, updated_at)
		VALUES ('haven/Toast', 'Toast', 'haven', 'agent', 'working', 'sol-a1b2c3d4e5f6a7b8', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`
		INSERT INTO agents (id, name, world, role, state, tether_item, created_at, updated_at)
		VALUES ('haven/Meridian', 'Meridian', 'haven', 'envoy', 'idle', NULL, '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen through OpenSphere — should migrate to V10.
	s2, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// Verify schema version is V10.
	var version int
	err = s2.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != CurrentSphereSchema {
		t.Fatalf("expected schema version %d, got %d", CurrentSphereSchema, version)
	}

	// Verify active_writ column exists.
	activeWritExists, err := columnExists(s2.db, "agents", "active_writ")
	if err != nil {
		t.Fatalf("failed to check active_writ column: %v", err)
	}
	if !activeWritExists {
		t.Fatal("expected active_writ column after V10 migration")
	}

	// Verify tether_item column no longer exists.
	oldColExists, err := columnExists(s2.db, "agents", "tether_item")
	if err != nil {
		t.Fatalf("failed to check tether_item column: %v", err)
	}
	if oldColExists {
		t.Fatal("expected tether_item column to be renamed after V10 migration")
	}

	// Verify data was preserved.
	agent, err := s2.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.ActiveWrit != "sol-a1b2c3d4e5f6a7b8" {
		t.Fatalf("expected active_writ 'sol-a1b2c3d4e5f6a7b8', got %q", agent.ActiveWrit)
	}

	// Verify NULL values are preserved.
	meridian, err := s2.GetAgent("haven/Meridian")
	if err != nil {
		t.Fatal(err)
	}
	if meridian.ActiveWrit != "" {
		t.Fatalf("expected empty active_writ for idle agent, got %q", meridian.ActiveWrit)
	}

	// Verify source_ref column exists on escalations (V11 migration).
	sourceRefExists, err := columnExists(s2.db, "escalations", "source_ref")
	if err != nil {
		t.Fatalf("failed to check source_ref column: %v", err)
	}
	if !sourceRefExists {
		t.Fatal("expected source_ref column on escalations after V11 migration")
	}

	// Verify last_notified_at column exists on escalations (V12 migration).
	lastNotifiedAtExists, err := columnExists(s2.db, "escalations", "last_notified_at")
	if err != nil {
		t.Fatalf("failed to check last_notified_at column: %v", err)
	}
	if !lastNotifiedAtExists {
		t.Fatal("expected last_notified_at column on escalations after V12 migration")
	}

	// Verify partial index on source_ref exists (V12 migration).
	var idxCount int
	err = s2.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_escalations_source_ref'`).Scan(&idxCount)
	if err != nil {
		t.Fatalf("failed to check source_ref index: %v", err)
	}
	if idxCount != 1 {
		t.Fatal("expected idx_escalations_source_ref index after V12 migration")
	}
}

func TestMigrateSphereV11ToV12(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	// Simulate a V11 sphere database.
	dbPath := filepath.Join(dir, ".store", "sphere.db")
	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Create tables as they existed at V11.
	_, err = s.db.Exec(`
		CREATE TABLE agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			world TEXT NOT NULL,
			role TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'idle',
			active_writ TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE escalations (
			id           TEXT PRIMARY KEY,
			severity     TEXT NOT NULL,
			source       TEXT NOT NULL,
			description  TEXT NOT NULL,
			source_ref   TEXT,
			status       TEXT NOT NULL DEFAULT 'open',
			acknowledged INTEGER NOT NULL DEFAULT 0,
			created_at   TEXT NOT NULL,
			updated_at   TEXT NOT NULL
		);
		CREATE TABLE schema_version (version INTEGER NOT NULL);
		INSERT INTO schema_version VALUES (11);
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Seed escalation data to verify it survives migration.
	_, err = s.db.Exec(`
		INSERT INTO escalations (id, severity, source, description, source_ref, status, acknowledged, created_at, updated_at)
		VALUES ('esc-existing01', 'high', 'ember/forge', 'Test escalation', 'mr:mr-abc123', 'open', 0, '2025-06-01T10:00:00Z', '2025-06-01T10:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen through OpenSphere — should migrate V11 → V13.
	s2, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// Verify schema version is current (13).
	var version int
	err = s2.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != CurrentSphereSchema {
		t.Fatalf("expected schema version %d, got %d", CurrentSphereSchema, version)
	}

	// Verify last_notified_at column exists and is nullable.
	lastNotifiedExists, err := columnExists(s2.db, "escalations", "last_notified_at")
	if err != nil {
		t.Fatalf("failed to check last_notified_at column: %v", err)
	}
	if !lastNotifiedExists {
		t.Fatal("expected last_notified_at column after V12 migration")
	}

	// Verify partial index on source_ref exists.
	var idxCount int
	err = s2.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_escalations_source_ref'`).Scan(&idxCount)
	if err != nil {
		t.Fatalf("failed to check source_ref index: %v", err)
	}
	if idxCount != 1 {
		t.Fatal("expected idx_escalations_source_ref index after V12 migration")
	}

	// Verify existing escalation data survived migration.
	esc, err := s2.GetEscalation("esc-existing01")
	if err != nil {
		t.Fatal(err)
	}
	if esc.Severity != "high" {
		t.Fatalf("expected severity 'high', got %q", esc.Severity)
	}
	if esc.SourceRef != "mr:mr-abc123" {
		t.Fatalf("expected source_ref 'mr:mr-abc123', got %q", esc.SourceRef)
	}
	if esc.LastNotifiedAt != nil {
		t.Fatalf("expected nil LastNotifiedAt for pre-existing escalation, got %v", esc.LastNotifiedAt)
	}

	// Verify UpdateEscalationLastNotified works on migrated data.
	if err := s2.UpdateEscalationLastNotified("esc-existing01"); err != nil {
		t.Fatal(err)
	}
	esc, err = s2.GetEscalation("esc-existing01")
	if err != nil {
		t.Fatal(err)
	}
	if esc.LastNotifiedAt == nil {
		t.Fatal("expected non-nil LastNotifiedAt after update")
	}
}

func TestWritCRUD(t *testing.T) {
	s := setupWorld(t)

	// Create.
	id, err := s.CreateWrit("Test item", "A test writ", "autarch", 2, []string{"sol:task"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	// Get.
	item, err := s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "Test item" {
		t.Fatalf("expected title 'Test item', got %q", item.Title)
	}
	if item.Description != "A test writ" {
		t.Fatalf("expected description 'A test writ', got %q", item.Description)
	}
	if item.Status != "open" {
		t.Fatalf("expected status 'open', got %q", item.Status)
	}
	if item.Priority != 2 {
		t.Fatalf("expected priority 2, got %d", item.Priority)
	}
	if item.CreatedBy != "autarch" {
		t.Fatalf("expected created_by 'autarch', got %q", item.CreatedBy)
	}
	if len(item.Labels) != 1 || item.Labels[0] != "sol:task" {
		t.Fatalf("expected labels [sol:task], got %v", item.Labels)
	}

	// List.
	items, err := s.ListWrits(ListFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	// Update.
	err = s.UpdateWrit(id, WritUpdates{Status: "working", Assignee: "haven/Toast"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "working" {
		t.Fatalf("expected status 'working', got %q", item.Status)
	}
	if item.Assignee != "haven/Toast" {
		t.Fatalf("expected assignee 'haven/Toast', got %q", item.Assignee)
	}

	// Clear assignee.
	err = s.UpdateWrit(id, WritUpdates{Assignee: "-"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Assignee != "" {
		t.Fatalf("expected empty assignee, got %q", item.Assignee)
	}

	// Update title and description.
	err = s.UpdateWrit(id, WritUpdates{Title: "New title", Description: "New desc"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "New title" {
		t.Fatalf("expected title 'New title', got %q", item.Title)
	}
	if item.Description != "New desc" {
		t.Fatalf("expected description 'New desc', got %q", item.Description)
	}

	// Update only title, description should remain unchanged.
	err = s.UpdateWrit(id, WritUpdates{Title: "Updated title"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "Updated title" {
		t.Fatalf("expected title 'Updated title', got %q", item.Title)
	}
	if item.Description != "New desc" {
		t.Fatalf("expected description unchanged 'New desc', got %q", item.Description)
	}

	// Close.
	_, err = s.CloseWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "closed" {
		t.Fatalf("expected status 'closed', got %q", item.Status)
	}
	if item.ClosedAt == nil {
		t.Fatal("expected closed_at to be set")
	}
}

func TestUpdateWritInvalidStatus(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWrit("Status test", "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	err = s.UpdateWrit(id, WritUpdates{Status: WritStatus("banana")})
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
	if err.Error() != `invalid writ status "banana"` {
		t.Fatalf("unexpected error: %v", err)
	}

	// Valid statuses should work.
	for _, status := range []WritStatus{WritOpen, WritTethered, WritWorking, WritResolve, WritDone, WritClosed} {
		if err := s.UpdateWrit(id, WritUpdates{Status: status}); err != nil {
			t.Fatalf("expected valid status %q to succeed, got: %v", status, err)
		}
	}
}

func TestLabels(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWrit("Label test", "", "autarch", 2, []string{"bug", "urgent"})
	if err != nil {
		t.Fatal(err)
	}

	// Verify initial labels.
	item, err := s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(item.Labels))
	}

	// Add label.
	err = s.AddLabel(id, "backend")
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Labels) != 3 {
		t.Fatalf("expected 3 labels, got %d", len(item.Labels))
	}

	// Add duplicate label (no-op).
	err = s.AddLabel(id, "bug")
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Labels) != 3 {
		t.Fatalf("expected 3 labels after duplicate add, got %d", len(item.Labels))
	}

	// Remove label.
	err = s.RemoveLabel(id, "urgent")
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Labels) != 2 {
		t.Fatalf("expected 2 labels after remove, got %d", len(item.Labels))
	}

	// Filter by label.
	items, err := s.ListWrits(ListFilters{Label: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item with label 'backend', got %d", len(items))
	}

	items, err = s.ListWrits(ListFilters{Label: "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items with label 'nonexistent', got %d", len(items))
	}
}

func TestIDGeneration(t *testing.T) {
	s := setupWorld(t)

	pattern := regexp.MustCompile(`^sol-[0-9a-f]{16}$`)
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := s.CreateWrit("ID test", "", "autarch", 2, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(id) {
			t.Fatalf("ID %q does not match pattern sol-[0-9a-f]{16}", id)
		}
		if seen[id] {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = true
	}
}

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Open two connections to the same database.
	s1, err := OpenWorld("concurrent")
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()

	s2, err := OpenWorld("concurrent")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// Write from both connections concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			_, err := s1.CreateWrit("item from s1", "", "autarch", 2, nil)
			if err != nil {
				errs <- err
			}
		}(i)
		go func(n int) {
			defer wg.Done()
			_, err := s2.CreateWrit("item from s2", "", "autarch", 2, nil)
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent write error: %v", err)
	}

	// Verify all items were written.
	items, err := s1.ListWrits(ListFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("expected 20 items, got %d", len(items))
	}
}

func TestNotFound(t *testing.T) {
	s := setupWorld(t)

	_, err := s.GetWrit("sol-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent writ")
	}
	expected := `writ "sol-nonexist": not found`
	if err.Error() != expected {
		t.Fatalf("expected error %q, got %q", expected, err.Error())
	}
}

func TestAgentCRUD(t *testing.T) {
	s := setupSphere(t)

	// Create.
	id, err := s.CreateAgent("Toast", "haven", "outpost")
	if err != nil {
		t.Fatal(err)
	}
	if id != "haven/Toast" {
		t.Fatalf("expected id 'haven/Toast', got %q", id)
	}

	// Get.
	agent, err := s.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "Toast" {
		t.Fatalf("expected name 'Toast', got %q", agent.Name)
	}
	if agent.World != "haven" {
		t.Fatalf("expected world 'haven', got %q", agent.World)
	}
	if agent.Role != "outpost" {
		t.Fatalf("expected role 'outpost', got %q", agent.Role)
	}
	if agent.State != "idle" {
		t.Fatalf("expected state 'idle', got %q", agent.State)
	}
	if agent.ActiveWrit != "" {
		t.Fatalf("expected empty active_writ, got %q", agent.ActiveWrit)
	}

	// Update state with tether.
	err = s.UpdateAgentState("haven/Toast", "working", "sol-abc12345")
	if err != nil {
		t.Fatal(err)
	}
	agent, err = s.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Fatalf("expected state 'working', got %q", agent.State)
	}
	if agent.ActiveWrit != "sol-abc12345" {
		t.Fatalf("expected active_writ 'sol-abc12345', got %q", agent.ActiveWrit)
	}

	// Clear tether (back to idle).
	err = s.UpdateAgentState("haven/Toast", "idle", "")
	if err != nil {
		t.Fatal(err)
	}
	agent, err = s.GetAgent("haven/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "idle" {
		t.Fatalf("expected state 'idle', got %q", agent.State)
	}
	if agent.ActiveWrit != "" {
		t.Fatalf("expected empty active_writ, got %q", agent.ActiveWrit)
	}

	// List agents.
	s.CreateAgent("Jasper", "haven", "outpost")
	s.CreateAgent("Wren", "haven", "sentinel")

	agents, err := s.ListAgents("haven", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	// List filtered by state.
	agents, err = s.ListAgents("haven", "idle")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 idle agents, got %d", len(agents))
	}

	// Find idle agent.
	idle, err := s.FindIdleAgent("haven")
	if err != nil {
		t.Fatal(err)
	}
	if idle == nil {
		t.Fatal("expected an idle agent")
	}
	if idle.Role != "outpost" {
		t.Fatalf("expected role 'outpost', got %q", idle.Role)
	}

	// Set all agents to working, FindIdleAgent should return nil.
	s.UpdateAgentState("haven/Toast", "working", "sol-item1")
	s.UpdateAgentState("haven/Jasper", "working", "sol-item2")

	idle, err = s.FindIdleAgent("haven")
	if err != nil {
		t.Fatal(err)
	}
	if idle != nil {
		t.Fatalf("expected no idle agent, got %v", idle)
	}
}

func TestDeleteAgentsForWorld(t *testing.T) {
	s := setupSphere(t)

	// Create agents in world "alpha" and "beta".
	s.CreateAgent("Toast", "alpha", "outpost")
	s.CreateAgent("Jasper", "alpha", "outpost")
	s.CreateAgent("Wren", "beta", "outpost")

	// Delete agents for "alpha".
	if err := s.DeleteAgentsForWorld("alpha"); err != nil {
		t.Fatal(err)
	}

	// Verify alpha agents are gone.
	agents, err := s.ListAgents("alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents for alpha, got %d", len(agents))
	}

	// Verify beta agents still exist.
	agents, err = s.ListAgents("beta", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent for beta, got %d", len(agents))
	}
}

func TestDeleteAgentsForWorldEmpty(t *testing.T) {
	s := setupSphere(t)

	// Delete agents for a world that has none — should not error.
	if err := s.DeleteAgentsForWorld("nonexistent"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestDeleteAgent(t *testing.T) {
	s := setupSphere(t)

	s.CreateAgent("Toast", "alpha", "outpost")
	s.CreateAgent("Jasper", "alpha", "outpost")

	// Delete one agent.
	if err := s.DeleteAgent("alpha/Toast"); err != nil {
		t.Fatal(err)
	}

	// Verify deleted agent is gone.
	_, err := s.GetAgent("alpha/Toast")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for deleted agent, got: %v", err)
	}

	// Verify other agent still exists.
	agent, err := s.GetAgent("alpha/Jasper")
	if err != nil {
		t.Fatalf("expected Jasper to still exist: %v", err)
	}
	if agent.Name != "Jasper" {
		t.Errorf("expected name Jasper, got %q", agent.Name)
	}
}

func TestDeleteAgentNotFound(t *testing.T) {
	s := setupSphere(t)

	err := s.DeleteAgent("noworld/NoAgent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound for nonexistent agent, got: %v", err)
	}
}

func TestAgentNotFound(t *testing.T) {
	s := setupSphere(t)

	_, err := s.GetAgent("noworld/NoAgent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestListWritsFilters(t *testing.T) {
	s := setupWorld(t)

	// Create items with different statuses and priorities.
	id1, _ := s.CreateWrit("High priority", "", "autarch", 1, []string{"feature"})
	id2, _ := s.CreateWrit("Normal priority", "", "autarch", 2, []string{"bug"})
	s.CreateWrit("Low priority", "", "autarch", 3, nil)

	// Assign one.
	s.UpdateWrit(id1, WritUpdates{Assignee: "haven/Toast"})
	s.UpdateWrit(id2, WritUpdates{Status: "working"})

	// Filter by status.
	items, err := s.ListWrits(ListFilters{Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 open items, got %d", len(items))
	}

	// Filter by assignee.
	items, err = s.ListWrits(ListFilters{Assignee: "haven/Toast"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 assigned item, got %d", len(items))
	}

	// Filter by priority.
	items, err = s.ListWrits(ListFilters{Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 high-priority item, got %d", len(items))
	}
}

func TestMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	// Open and close twice — migration should be idempotent.
	s1, err := OpenWorld("idempotent")
	if err != nil {
		t.Fatal(err)
	}
	s1.CreateWrit("test", "", "autarch", 2, nil)
	s1.Close()

	s2, err := OpenWorld("idempotent")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	items, err := s2.ListWrits(ListFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item after reopen, got %d", len(items))
	}

	// Verify merge_requests table exists after reopen.
	var count int
	err = s2.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='merge_requests'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected merge_requests table after reopen, got count=%d", count)
	}

	// Verify schema version is 7.
	var version int
	err = s2.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != CurrentWorldSchema {
		t.Fatalf("expected schema version %d after reopen, got %d", CurrentWorldSchema, version)
	}
}

func TestMigrateWorldV1ToV4(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	// Simulate a V1-only database by manually creating it.
	dbPath := filepath.Join(dir, ".store", "v1test.db")
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
	// Create a writ while at V1.
	_, err = s.db.Exec(
		`INSERT INTO writs (id, title, status, priority, created_by, created_at, updated_at)
		 VALUES ('sol-v1item01', 'V1 item', 'open', 2, 'operator', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen through OpenWorld — should migrate to V2.
	s2, err := OpenWorld("v1test")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// Verify merge_requests table exists.
	var count int
	err = s2.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='merge_requests'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected merge_requests table, got count=%d", count)
	}

	// Verify existing writs are untouched.
	item, err := s2.GetWrit("sol-v1item01")
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "V1 item" {
		t.Fatalf("expected title 'V1 item', got %q", item.Title)
	}

	// Verify schema version is latest (all world migrations applied).
	var version int
	err = s2.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != CurrentWorldSchema {
		t.Fatalf("expected schema version %d, got %d", CurrentWorldSchema, version)
	}
}

func TestCreateWritWithOpts(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWritWithOpts(CreateWritOpts{
		Title:       "Resolve conflicts",
		Description: "Resolve merge conflicts for branch X",
		CreatedBy:   "haven/forge",
		Priority:    1,
		Labels:      []string{"conflict-resolution", "source-mr:mr-12345678"},
		ParentID:    "sol-parent01",
	})
	if err != nil {
		t.Fatal(err)
	}

	item, err := s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "Resolve conflicts" {
		t.Errorf("title = %q, want %q", item.Title, "Resolve conflicts")
	}
	if item.Priority != 1 {
		t.Errorf("priority = %d, want 1", item.Priority)
	}
	if item.ParentID != "sol-parent01" {
		t.Errorf("parent_id = %q, want %q", item.ParentID, "sol-parent01")
	}
	if item.CreatedBy != "haven/forge" {
		t.Errorf("created_by = %q, want %q", item.CreatedBy, "haven/forge")
	}
	if len(item.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d: %v", len(item.Labels), item.Labels)
	}
}

func TestCreateWritWithOptsNoParent(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWritWithOpts(CreateWritOpts{
		Title:     "No parent",
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatal(err)
	}

	item, err := s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.ParentID != "" {
		t.Errorf("parent_id = %q, want empty", item.ParentID)
	}
	if item.Priority != 2 {
		t.Errorf("priority = %d, want 2 (default)", item.Priority)
	}
}

func TestHasLabel(t *testing.T) {
	item := &Writ{Labels: []string{"bug", "urgent", "conflict-resolution"}}

	if !item.HasLabel("bug") {
		t.Error("expected HasLabel(\"bug\") = true")
	}
	if !item.HasLabel("conflict-resolution") {
		t.Error("expected HasLabel(\"conflict-resolution\") = true")
	}
	if item.HasLabel("feature") {
		t.Error("expected HasLabel(\"feature\") = false")
	}
	if item.HasLabel("") {
		t.Error("expected HasLabel(\"\") = false")
	}

	// Empty labels.
	empty := &Writ{}
	if empty.HasLabel("anything") {
		t.Error("expected HasLabel on empty labels = false")
	}
}

func TestColumnExists(t *testing.T) {
	s := setupWorld(t)
	// writs table has a "title" column.
	exists, err := columnExists(s.db, "writs", "title")
	if err != nil {
		t.Fatalf("columnExists error: %v", err)
	}
	if !exists {
		t.Fatal("expected title column to exist")
	}
	// Nonexistent column.
	exists, err = columnExists(s.db, "writs", "nonexistent")
	if err != nil {
		t.Fatalf("columnExists error: %v", err)
	}
	if exists {
		t.Fatal("expected nonexistent column to not exist")
	}
}

func TestTableExists(t *testing.T) {
	s := setupWorld(t)
	exists, err := tableExists(s.db, "writs")
	if err != nil {
		t.Fatalf("tableExists error: %v", err)
	}
	if !exists {
		t.Fatal("expected writs table to exist")
	}
	exists, err = tableExists(s.db, "nonexistent")
	if err != nil {
		t.Fatalf("tableExists error: %v", err)
	}
	if exists {
		t.Fatal("expected nonexistent table to not exist")
	}
}

func TestErrNotFound(t *testing.T) {
	worldStore := setupWorld(t)
	sphereStore := setupSphere(t)

	// GetAgent with nonexistent ID.
	_, err := sphereStore.GetAgent("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected errors.Is(err, ErrNotFound), got: %v", err)
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected error to contain entity ID, got: %v", err)
	}

	// GetWrit with nonexistent ID.
	_, err = worldStore.GetWrit("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent writ")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected errors.Is(err, ErrNotFound), got: %v", err)
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("expected error to contain entity ID, got: %v", err)
	}
}

func TestInvalidCaravanStatus(t *testing.T) {
	s := setupSphere(t)

	id, err := s.CreateCaravan("test-caravan", "autarch")
	if err != nil {
		t.Fatal(err)
	}

	err = s.UpdateCaravanStatus(id, "banana")
	if err == nil {
		t.Fatal("expected error for invalid caravan status")
	}
	if !strings.Contains(err.Error(), "invalid caravan status") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInvalidAgentState(t *testing.T) {
	s := setupSphere(t)

	_, err := s.CreateAgent("TestAgent", "testworld", "outpost")
	if err != nil {
		t.Fatal(err)
	}

	err = s.UpdateAgentState("testworld/TestAgent", "banana", "")
	if err == nil {
		t.Fatal("expected error for invalid agent state")
	}
	if !strings.Contains(err.Error(), "invalid agent state") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateWritWithOptsKindAndMetadata(t *testing.T) {
	s := setupWorld(t)

	meta := map[string]any{"key1": "val1", "key2": float64(42)}
	id, err := s.CreateWritWithOpts(CreateWritOpts{
		Title:     "Analysis task",
		CreatedBy: "autarch",
		Kind:      "analysis",
		Metadata:  meta,
	})
	if err != nil {
		t.Fatal(err)
	}

	item, err := s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != "analysis" {
		t.Errorf("kind = %q, want %q", item.Kind, "analysis")
	}
	if item.Metadata == nil {
		t.Fatal("metadata is nil, want non-nil")
	}
	if item.Metadata["key1"] != "val1" {
		t.Errorf("metadata[key1] = %v, want %q", item.Metadata["key1"], "val1")
	}
	if item.Metadata["key2"] != float64(42) {
		t.Errorf("metadata[key2] = %v, want 42", item.Metadata["key2"])
	}
}

func TestCreateWritWithOptsDefaultKind(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWritWithOpts(CreateWritOpts{
		Title:     "Default kind",
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatal(err)
	}

	item, err := s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != "code" {
		t.Errorf("kind = %q, want %q (default)", item.Kind, "code")
	}
	if item.Metadata != nil {
		t.Errorf("metadata = %v, want nil", item.Metadata)
	}
}

func TestCloseWritWithReason(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWrit("Close test", "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Close with reason.
	if _, err := s.CloseWrit(id, "completed"); err != nil {
		t.Fatal(err)
	}
	item, err := s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "closed" {
		t.Errorf("status = %q, want %q", item.Status, "closed")
	}
	if item.CloseReason != "completed" {
		t.Errorf("close_reason = %q, want %q", item.CloseReason, "completed")
	}
	if item.ClosedAt == nil {
		t.Error("expected closed_at to be set")
	}
}

func TestCloseWritWithoutReason(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWrit("Close no reason", "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.CloseWrit(id); err != nil {
		t.Fatal(err)
	}
	item, err := s.GetWrit(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.CloseReason != "" {
		t.Errorf("close_reason = %q, want empty", item.CloseReason)
	}
}

func TestSetWritMetadata(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWritWithOpts(CreateWritOpts{
		Title:     "Metadata test",
		CreatedBy: "autarch",
		Metadata:  map[string]any{"a": "1", "b": "2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Merge: add new key, update existing.
	if err := s.SetWritMetadata(id, map[string]any{"b": "updated", "c": "3"}); err != nil {
		t.Fatal(err)
	}
	meta, err := s.GetWritMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta["a"] != "1" {
		t.Errorf("a = %v, want %q", meta["a"], "1")
	}
	if meta["b"] != "updated" {
		t.Errorf("b = %v, want %q", meta["b"], "updated")
	}
	if meta["c"] != "3" {
		t.Errorf("c = %v, want %q", meta["c"], "3")
	}

	// Delete key by setting to nil.
	if err := s.SetWritMetadata(id, map[string]any{"a": nil}); err != nil {
		t.Fatal(err)
	}
	meta, err = s.GetWritMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := meta["a"]; ok {
		t.Error("expected key 'a' to be deleted")
	}
	if meta["b"] != "updated" {
		t.Errorf("b = %v, want %q (unchanged)", meta["b"], "updated")
	}
}

func TestGetWritMetadataEmpty(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWrit("No metadata", "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	meta, err := s.GetWritMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		t.Errorf("expected nil metadata, got %v", meta)
	}
}

func TestSetWritMetadataOnEmptyWrit(t *testing.T) {
	s := setupWorld(t)

	id, err := s.CreateWrit("Empty meta writ", "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Set metadata on a writ that had no metadata.
	if err := s.SetWritMetadata(id, map[string]any{"x": "y"}); err != nil {
		t.Fatal(err)
	}
	meta, err := s.GetWritMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta["x"] != "y" {
		t.Errorf("x = %v, want %q", meta["x"], "y")
	}
}

func TestListWritsIncludesKindAndMetadata(t *testing.T) {
	s := setupWorld(t)

	_, err := s.CreateWritWithOpts(CreateWritOpts{
		Title:     "Listed writ",
		CreatedBy: "autarch",
		Kind:      "analysis",
		Metadata:  map[string]any{"foo": "bar"},
	})
	if err != nil {
		t.Fatal(err)
	}

	items, err := s.ListWrits(ListFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one writ")
	}
	found := false
	for _, w := range items {
		if w.Kind == "analysis" {
			found = true
			if w.Metadata == nil || w.Metadata["foo"] != "bar" {
				t.Errorf("metadata = %v, want {foo: bar}", w.Metadata)
			}
		}
	}
	if !found {
		t.Error("expected to find writ with kind=analysis")
	}
}
