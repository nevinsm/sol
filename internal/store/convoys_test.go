package store

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestCreateConvoy(t *testing.T) {
	s := setupTown(t)

	id, err := s.CreateConvoy("auth-feature", "operator")
	if err != nil {
		t.Fatalf("CreateConvoy() error: %v", err)
	}

	// Verify ID format.
	pattern := regexp.MustCompile(`^convoy-[0-9a-f]{8}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("convoy ID %q does not match pattern convoy-[0-9a-f]{8}", id)
	}

	// Verify with GetConvoy.
	c, err := s.GetConvoy(id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "auth-feature" {
		t.Fatalf("expected name %q, got %q", "auth-feature", c.Name)
	}
	if c.Status != "open" {
		t.Fatalf("expected status %q, got %q", "open", c.Status)
	}
	if c.Owner != "operator" {
		t.Fatalf("expected owner %q, got %q", "operator", c.Owner)
	}
	if c.ClosedAt != nil {
		t.Fatalf("expected nil closed_at, got %v", c.ClosedAt)
	}
}

func TestGetConvoyNotFound(t *testing.T) {
	s := setupTown(t)

	_, err := s.GetConvoy("convoy-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent convoy")
	}
}

func TestAddConvoyItem(t *testing.T) {
	s := setupTown(t)

	convoyID, _ := s.CreateConvoy("test-convoy", "operator")

	// Add 3 items.
	s.AddConvoyItem(convoyID, "gt-11111111", "myrig")
	s.AddConvoyItem(convoyID, "gt-22222222", "myrig")
	s.AddConvoyItem(convoyID, "gt-33333333", "myrig")

	items, err := s.ListConvoyItems(convoyID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
}

func TestRemoveConvoyItem(t *testing.T) {
	s := setupTown(t)

	convoyID, _ := s.CreateConvoy("test-convoy", "operator")
	s.AddConvoyItem(convoyID, "gt-11111111", "myrig")
	s.AddConvoyItem(convoyID, "gt-22222222", "myrig")

	// Remove one.
	if err := s.RemoveConvoyItem(convoyID, "gt-11111111"); err != nil {
		t.Fatal(err)
	}

	items, err := s.ListConvoyItems(convoyID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item after remove, got %d", len(items))
	}
	if items[0].WorkItemID != "gt-22222222" {
		t.Fatalf("expected remaining item gt-22222222, got %s", items[0].WorkItemID)
	}
}

func TestListConvoys(t *testing.T) {
	s := setupTown(t)

	s.CreateConvoy("convoy-1", "operator")
	s.CreateConvoy("convoy-2", "operator")
	id3, _ := s.CreateConvoy("convoy-3", "operator")

	// List all → 3.
	all, err := s.ListConvoys("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 convoys, got %d", len(all))
	}

	// List by status=open → 3.
	open, err := s.ListConvoys("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("expected 3 open convoys, got %d", len(open))
	}

	// Close one → list open → 2.
	s.UpdateConvoyStatus(id3, "closed")
	open, err = s.ListConvoys("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("expected 2 open convoys, got %d", len(open))
	}
}

func TestUpdateConvoyStatus(t *testing.T) {
	s := setupTown(t)

	id, _ := s.CreateConvoy("test-convoy", "operator")

	// Update to "closed" → sets closed_at.
	if err := s.UpdateConvoyStatus(id, "closed"); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetConvoy(id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "closed" {
		t.Fatalf("expected status %q, got %q", "closed", c.Status)
	}
	if c.ClosedAt == nil {
		t.Fatal("expected closed_at to be set")
	}
}

func TestCheckConvoyReadiness(t *testing.T) {
	// Set up GT_HOME for both town and rig stores.
	dir := t.TempDir()
	os.Setenv("GT_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("GT_HOME") })
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	townStore, err := OpenTown()
	if err != nil {
		t.Fatal(err)
	}
	defer townStore.Close()

	// Create a rig store with work items.
	rigStore, err := OpenRig("testrig")
	if err != nil {
		t.Fatal(err)
	}

	idA, _ := rigStore.CreateWorkItem("Item A", "", "operator", 2, nil)
	idB, _ := rigStore.CreateWorkItem("Item B", "", "operator", 2, nil)
	idC, _ := rigStore.CreateWorkItem("Item C", "", "operator", 2, nil)

	// A depends on B; C has no deps.
	rigStore.AddDependency(idA, idB)
	rigStore.Close()

	// Create convoy with all 3 items.
	convoyID, _ := townStore.CreateConvoy("test-convoy", "operator")
	townStore.AddConvoyItem(convoyID, idA, "testrig")
	townStore.AddConvoyItem(convoyID, idB, "testrig")
	townStore.AddConvoyItem(convoyID, idC, "testrig")

	// Check readiness: B and C should be ready, A should not.
	statuses, err := townStore.CheckConvoyReadiness(convoyID, OpenRig)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	statusMap := map[string]ConvoyItemStatus{}
	for _, st := range statuses {
		statusMap[st.WorkItemID] = st
	}

	// A depends on B (open) → not ready.
	if statusMap[idA].Ready {
		t.Fatalf("expected item A to not be ready (depends on open B)")
	}

	// B has no deps → ready.
	if !statusMap[idB].Ready {
		t.Fatalf("expected item B to be ready (no deps)")
	}

	// C has no deps → ready.
	if !statusMap[idC].Ready {
		t.Fatalf("expected item C to be ready (no deps)")
	}

	// Mark B as done → A should now be ready.
	rigStore2, err := OpenRig("testrig")
	if err != nil {
		t.Fatal(err)
	}
	rigStore2.UpdateWorkItem(idB, WorkItemUpdates{Status: "done"})
	rigStore2.Close()

	statuses2, err := townStore.CheckConvoyReadiness(convoyID, OpenRig)
	if err != nil {
		t.Fatal(err)
	}
	statusMap2 := map[string]ConvoyItemStatus{}
	for _, st := range statuses2 {
		statusMap2[st.WorkItemID] = st
	}
	if !statusMap2[idA].Ready {
		t.Fatalf("expected item A to be ready after B is done")
	}
}

func TestTryCloseConvoy(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("GT_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("GT_HOME") })
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	townStore, err := OpenTown()
	if err != nil {
		t.Fatal(err)
	}
	defer townStore.Close()

	// Create rig store with 2 items.
	rigStore, err := OpenRig("testrig")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := rigStore.CreateWorkItem("Item A", "", "operator", 2, nil)
	idB, _ := rigStore.CreateWorkItem("Item B", "", "operator", 2, nil)
	rigStore.Close()

	convoyID, _ := townStore.CreateConvoy("test-convoy", "operator")
	townStore.AddConvoyItem(convoyID, idA, "testrig")
	townStore.AddConvoyItem(convoyID, idB, "testrig")

	// Some items open → convoy stays open.
	closed, err := townStore.TryCloseConvoy(convoyID, OpenRig)
	if err != nil {
		t.Fatal(err)
	}
	if closed {
		t.Fatal("expected convoy to not be closed (items still open)")
	}

	// Mark all items done/closed.
	rigStore2, err := OpenRig("testrig")
	if err != nil {
		t.Fatal(err)
	}
	rigStore2.UpdateWorkItem(idA, WorkItemUpdates{Status: "done"})
	rigStore2.CloseWorkItem(idB)
	rigStore2.Close()

	// All done → convoy auto-closed.
	closed, err = townStore.TryCloseConvoy(convoyID, OpenRig)
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("expected convoy to be closed (all items done/closed)")
	}

	// Verify convoy status.
	c, err := townStore.GetConvoy(convoyID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "closed" {
		t.Fatalf("expected convoy status %q, got %q", "closed", c.Status)
	}
	if c.ClosedAt == nil {
		t.Fatal("expected closed_at to be set")
	}
}

func TestTownSchemaV3(t *testing.T) {
	s := setupTown(t)

	// Verify the schema version is 3.
	var v int
	if err := s.DB().QueryRow("SELECT version FROM schema_version").Scan(&v); err != nil {
		t.Fatalf("failed to get schema version: %v", err)
	}
	if v != 3 {
		t.Errorf("schema version = %d, want 3", v)
	}

	// Verify convoy tables exist.
	for _, table := range []string{"convoys", "convoy_items"} {
		var count int
		err := s.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected table %s, got count=%d", table, count)
		}
	}
}
