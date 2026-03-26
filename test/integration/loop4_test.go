package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

// --- Workflow Integration Tests ---

func TestCastWithGuidelines(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("GuidelinesBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("GL task", "Guidelines test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast — guidelines are auto-selected by kind (default for code writs).
	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "GuidelinesBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast with guidelines: %v", err)
	}

	if result.Guidelines != "default" {
		t.Errorf("result guidelines: got %q, want default", result.Guidelines)
	}

	// Verify .guidelines.md written to worktree.
	guidelinesPath := filepath.Join(result.WorktreeDir, ".guidelines.md")
	data, err := os.ReadFile(guidelinesPath)
	if err != nil {
		t.Fatalf("read .guidelines.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Execution Guidelines") {
		t.Error(".guidelines.md should contain execution guidelines header")
	}
}

func TestPrimeWithGuidelines(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("PrimeBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Prime GL task", "Prime guidelines test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast — auto-selects guidelines.
	if _, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "PrimeBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Call Prime.
	result, err := dispatch.Prime("ember", "PrimeBot", "outpost", worldStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Verify output contains guidelines section.
	if !strings.Contains(result.Output, "--- GUIDELINES ---") {
		t.Error("prime output should contain guidelines section")
	}
	if !strings.Contains(result.Output, "Execution Guidelines") {
		t.Error("prime output should contain guidelines content")
	}
	if !strings.Contains(result.Output, "--- END GUIDELINES ---") {
		t.Error("prime output should contain end guidelines marker")
	}

	// Should not contain old workflow references.
	if strings.Contains(result.Output, "sol workflow advance") {
		t.Error("prime output should not contain workflow advance")
	}
}

func TestPrimeGuidelinesInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("PlainBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Plain task", "Guidelines injection test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	if _, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "PlainBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Call Prime.
	result, err := dispatch.Prime("ember", "PlainBot", "outpost", worldStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Should contain guidelines section (auto-selected default template).
	if !strings.Contains(result.Output, "--- GUIDELINES ---") {
		t.Error("prime output should contain guidelines section")
	}

	// Should NOT contain old workflow references.
	if strings.Contains(result.Output, "sol workflow advance") {
		t.Error("prime output should not contain workflow commands")
	}

	// Should contain standard markers.
	if !strings.Contains(result.Output, "=== WORK CONTEXT ===") {
		t.Error("prime output should contain work context header")
	}
	if !strings.Contains(result.Output, itemID) {
		t.Error("prime output should contain writ ID")
	}
}

func TestResolveWithGuidelinesCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("DoneBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Done GL task", "Done guidelines test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast.
	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "DoneBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Verify .guidelines.md exists.
	guidelinesPath := filepath.Join(result.WorktreeDir, ".guidelines.md")
	if _, err := os.Stat(guidelinesPath); os.IsNotExist(err) {
		t.Fatal(".guidelines.md should exist after cast")
	}

	// Simulate agent work in worktree.
	if err := os.WriteFile(filepath.Join(result.WorktreeDir, "work.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}

	// Call Resolve.
	_, err = dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:     "ember",
		AgentName: "DoneBot",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

// --- Caravan Integration Tests ---

func TestCaravanCreateAndCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create writs and deps in world store, then close it.
	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	idA, err := worldStore.CreateWrit("Task A", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit A: %v", err)
	}
	idB, err := worldStore.CreateWrit("Task B", "Second task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit B: %v", err)
	}
	idC, err := worldStore.CreateWrit("Task C", "Depends on A and B", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit C: %v", err)
	}
	if err := worldStore.AddDependency(idC, idA); err != nil {
		t.Fatalf("AddDependency C→A: %v", err)
	}
	if err := worldStore.AddDependency(idC, idB); err != nil {
		t.Fatalf("AddDependency C→B: %v", err)
	}
	worldStore.Close()

	// Create caravan with all 3.
	caravanID, err := sphereStore.CreateCaravan("test-caravan", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem A: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idB, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem B: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idC, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem C: %v", err)
	}

	// Check readiness: A and B ready, C blocked.
	statuses, err := sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness: %v", err)
	}

	readyCount := 0
	blockedCount := 0
	for _, st := range statuses {
		if st.Ready {
			readyCount++
		} else {
			blockedCount++
		}
	}
	if readyCount != 2 {
		t.Errorf("ready count: got %d, want 2", readyCount)
	}
	if blockedCount != 1 {
		t.Errorf("blocked count: got %d, want 1", blockedCount)
	}

	// Close A (merged).
	rs, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	if _, err := rs.CloseWrit(idA); err != nil {
		t.Fatalf("close writ A: %v", err)
	}
	rs.Close()

	// Check again: B ready, C still blocked (B not closed).
	statuses, err = sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness after A closed: %v", err)
	}
	for _, st := range statuses {
		if st.WritID == idC && st.Ready {
			t.Error("C should still be blocked (B not closed)")
		}
	}

	// Close B (merged).
	rs, err = store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	if _, err := rs.CloseWrit(idB); err != nil {
		t.Fatalf("close writ B: %v", err)
	}
	rs.Close()

	// Check again: C now ready.
	statuses, err = sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness after B closed: %v", err)
	}
	for _, st := range statuses {
		if st.WritID == idC && !st.Ready {
			t.Error("C should be ready now (A and B closed)")
		}
	}
}

func TestCaravanAutoClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create 2 items, no deps.
	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	id1, err := worldStore.CreateWrit("Auto 1", "First", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit 1: %v", err)
	}
	id2, err := worldStore.CreateWrit("Auto 2", "Second", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit 2: %v", err)
	}

	// Mark both as closed.
	if err := worldStore.UpdateWrit(id1, store.WritUpdates{Status: "closed"}); err != nil {
		t.Fatalf("update writ 1: %v", err)
	}
	if err := worldStore.UpdateWrit(id2, store.WritUpdates{Status: "closed"}); err != nil {
		t.Fatalf("update writ 2: %v", err)
	}
	worldStore.Close()

	// Create caravan.
	caravanID, err := sphereStore.CreateCaravan("auto-close-test", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, id1, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem 1: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, id2, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem 2: %v", err)
	}

	// TryCloseCaravan → should return true.
	closed, err := sphereStore.TryCloseCaravan(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("TryCloseCaravan: %v", err)
	}
	if !closed {
		t.Error("caravan should auto-close when all items are closed (merged)")
	}

	// Verify caravan status.
	caravan, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan: %v", err)
	}
	if caravan.Status != "closed" {
		t.Errorf("caravan status: got %q, want closed", caravan.Status)
	}
	if caravan.ClosedAt == nil {
		t.Error("closed_at should be set")
	}
}

func TestCaravanMultiWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create writs in world "alpha".
	alphaStore, err := store.OpenWorld("alpha")
	if err != nil {
		t.Fatalf("open alpha world: %v", err)
	}
	idA, err := alphaStore.CreateWrit("Alpha task", "Task in alpha world", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit alpha: %v", err)
	}
	alphaStore.Close()

	// Create writs in world "beta".
	betaStore, err := store.OpenWorld("beta")
	if err != nil {
		t.Fatalf("open beta world: %v", err)
	}
	idB, err := betaStore.CreateWrit("Beta task 1", "First task in beta", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit beta 1: %v", err)
	}
	idC, err := betaStore.CreateWrit("Beta task 2", "Second task in beta", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit beta 2: %v", err)
	}
	// C depends on B within beta world.
	if err := betaStore.AddDependency(idC, idB); err != nil {
		t.Fatalf("AddDependency C→B: %v", err)
	}
	betaStore.Close()

	// Create caravan spanning both worlds.
	caravanID, err := sphereStore.CreateCaravan("multi-world-caravan", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idA, "alpha", 0); err != nil {
		t.Fatalf("AddCaravanItem alpha: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idB, "beta", 0); err != nil {
		t.Fatalf("AddCaravanItem beta B: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idC, "beta", 0); err != nil {
		t.Fatalf("AddCaravanItem beta C: %v", err)
	}

	// Check readiness: A ready (no deps), B ready (no deps), C blocked (depends on B).
	statuses, err := sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness: %v", err)
	}

	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	statusMap := map[string]store.CaravanItemStatus{}
	for _, st := range statuses {
		statusMap[st.WritID] = st
	}

	// A (alpha) should be ready.
	if st, ok := statusMap[idA]; !ok {
		t.Error("missing status for alpha item")
	} else {
		if st.World != "alpha" {
			t.Errorf("alpha item world: got %q, want alpha", st.World)
		}
		if !st.Ready {
			t.Error("alpha item should be ready (no deps)")
		}
	}

	// B (beta) should be ready.
	if st, ok := statusMap[idB]; !ok {
		t.Error("missing status for beta item B")
	} else if !st.Ready {
		t.Error("beta item B should be ready (no deps)")
	}

	// C (beta) should be blocked.
	if st, ok := statusMap[idC]; !ok {
		t.Error("missing status for beta item C")
	} else if st.Ready {
		t.Error("beta item C should be blocked (depends on B)")
	}

	// Close B (merged) → C should become ready.
	bs, err := store.OpenWorld("beta")
	if err != nil {
		t.Fatalf("open beta world: %v", err)
	}
	if _, err := bs.CloseWrit(idB); err != nil {
		t.Fatalf("close writ B: %v", err)
	}
	bs.Close()

	statuses, err = sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness after B closed: %v", err)
	}
	for _, st := range statuses {
		if st.WritID == idC && !st.Ready {
			t.Error("beta item C should be ready after B is closed")
		}
	}

	// Mark all items closed (merged) → caravan should auto-close.
	// Note: "done" (code complete) is NOT sufficient — items must be "closed" (merged).
	as, err := store.OpenWorld("alpha")
	if err != nil {
		t.Fatalf("open alpha world: %v", err)
	}
	if _, err := as.CloseWrit(idA); err != nil {
		t.Fatalf("close writ A: %v", err)
	}
	as.Close()

	bs, err = store.OpenWorld("beta")
	if err != nil {
		t.Fatalf("open beta world: %v", err)
	}
	if _, err := bs.CloseWrit(idB); err != nil {
		t.Fatalf("close writ B: %v", err)
	}
	if _, err := bs.CloseWrit(idC); err != nil {
		t.Fatalf("close writ C: %v", err)
	}
	bs.Close()

	closed, err := sphereStore.TryCloseCaravan(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("TryCloseCaravan: %v", err)
	}
	if !closed {
		t.Error("multi-world caravan should auto-close when all items closed (merged)")
	}
}

