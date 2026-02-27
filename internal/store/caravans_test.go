package store

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestCreateCaravan(t *testing.T) {
	s := setupSphere(t)

	id, err := s.CreateCaravan("auth-feature", "operator")
	if err != nil {
		t.Fatalf("CreateCaravan() error: %v", err)
	}

	// Verify ID format.
	pattern := regexp.MustCompile(`^car-[0-9a-f]{8}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("caravan ID %q does not match pattern car-[0-9a-f]{8}", id)
	}

	// Verify with GetCaravan.
	c, err := s.GetCaravan(id)
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

func TestGetCaravanNotFound(t *testing.T) {
	s := setupSphere(t)

	_, err := s.GetCaravan("caravan-nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent caravan")
	}
}

func TestAddCaravanItem(t *testing.T) {
	s := setupSphere(t)

	caravanID, _ := s.CreateCaravan("test-caravan", "operator")

	// Add 3 items.
	s.AddCaravanItem(caravanID, "sol-11111111", "haven")
	s.AddCaravanItem(caravanID, "sol-22222222", "haven")
	s.AddCaravanItem(caravanID, "sol-33333333", "haven")

	items, err := s.ListCaravanItems(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
}

func TestRemoveCaravanItem(t *testing.T) {
	s := setupSphere(t)

	caravanID, _ := s.CreateCaravan("test-caravan", "operator")
	s.AddCaravanItem(caravanID, "sol-11111111", "haven")
	s.AddCaravanItem(caravanID, "sol-22222222", "haven")

	// Remove one.
	if err := s.RemoveCaravanItem(caravanID, "sol-11111111"); err != nil {
		t.Fatal(err)
	}

	items, err := s.ListCaravanItems(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item after remove, got %d", len(items))
	}
	if items[0].WorkItemID != "sol-22222222" {
		t.Fatalf("expected remaining item sol-22222222, got %s", items[0].WorkItemID)
	}
}

func TestListCaravans(t *testing.T) {
	s := setupSphere(t)

	s.CreateCaravan("caravan-1", "operator")
	s.CreateCaravan("caravan-2", "operator")
	id3, _ := s.CreateCaravan("caravan-3", "operator")

	// List all → 3.
	all, err := s.ListCaravans("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 caravans, got %d", len(all))
	}

	// List by status=open → 3.
	open, err := s.ListCaravans("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("expected 3 open caravans, got %d", len(open))
	}

	// Close one → list open → 2.
	s.UpdateCaravanStatus(id3, "closed")
	open, err = s.ListCaravans("open")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("expected 2 open caravans, got %d", len(open))
	}
}

func TestUpdateCaravanStatus(t *testing.T) {
	s := setupSphere(t)

	id, _ := s.CreateCaravan("test-caravan", "operator")

	// Update to "closed" → sets closed_at.
	if err := s.UpdateCaravanStatus(id, "closed"); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetCaravan(id)
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

func TestCheckCaravanReadiness(t *testing.T) {
	// Set up SOL_HOME for both sphere and world stores.
	dir := t.TempDir()
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	// Create a world store with work items.
	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}

	idA, _ := worldStore.CreateWorkItem("Item A", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWorkItem("Item B", "", "operator", 2, nil)
	idC, _ := worldStore.CreateWorkItem("Item C", "", "operator", 2, nil)

	// A depends on B; C has no deps.
	worldStore.AddDependency(idA, idB)
	worldStore.Close()

	// Create caravan with all 3 items.
	caravanID, _ := sphereStore.CreateCaravan("test-caravan", "operator")
	sphereStore.AddCaravanItem(caravanID, idA, "ember")
	sphereStore.AddCaravanItem(caravanID, idB, "ember")
	sphereStore.AddCaravanItem(caravanID, idC, "ember")

	// Check readiness: B and C should be ready, A should not.
	statuses, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	statusMap := map[string]CaravanItemStatus{}
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
	worldStore2, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	worldStore2.UpdateWorkItem(idB, WorkItemUpdates{Status: "done"})
	worldStore2.Close()

	statuses2, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	statusMap2 := map[string]CaravanItemStatus{}
	for _, st := range statuses2 {
		statusMap2[st.WorkItemID] = st
	}
	if !statusMap2[idA].Ready {
		t.Fatalf("expected item A to be ready after B is done")
	}
}

func TestTryCloseCaravan(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	// Create world store with 2 items.
	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := worldStore.CreateWorkItem("Item A", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWorkItem("Item B", "", "operator", 2, nil)
	worldStore.Close()

	caravanID, _ := sphereStore.CreateCaravan("test-caravan", "operator")
	sphereStore.AddCaravanItem(caravanID, idA, "ember")
	sphereStore.AddCaravanItem(caravanID, idB, "ember")

	// Some items open → caravan stays open.
	closed, err := sphereStore.TryCloseCaravan(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if closed {
		t.Fatal("expected caravan to not be closed (items still open)")
	}

	// Mark all items done/closed.
	worldStore2, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	worldStore2.UpdateWorkItem(idA, WorkItemUpdates{Status: "done"})
	worldStore2.CloseWorkItem(idB)
	worldStore2.Close()

	// All done → caravan auto-closed.
	closed, err = sphereStore.TryCloseCaravan(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("expected caravan to be closed (all items done/closed)")
	}

	// Verify caravan status.
	c, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "closed" {
		t.Fatalf("expected caravan status %q, got %q", "closed", c.Status)
	}
	if c.ClosedAt == nil {
		t.Fatal("expected closed_at to be set")
	}
}

func TestSphereSchemaV4(t *testing.T) {
	s := setupSphere(t)

	// Verify the schema version is 5.
	var v int
	if err := s.DB().QueryRow("SELECT version FROM schema_version").Scan(&v); err != nil {
		t.Fatalf("failed to get schema version: %v", err)
	}
	if v != 5 {
		t.Errorf("schema version = %d, want 5", v)
	}

	// Verify caravan tables exist.
	for _, table := range []string{"caravans", "caravan_items"} {
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
