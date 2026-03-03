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
	pattern := regexp.MustCompile(`^car-[0-9a-f]{16}$`)
	if !pattern.MatchString(id) {
		t.Fatalf("caravan ID %q does not match pattern car-[0-9a-f]{16}", id)
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

func TestCreateCaravanItem(t *testing.T) {
	s := setupSphere(t)

	caravanID, _ := s.CreateCaravan("test-caravan", "operator")

	// Add 3 items.
	s.CreateCaravanItem(caravanID, "sol-11111111", "haven", 0)
	s.CreateCaravanItem(caravanID, "sol-22222222", "haven", 0)
	s.CreateCaravanItem(caravanID, "sol-33333333", "haven", 0)

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
	s.CreateCaravanItem(caravanID, "sol-11111111", "haven", 0)
	s.CreateCaravanItem(caravanID, "sol-22222222", "haven", 0)

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
	t.Setenv("SOL_HOME", dir)
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
	sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0)
	sphereStore.CreateCaravanItem(caravanID, idB, "ember", 0)
	sphereStore.CreateCaravanItem(caravanID, idC, "ember", 0)

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
	t.Setenv("SOL_HOME", dir)
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
	sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0)
	sphereStore.CreateCaravanItem(caravanID, idB, "ember", 0)

	// Some items open → caravan stays open.
	closed, err := sphereStore.TryCloseCaravan(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if closed {
		t.Fatal("expected caravan to not be closed (items still open)")
	}

	// Mark all items closed (merged).
	worldStore2, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	worldStore2.CloseWorkItem(idA)
	worldStore2.CloseWorkItem(idB)
	worldStore2.Close()

	// All closed → caravan auto-closed.
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

func TestTryCloseCaravanDoneNotSufficient(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := worldStore.CreateWorkItem("Item A", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWorkItem("Item B", "", "operator", 2, nil)
	worldStore.Close()

	caravanID, _ := sphereStore.CreateCaravan("test-done-not-closed", "operator")
	sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0)
	sphereStore.CreateCaravanItem(caravanID, idB, "ember", 0)

	// Set all items to done (code complete, awaiting merge).
	ws, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	ws.UpdateWorkItem(idA, WorkItemUpdates{Status: "done"})
	ws.UpdateWorkItem(idB, WorkItemUpdates{Status: "done"})
	ws.Close()

	// done is NOT sufficient to close caravan.
	closed, err := sphereStore.TryCloseCaravan(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if closed {
		t.Fatal("expected caravan to NOT close when items are done (not closed/merged)")
	}

	// Now close (merge) all items.
	ws2, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	ws2.CloseWorkItem(idA)
	ws2.CloseWorkItem(idB)
	ws2.Close()

	// Now caravan should close.
	closed, err = sphereStore.TryCloseCaravan(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("expected caravan to close when all items are closed (merged)")
	}
}

func TestSphereSchemaV4(t *testing.T) {
	s := setupSphere(t)

	// Verify the schema version is 7.
	var v int
	if err := s.DB().QueryRow("SELECT version FROM schema_version").Scan(&v); err != nil {
		t.Fatalf("failed to get schema version: %v", err)
	}
	if v != 7 {
		t.Errorf("schema version = %d, want 7", v)
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

func TestCaravanPhaseDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	s, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	caravanID, _ := s.CreateCaravan("phase-test", "operator")
	s.CreateCaravanItem(caravanID, "sol-11111111", "haven", 0)

	items, err := s.ListCaravanItems(caravanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Phase != 0 {
		t.Fatalf("expected phase 0, got %d", items[0].Phase)
	}
}

func TestCaravanPhaseReadiness(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	// Create work items in world.
	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := worldStore.CreateWorkItem("Phase 0 item", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWorkItem("Phase 1 item", "", "operator", 2, nil)
	worldStore.Close()

	// Create caravan: A in phase 0, B in phase 1.
	caravanID, _ := sphereStore.CreateCaravan("phase-readiness", "operator")
	sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0)
	sphereStore.CreateCaravanItem(caravanID, idB, "ember", 1)

	// Phase 0 item should be ready, phase 1 should NOT.
	statuses, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	statusMap := map[string]CaravanItemStatus{}
	for _, st := range statuses {
		statusMap[st.WorkItemID] = st
	}
	if !statusMap[idA].Ready {
		t.Fatal("expected phase 0 item A to be ready")
	}
	if statusMap[idB].Ready {
		t.Fatal("expected phase 1 item B to NOT be ready (phase 0 not done)")
	}

	// Mark phase 0 item done → phase 1 should become ready.
	worldStore2, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	worldStore2.UpdateWorkItem(idA, WorkItemUpdates{Status: "done"})
	worldStore2.Close()

	statuses2, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	statusMap2 := map[string]CaravanItemStatus{}
	for _, st := range statuses2 {
		statusMap2[st.WorkItemID] = st
	}
	if !statusMap2[idB].Ready {
		t.Fatal("expected phase 1 item B to be ready after phase 0 done")
	}
}

func TestCaravanPhaseMultiple(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := worldStore.CreateWorkItem("Phase 0", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWorkItem("Phase 1", "", "operator", 2, nil)
	idC, _ := worldStore.CreateWorkItem("Phase 2", "", "operator", 2, nil)
	worldStore.Close()

	caravanID, _ := sphereStore.CreateCaravan("multi-phase", "operator")
	sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0)
	sphereStore.CreateCaravanItem(caravanID, idB, "ember", 1)
	sphereStore.CreateCaravanItem(caravanID, idC, "ember", 2)

	// Only phase 0 ready initially.
	statuses, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	sm := map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WorkItemID] = st
	}
	if !sm[idA].Ready {
		t.Fatal("expected phase 0 item ready")
	}
	if sm[idB].Ready {
		t.Fatal("expected phase 1 item NOT ready")
	}
	if sm[idC].Ready {
		t.Fatal("expected phase 2 item NOT ready")
	}

	// Complete phase 0 → phase 1 becomes ready, phase 2 still not.
	ws, _ := OpenWorld("ember")
	ws.UpdateWorkItem(idA, WorkItemUpdates{Status: "done"})
	ws.Close()

	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	sm = map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WorkItemID] = st
	}
	if !sm[idB].Ready {
		t.Fatal("expected phase 1 item ready after phase 0 done")
	}
	if sm[idC].Ready {
		t.Fatal("expected phase 2 item NOT ready (phase 1 not done)")
	}

	// Complete phase 1 → phase 2 becomes ready.
	ws, _ = OpenWorld("ember")
	ws.UpdateWorkItem(idB, WorkItemUpdates{Status: "done"})
	ws.Close()

	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	sm = map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WorkItemID] = st
	}
	if !sm[idC].Ready {
		t.Fatal("expected phase 2 item ready after phase 1 done")
	}
}