// --- Caravan Launch Integration Test ---

func TestCaravanLaunchDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()
	logger := events.NewLogger(solHome)

	// Create 3 writs: A and B are independent, C depends on A.
	idA, err := worldStore.CreateWrit("Task A", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit A: %v", err)
	}
	idB, err := worldStore.CreateWrit("Task B", "Second task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit B: %v", err)
	}
	idC, err := worldStore.CreateWrit("Task C", "Depends on A", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit C: %v", err)
	}
	if err := worldStore.AddDependency(idC, idA); err != nil {
		t.Fatalf("AddDependency C→A: %v", err)
	}

	// Create a caravan with all 3 items.
	caravanID, err := sphereStore.CreateCaravan("launch-test", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	for _, id := range []string{idA, idB, idC} {
		if err := sphereStore.CreateCaravanItem(caravanID, id, "ember", 0); err != nil {
			t.Fatalf("CreateCaravanItem %s: %v", id, err)
		}
	}

	// Pre-create 2 agents. C is blocked, so only A and B should dispatch.
	if _, err := sphereStore.CreateAgent("Alpha", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent Alpha: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Beta", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent Beta: %v", err)
	}

	// Check readiness: A and B ready, C blocked.
	statuses, err := sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness: %v", err)
	}
	readyCount := 0
	for _, st := range statuses {
		if st.WritStatus == "open" && st.Ready {
			readyCount++
		}
	}
	if readyCount != 2 {
		t.Fatalf("expected 2 ready items, got %d", readyCount)
	}

	// Dispatch ready items (simulates caravan launch logic).
	dispatched := 0
	for _, st := range statuses {
		if st.WritStatus != "open" || !st.Ready {
			continue
		}
		result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
			WritID: st.WritID,
			World:      "ember",
			SourceRepo: sourceRepo,
		}, worldStore, sphereStore, mgr, logger)
		if err != nil {
			t.Fatalf("Cast %s: %v", st.WritID, err)
		}
		if result.AgentName == "" {
			t.Errorf("Cast %s: empty agent name", st.WritID)
		}
		dispatched++
	}
	if dispatched != 2 {
		t.Errorf("dispatched: got %d, want 2", dispatched)
	}

	// Verify: 2 sessions started.
	mgr.mu.Lock()
	startedCount := len(mgr.started)
	mgr.mu.Unlock()
	if startedCount != 2 {
		t.Errorf("sessions started: got %d, want 2", startedCount)
	}

	// Verify: A and B are tethered, C is still open.
	itemA, _ := worldStore.GetWrit(idA)
	itemB, _ := worldStore.GetWrit(idB)
	itemC, _ := worldStore.GetWrit(idC)
	if itemA.Status != "tethered" {
		t.Errorf("item A status: got %q, want tethered", itemA.Status)
	}
	if itemB.Status != "tethered" {
		t.Errorf("item B status: got %q, want tethered", itemB.Status)
	}
	if itemC.Status != "open" {
		t.Errorf("item C status: got %q, want open", itemC.Status)
	}

	// Verify cast events emitted.
	assertEventEmitted(t, solHome, events.EventCast)
}

// --- End-to-End Workflow Test ---

func TestGuidelinesExplicitTemplate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("PropBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Investigation task", "Debug test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	logger := events.NewLogger(solHome)

	// Cast with explicit guidelines template.
	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "PropBot",
		SourceRepo: sourceRepo,
		Guidelines: "investigation",
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	if result.Guidelines != "investigation" {
		t.Errorf("guidelines: got %q, want investigation", result.Guidelines)
	}

	// Verify .guidelines.md contains investigation template.
	data, err := os.ReadFile(filepath.Join(result.WorktreeDir, ".guidelines.md"))
	if err != nil {
		t.Fatalf("read .guidelines.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Investigation") {
		t.Error(".guidelines.md should contain investigation content")
	}

	// Prime should include guidelines.
	primeResult, err := dispatch.Prime("ember", "PropBot", "outpost", worldStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}
	if !strings.Contains(primeResult.Output, "--- GUIDELINES ---") {
		t.Error("prime output should contain guidelines section")
	}
	if !strings.Contains(primeResult.Output, "Investigation") {
		t.Error("prime output should contain investigation guidelines content")
	}

	// Verify events.
	assertEventEmitted(t, solHome, events.EventCast)
}

// --- CLI Smoke Tests ---

func TestCLICastGuidelinesHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "cast", "--help")
	if err != nil {
		t.Fatalf("sol cast --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "--guidelines") {
		t.Errorf("cast help missing --guidelines flag: %s", out)
	}
	if !strings.Contains(out, "--var") {
		t.Errorf("cast help missing --var flag: %s", out)
	}
}
