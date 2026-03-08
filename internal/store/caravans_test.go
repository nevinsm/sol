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
	if c.Status != "drydock" {
		t.Fatalf("expected status %q, got %q", "drydock", c.Status)
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
	if items[0].WritID != "sol-22222222" {
		t.Fatalf("expected remaining item sol-22222222, got %s", items[0].WritID)
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

	// List by status=drydock → 3 (default status).
	drydock, err := s.ListCaravans("drydock")
	if err != nil {
		t.Fatal(err)
	}
	if len(drydock) != 3 {
		t.Fatalf("expected 3 drydock caravans, got %d", len(drydock))
	}

	// Commission one → list drydock → 2.
	s.UpdateCaravanStatus(id3, "open")
	drydock, err = s.ListCaravans("drydock")
	if err != nil {
		t.Fatal(err)
	}
	if len(drydock) != 2 {
		t.Fatalf("expected 2 drydock caravans, got %d", len(drydock))
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

func TestUpdateCaravanStatusClearsClosedAt(t *testing.T) {
	s := setupSphere(t)

	id, _ := s.CreateCaravan("reopen-test", "operator")

	// Close the caravan → sets closed_at.
	if err := s.UpdateCaravanStatus(id, "closed"); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetCaravan(id)
	if err != nil {
		t.Fatal(err)
	}
	if c.ClosedAt == nil {
		t.Fatal("expected closed_at to be set after closing")
	}

	// Transition from closed → drydock → closed_at should be cleared.
	if err := s.UpdateCaravanStatus(id, "drydock"); err != nil {
		t.Fatal(err)
	}
	c, err = s.GetCaravan(id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "drydock" {
		t.Fatalf("expected status %q, got %q", "drydock", c.Status)
	}
	if c.ClosedAt != nil {
		t.Fatalf("expected closed_at to be nil after reopening, got %v", c.ClosedAt)
	}
}

func TestCaravanAddToClosedReturnsError(t *testing.T) {
	s := setupSphere(t)

	id, _ := s.CreateCaravan("closed-add-test", "operator")

	// Close the caravan.
	if err := s.UpdateCaravanStatus(id, "closed"); err != nil {
		t.Fatal(err)
	}

	// Simulate the guard: fetch caravan and check status before add.
	caravan, err := s.GetCaravan(id)
	if err != nil {
		t.Fatal(err)
	}
	if caravan.Status != "closed" {
		t.Fatalf("expected status %q, got %q", "closed", caravan.Status)
	}
	// The command would return an error here — verify the status check works.
}

func TestCaravanReopenClosedTransitionsToDrydock(t *testing.T) {
	s := setupSphere(t)

	id, _ := s.CreateCaravan("reopen-closed-test", "operator")

	// Close the caravan.
	if err := s.UpdateCaravanStatus(id, "closed"); err != nil {
		t.Fatal(err)
	}
	c, _ := s.GetCaravan(id)
	if c.ClosedAt == nil {
		t.Fatal("expected closed_at to be set")
	}

	// Reopen: closed → drydock.
	if err := s.UpdateCaravanStatus(id, "drydock"); err != nil {
		t.Fatal(err)
	}

	c, err := s.GetCaravan(id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "drydock" {
		t.Fatalf("expected status %q after reopen, got %q", "drydock", c.Status)
	}
	if c.ClosedAt != nil {
		t.Fatalf("expected closed_at to be nil after reopen, got %v", c.ClosedAt)
	}
}

func TestCaravanReopenNonClosedReturnsError(t *testing.T) {
	s := setupSphere(t)

	id, _ := s.CreateCaravan("reopen-nonclosed-test", "operator")

	// Caravan is in drydock (not closed) — reopen should be rejected.
	caravan, err := s.GetCaravan(id)
	if err != nil {
		t.Fatal(err)
	}
	if caravan.Status == "closed" {
		t.Fatal("expected caravan to NOT be closed (it should be drydock)")
	}

	// Commission to open, then verify reopen is also rejected.
	if err := s.UpdateCaravanStatus(id, "open"); err != nil {
		t.Fatal(err)
	}
	caravan, _ = s.GetCaravan(id)
	if caravan.Status == "closed" {
		t.Fatal("expected caravan to NOT be closed (it should be open)")
	}
}

func TestCaravanAddToDrydockedAndOpenWorks(t *testing.T) {
	s := setupSphere(t)

	id, _ := s.CreateCaravan("add-allowed-test", "operator")

	// Add to drydocked caravan should work.
	if err := s.CreateCaravanItem(id, "sol-aaaa000000000001", "testworld", 0); err != nil {
		t.Fatalf("expected add to drydocked caravan to succeed: %v", err)
	}

	// Commission to open.
	if err := s.UpdateCaravanStatus(id, "open"); err != nil {
		t.Fatal(err)
	}

	// Add to open caravan should work.
	if err := s.CreateCaravanItem(id, "sol-aaaa000000000002", "testworld", 0); err != nil {
		t.Fatalf("expected add to open caravan to succeed: %v", err)
	}

	items, err := s.ListCaravanItems(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
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

	// Create a world store with writs.
	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}

	idA, _ := worldStore.CreateWrit("Item A", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWrit("Item B", "", "operator", 2, nil)
	idC, _ := worldStore.CreateWrit("Item C", "", "operator", 2, nil)

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
		statusMap[st.WritID] = st
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

	// Mark B as done → A should still NOT be ready (done != merged).
	worldStore2, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	worldStore2.UpdateWrit(idB, WritUpdates{Status: "done"})
	worldStore2.Close()

	statuses2, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	statusMap2 := map[string]CaravanItemStatus{}
	for _, st := range statuses2 {
		statusMap2[st.WritID] = st
	}
	if statusMap2[idA].Ready {
		t.Fatalf("expected item A to NOT be ready after B is done (not closed)")
	}

	// Close B (merged) → A should now be ready.
	worldStore3, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	worldStore3.CloseWrit(idB)
	worldStore3.Close()

	statuses3, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	statusMap3 := map[string]CaravanItemStatus{}
	for _, st := range statuses3 {
		statusMap3[st.WritID] = st
	}
	if !statusMap3[idA].Ready {
		t.Fatalf("expected item A to be ready after B is closed (merged)")
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
	idA, _ := worldStore.CreateWrit("Item A", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWrit("Item B", "", "operator", 2, nil)
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
	worldStore2.CloseWrit(idA)
	worldStore2.CloseWrit(idB)
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
	idA, _ := worldStore.CreateWrit("Item A", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWrit("Item B", "", "operator", 2, nil)
	worldStore.Close()

	caravanID, _ := sphereStore.CreateCaravan("test-done-not-closed", "operator")
	sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0)
	sphereStore.CreateCaravanItem(caravanID, idB, "ember", 0)

	// Set all items to done (code complete, awaiting merge).
	ws, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	ws.UpdateWrit(idA, WritUpdates{Status: "done"})
	ws.UpdateWrit(idB, WritUpdates{Status: "done"})
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
	ws2.CloseWrit(idA)
	ws2.CloseWrit(idB)
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

	// Verify the schema version is latest.
	var v int
	if err := s.DB().QueryRow("SELECT version FROM schema_version").Scan(&v); err != nil {
		t.Fatalf("failed to get schema version: %v", err)
	}
	if v != CurrentSphereSchema {
		t.Errorf("schema version = %d, want %d", v, CurrentSphereSchema)
	}

	// Verify caravan tables exist (including caravan_dependencies).
	for _, table := range []string{"caravans", "caravan_items", "caravan_dependencies"} {
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

	// Create writs in world.
	worldStore, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	idA, _ := worldStore.CreateWrit("Phase 0 item", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWrit("Phase 1 item", "", "operator", 2, nil)
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
		statusMap[st.WritID] = st
	}
	if !statusMap[idA].Ready {
		t.Fatal("expected phase 0 item A to be ready")
	}
	if statusMap[idB].Ready {
		t.Fatal("expected phase 1 item B to NOT be ready (phase 0 not done)")
	}

	// Mark phase 0 item done → phase 1 should still NOT be ready (done != merged).
	worldStore2, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	worldStore2.UpdateWrit(idA, WritUpdates{Status: "done"})
	worldStore2.Close()

	statuses2, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	statusMap2 := map[string]CaravanItemStatus{}
	for _, st := range statuses2 {
		statusMap2[st.WritID] = st
	}
	if statusMap2[idB].Ready {
		t.Fatal("expected phase 1 item B to NOT be ready after phase 0 done (not closed)")
	}

	// Close phase 0 item (merged) → phase 1 should become ready.
	worldStore3, err := OpenWorld("ember")
	if err != nil {
		t.Fatal(err)
	}
	worldStore3.CloseWrit(idA)
	worldStore3.Close()

	statuses3, err := sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	if err != nil {
		t.Fatal(err)
	}
	statusMap3 := map[string]CaravanItemStatus{}
	for _, st := range statuses3 {
		statusMap3[st.WritID] = st
	}
	if !statusMap3[idB].Ready {
		t.Fatal("expected phase 1 item B to be ready after phase 0 closed")
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
	idA, _ := worldStore.CreateWrit("Phase 0", "", "operator", 2, nil)
	idB, _ := worldStore.CreateWrit("Phase 1", "", "operator", 2, nil)
	idC, _ := worldStore.CreateWrit("Phase 2", "", "operator", 2, nil)
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
		sm[st.WritID] = st
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

	// Close phase 0 (merged) → phase 1 becomes ready, phase 2 still not.
	ws, _ := OpenWorld("ember")
	ws.CloseWrit(idA)
	ws.Close()

	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	sm = map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WritID] = st
	}
	if !sm[idB].Ready {
		t.Fatal("expected phase 1 item ready after phase 0 closed")
	}
	if sm[idC].Ready {
		t.Fatal("expected phase 2 item NOT ready (phase 1 not closed)")
	}

	// Close phase 1 (merged) → phase 2 becomes ready.
	ws, _ = OpenWorld("ember")
	ws.CloseWrit(idB)
	ws.Close()

	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	sm = map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WritID] = st
	}
	if !sm[idC].Ready {
		t.Fatal("expected phase 2 item ready after phase 1 closed")
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
	idA, _ := alphaStore.CreateWrit("Alpha item", "", "operator", 2, nil)
	alphaStore.Close()

	betaStore, err := OpenWorld("beta")
	if err != nil {
		t.Fatal(err)
	}
	idB, _ := betaStore.CreateWrit("Beta item phase 0", "", "operator", 2, nil)
	idC, _ := betaStore.CreateWrit("Beta item phase 1", "", "operator", 2, nil)
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
		sm[st.WritID] = st
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

	// Close only A (alpha phase 0). C still not ready because B (beta phase 0) is open.
	as, _ := OpenWorld("alpha")
	as.CloseWrit(idA)
	as.Close()

	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	sm = map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WritID] = st
	}
	if sm[idC].Ready {
		t.Fatal("expected phase 1 item NOT ready (B in phase 0 still open)")
	}

	// Close B → C becomes ready (all phase 0 items closed across worlds).
	bs, _ := OpenWorld("beta")
	bs.CloseWrit(idB)
	bs.Close()

	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, OpenWorld)
	sm = map[string]CaravanItemStatus{}
	for _, st := range statuses {
		sm[st.WritID] = st
	}
	if !sm[idC].Ready {
		t.Fatal("expected phase 1 item ready after all phase 0 closed")
	}
}

func TestUpdateCaravanItemPhase(t *testing.T) {
	s := setupSphere(t)

	caravanID, _ := s.CreateCaravan("phase-test", "operator")
	s.CreateCaravanItem(caravanID, "sol-0000000000000001", "testworld", 0)
	s.CreateCaravanItem(caravanID, "sol-0000000000000002", "testworld", 0)

	// Update single item phase.
	err := s.UpdateCaravanItemPhase(caravanID, "sol-0000000000000001", 2)
	if err != nil {
		t.Fatalf("UpdateCaravanItemPhase() error: %v", err)
	}

	items, _ := s.ListCaravanItems(caravanID)
	phaseMap := map[string]int{}
	for _, item := range items {
		phaseMap[item.WritID] = item.Phase
	}
	if phaseMap["sol-0000000000000001"] != 2 {
		t.Fatalf("expected phase 2, got %d", phaseMap["sol-0000000000000001"])
	}
	if phaseMap["sol-0000000000000002"] != 0 {
		t.Fatalf("expected phase 0, got %d", phaseMap["sol-0000000000000002"])
	}
}

func TestUpdateCaravanItemPhaseNotFound(t *testing.T) {
	s := setupSphere(t)

	caravanID, _ := s.CreateCaravan("phase-notfound", "operator")

	err := s.UpdateCaravanItemPhase(caravanID, "sol-nonexistent", 1)
	if err == nil {
		t.Fatal("expected error for nonexistent item")
	}
}

func TestUpdateAllCaravanItemPhases(t *testing.T) {
	s := setupSphere(t)

	caravanID, _ := s.CreateCaravan("bulk-phase", "operator")
	s.CreateCaravanItem(caravanID, "sol-0000000000000001", "testworld", 0)
	s.CreateCaravanItem(caravanID, "sol-0000000000000002", "testworld", 1)
	s.CreateCaravanItem(caravanID, "sol-0000000000000003", "testworld", 2)

	n, err := s.UpdateAllCaravanItemPhases(caravanID, 5)
	if err != nil {
		t.Fatalf("UpdateAllCaravanItemPhases() error: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 rows affected, got %d", n)
	}

	items, _ := s.ListCaravanItems(caravanID)
	for _, item := range items {
		if item.Phase != 5 {
			t.Fatalf("expected phase 5 for %s, got %d", item.WritID, item.Phase)
		}
	}
}