func TestCaravanPhaseMixedWorlds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	sphereStore, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	// Create items in two different worlds.
	alphaStore, err := OpenWorld("alpha")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := alphaStore.CreateWorkItem("Alpha item", "", "operator", 2, nil)
	alphaStore.Close()

	betaStore, err := OpenWorld("beta")
	if err != nil {
		t.Fatal(err)
	}
	idB, _ := betaStore.CreateWorkItem("Beta item phase 0", "", "operator", 2, nil)
	idC, _ := betaStore.CreateWorkItem("Beta item phase 1", "", "operator", 2, nil)
	betaStore.Close()

	// A (alpha, phase 0), B (beta, phase 0), C (beta, phase 1).
	caravanID, _ := sphereStore.CreateCaravan("mixed-worlds", "operator")
	sphereStore.CreateCaravanItem(caravanID, idA, "alpha", 0)
	sphereStore.CreateCaravanItem(caravanID, idB, "beta", 0)
	sphereStore.CreateCaravanItem(caravanID, idC, "beta", 1)

	// Phase 0 items (A, B) ready; phase 1 item (C) not ready.
	statuses, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	sm := map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WorkItemID] = st
	}
	if !sm[idA].Ready {
		t.Fatal("expected alpha phase 0 item ready")
	}
	if !sm[idB].Ready {
		t.Fatal("expected beta phase 0 item ready")
	}
	if sm[idC].Ready {
		t.Fatal("expected beta phase 1 item NOT ready")
	}

	// Complete only A (alpha phase 0). C still not ready because B (beta phase 0) is open.
	as, _ := OpenWorld("alpha")
	as.UpdateWorkItem(idA, WorkItemUpdates{Status: "done"})
	as.Close()

	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	sm = map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WorkItemID] = st
	}
	if sm[idC].Ready {
		t.Fatal("expected phase 1 item NOT ready (B in phase 0 still open)")
	}

	// Complete B → C becomes ready (all phase 0 items done across worlds).
	bs, _ := OpenWorld("beta")
	bs.UpdateWorkItem(idB, WorkItemUpdates{Status: "done"})
	bs.Close()

	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	sm = map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WorkItemID] = st
	}
	if !sm[idC].Ready {
		t.Fatal("expected phase 1 item ready after all phase 0 done")
	}
}
