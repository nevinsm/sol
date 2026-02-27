package store

import (
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
)

// setupRig creates a temporary rig store for testing.
func setupRig(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	os.Setenv("GT_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("GT_HOME") })

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := OpenRig("testrig")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// setupTown creates a temporary town store for testing.
func setupTown(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	os.Setenv("GT_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("GT_HOME") })

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := OpenTown()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSchemaCreation(t *testing.T) {
	s := setupRig(t)

	// Verify work_items table exists.
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='work_items'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected work_items table, got count=%d", count)
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
	for _, idx := range []string{"idx_work_status", "idx_work_assignee", "idx_work_priority", "idx_labels_label", "idx_mr_phase", "idx_mr_work_item"} {
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
	if version != 4 {
		t.Fatalf("expected schema version 4, got %d", version)
	}
}

func TestTownSchemaCreation(t *testing.T) {
	s := setupTown(t)

	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='agents'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected agents table, got count=%d", count)
	}
}

func TestMigrateTownV2(t *testing.T) {
	s := setupTown(t)

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

	// Verify schema_version is 3.
	var version int
	err = s.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("expected schema version 3, got %d", version)
	}

	// Verify indexes exist.
	for _, idx := range []string{"idx_messages_recipient", "idx_messages_thread"} {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected index %s, got count=%d", idx, count)
		}
	}
}

func TestMigrateTownV1ToV2(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("GT_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("GT_HOME") })
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	// Simulate a V1-only town database.
	dbPath := filepath.Join(dir, ".store", "town.db")
	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(townSchemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	// Create an agent at V1.
	_, err = s.db.Exec(
		`INSERT INTO agents (id, name, rig, role, state, created_at, updated_at)
		 VALUES ('myrig/Toast', 'Toast', 'myrig', 'polecat', 'idle', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen through OpenTown — should migrate to V2.
	s2, err := OpenTown()
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
	agent, err := s2.GetAgent("myrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "Toast" {
		t.Fatalf("expected agent name 'Toast', got %q", agent.Name)
	}

	// Verify schema_version is 3 (V1→V2→V3 all applied).
	var version int
	err = s2.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("expected schema version 3, got %d", version)
	}
}

func TestWorkItemCRUD(t *testing.T) {
	s := setupRig(t)

	// Create.
	id, err := s.CreateWorkItem("Test item", "A test work item", "operator", 2, []string{"gt:task"})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	// Get.
	item, err := s.GetWorkItem(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "Test item" {
		t.Fatalf("expected title 'Test item', got %q", item.Title)
	}
	if item.Description != "A test work item" {
		t.Fatalf("expected description 'A test work item', got %q", item.Description)
	}
	if item.Status != "open" {
		t.Fatalf("expected status 'open', got %q", item.Status)
	}
	if item.Priority != 2 {
		t.Fatalf("expected priority 2, got %d", item.Priority)
	}
	if item.CreatedBy != "operator" {
		t.Fatalf("expected created_by 'operator', got %q", item.CreatedBy)
	}
	if len(item.Labels) != 1 || item.Labels[0] != "gt:task" {
		t.Fatalf("expected labels [gt:task], got %v", item.Labels)
	}

	// List.
	items, err := s.ListWorkItems(ListFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	// Update.
	err = s.UpdateWorkItem(id, WorkItemUpdates{Status: "working", Assignee: "myrig/Toast"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWorkItem(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "working" {
		t.Fatalf("expected status 'working', got %q", item.Status)
	}
	if item.Assignee != "myrig/Toast" {
		t.Fatalf("expected assignee 'myrig/Toast', got %q", item.Assignee)
	}

	// Clear assignee.
	err = s.UpdateWorkItem(id, WorkItemUpdates{Assignee: "-"})
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWorkItem(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Assignee != "" {
		t.Fatalf("expected empty assignee, got %q", item.Assignee)
	}

	// Close.
	err = s.CloseWorkItem(id)
	if err != nil {
		t.Fatal(err)
	}
	item, err = s.GetWorkItem(id)
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

func TestLabels(t *testing.T) {
	s := setupRig(t)

	id, err := s.CreateWorkItem("Label test", "", "operator", 2, []string{"bug", "urgent"})
	if err != nil {
		t.Fatal(err)
	}

	// Verify initial labels.
	item, err := s.GetWorkItem(id)
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
	item, err = s.GetWorkItem(id)
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
	item, err = s.GetWorkItem(id)
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
	item, err = s.GetWorkItem(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Labels) != 2 {
		t.Fatalf("expected 2 labels after remove, got %d", len(item.Labels))
	}

	// Filter by label.
	items, err := s.ListWorkItems(ListFilters{Label: "backend"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item with label 'backend', got %d", len(items))
	}

	items, err = s.ListWorkItems(ListFilters{Label: "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items with label 'nonexistent', got %d", len(items))
	}
}

func TestIDGeneration(t *testing.T) {
	s := setupRig(t)

	pattern := regexp.MustCompile(`^gt-[0-9a-f]{8}$`)
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := s.CreateWorkItem("ID test", "", "operator", 2, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(id) {
			t.Fatalf("ID %q does not match pattern gt-[0-9a-f]{8}", id)
		}
		if seen[id] {
			t.Fatalf("duplicate ID generated: %s", id)
		}
		seen[id] = true
	}
}

func TestConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("GT_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("GT_HOME") })

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Open two connections to the same database.
	s1, err := OpenRig("concurrent")
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()

	s2, err := OpenRig("concurrent")
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
			_, err := s1.CreateWorkItem("item from s1", "", "operator", 2, nil)
			if err != nil {
				errs <- err
			}
		}(i)
		go func(n int) {
			defer wg.Done()
			_, err := s2.CreateWorkItem("item from s2", "", "operator", 2, nil)
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
	items, err := s1.ListWorkItems(ListFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 20 {
		t.Fatalf("expected 20 items, got %d", len(items))
	}
}

func TestNotFound(t *testing.T) {
	s := setupRig(t)

	_, err := s.GetWorkItem("gt-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent work item")
	}
	expected := `work item "gt-nonexist" not found`
	if err.Error() != expected {
		t.Fatalf("expected error %q, got %q", expected, err.Error())
	}
}

func TestAgentCRUD(t *testing.T) {
	s := setupTown(t)

	// Create.
	id, err := s.CreateAgent("Toast", "myrig", "polecat")
	if err != nil {
		t.Fatal(err)
	}
	if id != "myrig/Toast" {
		t.Fatalf("expected id 'myrig/Toast', got %q", id)
	}

	// Get.
	agent, err := s.GetAgent("myrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "Toast" {
		t.Fatalf("expected name 'Toast', got %q", agent.Name)
	}
	if agent.Rig != "myrig" {
		t.Fatalf("expected rig 'myrig', got %q", agent.Rig)
	}
	if agent.Role != "polecat" {
		t.Fatalf("expected role 'polecat', got %q", agent.Role)
	}
	if agent.State != "idle" {
		t.Fatalf("expected state 'idle', got %q", agent.State)
	}
	if agent.HookItem != "" {
		t.Fatalf("expected empty hook_item, got %q", agent.HookItem)
	}

	// Update state with hook.
	err = s.UpdateAgentState("myrig/Toast", "working", "gt-abc12345")
	if err != nil {
		t.Fatal(err)
	}
	agent, err = s.GetAgent("myrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "working" {
		t.Fatalf("expected state 'working', got %q", agent.State)
	}
	if agent.HookItem != "gt-abc12345" {
		t.Fatalf("expected hook_item 'gt-abc12345', got %q", agent.HookItem)
	}

	// Clear hook (back to idle).
	err = s.UpdateAgentState("myrig/Toast", "idle", "")
	if err != nil {
		t.Fatal(err)
	}
	agent, err = s.GetAgent("myrig/Toast")
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != "idle" {
		t.Fatalf("expected state 'idle', got %q", agent.State)
	}
	if agent.HookItem != "" {
		t.Fatalf("expected empty hook_item, got %q", agent.HookItem)
	}

	// List agents.
	s.CreateAgent("Jasper", "myrig", "polecat")
	s.CreateAgent("Wren", "myrig", "witness")

	agents, err := s.ListAgents("myrig", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	// List filtered by state.
	agents, err = s.ListAgents("myrig", "idle")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 idle agents, got %d", len(agents))
	}

	// Find idle polecat.
	idle, err := s.FindIdleAgent("myrig")
	if err != nil {
		t.Fatal(err)
	}
	if idle == nil {
		t.Fatal("expected an idle polecat")
	}
	if idle.Role != "polecat" {
		t.Fatalf("expected role 'polecat', got %q", idle.Role)
	}

	// Set all polecats to working, FindIdleAgent should return nil.
	s.UpdateAgentState("myrig/Toast", "working", "gt-item1")
	s.UpdateAgentState("myrig/Jasper", "working", "gt-item2")

	idle, err = s.FindIdleAgent("myrig")
	if err != nil {
		t.Fatal(err)
	}
	if idle != nil {
		t.Fatalf("expected no idle polecat, got %v", idle)
	}
}

func TestAgentNotFound(t *testing.T) {
	s := setupTown(t)

	_, err := s.GetAgent("norig/NoAgent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestListWorkItemsFilters(t *testing.T) {
	s := setupRig(t)

	// Create items with different statuses and priorities.
	id1, _ := s.CreateWorkItem("High priority", "", "operator", 1, []string{"feature"})
	id2, _ := s.CreateWorkItem("Normal priority", "", "operator", 2, []string{"bug"})
	s.CreateWorkItem("Low priority", "", "operator", 3, nil)

	// Assign one.
	s.UpdateWorkItem(id1, WorkItemUpdates{Assignee: "myrig/Toast"})
	s.UpdateWorkItem(id2, WorkItemUpdates{Status: "working"})

	// Filter by status.
	items, err := s.ListWorkItems(ListFilters{Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 open items, got %d", len(items))
	}

	// Filter by assignee.
	items, err = s.ListWorkItems(ListFilters{Assignee: "myrig/Toast"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 assigned item, got %d", len(items))
	}

	// Filter by priority.
	items, err = s.ListWorkItems(ListFilters{Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 high-priority item, got %d", len(items))
	}
}

func TestMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("GT_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("GT_HOME") })
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	// Open and close twice — migration should be idempotent.
	s1, err := OpenRig("idempotent")
	if err != nil {
		t.Fatal(err)
	}
	s1.CreateWorkItem("test", "", "operator", 2, nil)
	s1.Close()

	s2, err := OpenRig("idempotent")
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	items, err := s2.ListWorkItems(ListFilters{})
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

	// Verify schema version is 4.
	var version int
	err = s2.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != 4 {
		t.Fatalf("expected schema version 4 after reopen, got %d", version)
	}
}

func TestMigrateRigV1ToV2(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("GT_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("GT_HOME") })
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	// Simulate a V1-only database by manually creating it.
	dbPath := filepath.Join(dir, ".store", "v1test.db")
	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(rigSchemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	// Create a work item while at V1.
	_, err = s.db.Exec(
		`INSERT INTO work_items (id, title, status, priority, created_by, created_at, updated_at)
		 VALUES ('gt-v1item01', 'V1 item', 'open', 2, 'operator', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen through OpenRig — should migrate to V2.
	s2, err := OpenRig("v1test")
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

	// Verify existing work items are untouched.
	item, err := s2.GetWorkItem("gt-v1item01")
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "V1 item" {
		t.Fatalf("expected title 'V1 item', got %q", item.Title)
	}

	// Verify schema version is 4 (V1→V2→V3→V4 all applied).
	var version int
	err = s2.db.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}
	if version != 4 {
		t.Fatalf("expected schema version 4, got %d", version)
	}
}

func TestCreateWorkItemWithOpts(t *testing.T) {
	s := setupRig(t)

	id, err := s.CreateWorkItemWithOpts(CreateWorkItemOpts{
		Title:       "Resolve conflicts",
		Description: "Resolve merge conflicts for branch X",
		CreatedBy:   "myrig/refinery",
		Priority:    1,
		Labels:      []string{"conflict-resolution", "source-mr:mr-12345678"},
		ParentID:    "gt-parent01",
	})
	if err != nil {
		t.Fatal(err)
	}

	item, err := s.GetWorkItem(id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "Resolve conflicts" {
		t.Errorf("title = %q, want %q", item.Title, "Resolve conflicts")
	}
	if item.Priority != 1 {
		t.Errorf("priority = %d, want 1", item.Priority)
	}
	if item.ParentID != "gt-parent01" {
		t.Errorf("parent_id = %q, want %q", item.ParentID, "gt-parent01")
	}
	if item.CreatedBy != "myrig/refinery" {
		t.Errorf("created_by = %q, want %q", item.CreatedBy, "myrig/refinery")
	}
	if len(item.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d: %v", len(item.Labels), item.Labels)
	}
}

func TestCreateWorkItemWithOptsNoParent(t *testing.T) {
	s := setupRig(t)

	id, err := s.CreateWorkItemWithOpts(CreateWorkItemOpts{
		Title:     "No parent",
		CreatedBy: "operator",
	})
	if err != nil {
		t.Fatal(err)
	}

	item, err := s.GetWorkItem(id)
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
	item := &WorkItem{Labels: []string{"bug", "urgent", "conflict-resolution"}}

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
	empty := &WorkItem{}
	if empty.HasLabel("anything") {
		t.Error("expected HasLabel on empty labels = false")
	}
}
