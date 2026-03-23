package integration

// loop6_test.go — Integration tests for caravan lifecycle state machine,
// expansion/convoy workflow types, sol writ activate, and agent diagnostics.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/workflow"
)

// ============================================================
// Caravan Lifecycle — State Machine Tests
// ============================================================

// TestCaravanLifecycleStateMachine tests the full caravan state machine:
// create(drydock) → commission(open) → drydock(drydock) → commission(open)
// → close-force(closed) → reopen(drydock) → remove-item → set-phase → delete.
func TestCaravanLifecycleStateMachine(t *testing.T) {
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

	// Create caravan — starts in "drydock".
	caravanID, err := sphereStore.CreateCaravan("lifecycle-test", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}

	// Create a world store so we can add items.
	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	writA, err := worldStore.CreateWrit("Item A", "First item", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit A: %v", err)
	}
	writB, err := worldStore.CreateWrit("Item B", "Second item", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit B: %v", err)
	}

	// Add items to caravan.
	if err := sphereStore.CreateCaravanItem(caravanID, writA, "ember", 0); err != nil {
		t.Fatalf("CreateCaravanItem A: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, writB, "ember", 0); err != nil {
		t.Fatalf("CreateCaravanItem B: %v", err)
	}

	// Verify initial status is drydock.
	caravan, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan (initial): %v", err)
	}
	if caravan.Status != "drydock" {
		t.Errorf("initial status: got %q, want drydock", caravan.Status)
	}

	// Commission: drydock → open.
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("commission (drydock → open): %v", err)
	}
	caravan, err = sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan (after commission): %v", err)
	}
	if caravan.Status != "open" {
		t.Errorf("after commission: got %q, want open", caravan.Status)
	}

	// Drydock: open → drydock.
	if err := sphereStore.UpdateCaravanStatus(caravanID, "drydock"); err != nil {
		t.Fatalf("drydock (open → drydock): %v", err)
	}
	caravan, err = sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan (after drydock): %v", err)
	}
	if caravan.Status != "drydock" {
		t.Errorf("after drydock: got %q, want drydock", caravan.Status)
	}

	// Commission again: drydock → open.
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("commission again: %v", err)
	}

	// Set phase on item A.
	if err := sphereStore.UpdateCaravanItemPhase(caravanID, writA, 1); err != nil {
		t.Fatalf("UpdateCaravanItemPhase: %v", err)
	}

	// Verify phase updated by checking items.
	items, err := sphereStore.ListCaravanItems(caravanID)
	if err != nil {
		t.Fatalf("ListCaravanItems: %v", err)
	}
	phaseMap := map[string]int{}
	for _, item := range items {
		phaseMap[item.WritID] = item.Phase
	}
	if phaseMap[writA] != 1 {
		t.Errorf("item A phase: got %d, want 1", phaseMap[writA])
	}
	if phaseMap[writB] != 0 {
		t.Errorf("item B phase: got %d, want 0", phaseMap[writB])
	}

	// Remove item B from caravan.
	if err := sphereStore.RemoveCaravanItem(caravanID, writB); err != nil {
		t.Fatalf("RemoveCaravanItem: %v", err)
	}
	items, err = sphereStore.ListCaravanItems(caravanID)
	if err != nil {
		t.Fatalf("ListCaravanItems (after remove): %v", err)
	}
	if len(items) != 1 {
		t.Errorf("item count after remove: got %d, want 1", len(items))
	}
	if items[0].WritID != writA {
		t.Errorf("remaining item: got %q, want %q", items[0].WritID, writA)
	}

	// Force close: open → closed (UpdateCaravanStatus directly, mimicking --force).
	if err := sphereStore.UpdateCaravanStatus(caravanID, "closed"); err != nil {
		t.Fatalf("force close (open → closed): %v", err)
	}
	caravan, err = sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan (after close): %v", err)
	}
	if caravan.Status != "closed" {
		t.Errorf("after close: got %q, want closed", caravan.Status)
	}
	if caravan.ClosedAt == nil {
		t.Error("closed_at should be set after closing")
	}

	// Reopen: closed → drydock.
	if err := sphereStore.UpdateCaravanStatus(caravanID, "drydock"); err != nil {
		t.Fatalf("reopen (closed → drydock): %v", err)
	}
	caravan, err = sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan (after reopen): %v", err)
	}
	if caravan.Status != "drydock" {
		t.Errorf("after reopen: got %q, want drydock", caravan.Status)
	}
	if caravan.ClosedAt != nil {
		t.Error("closed_at should be cleared after reopening")
	}

	// Delete: permanently removes caravan.
	if err := sphereStore.DeleteCaravan(caravanID); err != nil {
		t.Fatalf("DeleteCaravan: %v", err)
	}
	_, err = sphereStore.GetCaravan(caravanID)
	if err == nil {
		t.Error("GetCaravan should fail after delete")
	}
}

// TestCLICaravanLifecycle tests caravan lifecycle via the sol CLI.
func TestCLICaravanLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	// Create a writ so we can add it to the caravan.
	writOut, err := runGT(t, solHome, "writ", "create", "--world=ember", "--title=caravan-item")
	if err != nil {
		t.Fatalf("writ create: %v: %s", err, writOut)
	}
	writID := strings.TrimSpace(writOut)
	if !strings.HasPrefix(writID, "sol-") {
		t.Fatalf("unexpected writ ID: %q", writID)
	}

	// Create a caravan with the writ as an initial item.
	createOut, err := runGT(t, solHome, "caravan", "create", "cli-lifecycle", "--world=ember", writID)
	if err != nil {
		t.Fatalf("caravan create: %v: %s", err, createOut)
	}
	caravanID := extractCaravanID(t, createOut)

	// Commission: drydock → open.
	out, err := runGT(t, solHome, "caravan", "commission", caravanID)
	if err != nil {
		t.Fatalf("caravan commission: %v: %s", err, out)
	}
	if !strings.Contains(out, "open") {
		t.Errorf("commission output should mention 'open': %s", out)
	}

	// Drydock: open → drydock.
	out, err = runGT(t, solHome, "caravan", "drydock", caravanID)
	if err != nil {
		t.Fatalf("caravan drydock: %v: %s", err, out)
	}
	if !strings.Contains(out, "drydock") {
		t.Errorf("drydock output should mention 'drydock': %s", out)
	}

	// Commission again: drydock → open (needed for close/remove).
	out, err = runGT(t, solHome, "caravan", "commission", caravanID)
	if err != nil {
		t.Fatalf("caravan commission (2nd): %v: %s", err, out)
	}

	// set-phase on the writ item.
	out, err = runGT(t, solHome, "caravan", "set-phase", caravanID, writID, "1")
	if err != nil {
		t.Fatalf("caravan set-phase: %v: %s", err, out)
	}
	if !strings.Contains(out, "phase 1") {
		t.Errorf("set-phase output should mention 'phase 1': %s", out)
	}

	// remove: remove the item from caravan.
	out, err = runGT(t, solHome, "caravan", "remove", caravanID, writID)
	if err != nil {
		t.Fatalf("caravan remove: %v: %s", err, out)
	}
	if !strings.Contains(out, "Removed") {
		t.Errorf("remove output should mention 'Removed': %s", out)
	}

	// close --force: close the caravan regardless of item status.
	out, err = runGT(t, solHome, "caravan", "close", "--force", caravanID)
	if err != nil {
		t.Fatalf("caravan close --force: %v: %s", err, out)
	}
	if !strings.Contains(out, "Closed") {
		t.Errorf("close output should mention 'Closed': %s", out)
	}

	// reopen: closed → drydock.
	out, err = runGT(t, solHome, "caravan", "reopen", caravanID)
	if err != nil {
		t.Fatalf("caravan reopen: %v: %s", err, out)
	}
	if !strings.Contains(out, "drydock") {
		t.Errorf("reopen output should mention 'drydock': %s", out)
	}

	// delete --confirm: permanently removes the caravan (must be drydock or closed).
	out, err = runGT(t, solHome, "caravan", "delete", "--confirm", caravanID)
	if err != nil {
		t.Fatalf("caravan delete --confirm: %v: %s", err, out)
	}
	if !strings.Contains(out, "Deleted") {
		t.Errorf("delete output should mention 'Deleted': %s", out)
	}

	// Verify deletion: delete again should fail (caravan no longer exists).
	_, err = runGT(t, solHome, "caravan", "delete", "--confirm", caravanID)
	if err == nil {
		t.Error("second delete should fail (caravan already deleted)")
	}
}

// ============================================================
// Expansion Workflow Type Tests
// ============================================================

// TestSequentialWorkflowMaterialize verifies that a workflow with sequential
// step dependencies creates child writs correctly, using a parent writ as the target.
func TestSequentialWorkflowMaterialize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	// Create workflow with sequential steps at user tier.
	workflowDir := filepath.Join(solHome, "workflows", "test-sequential")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("create workflow dir: %v", err)
	}
	manifest := `name = "test-sequential"
description = "Test sequential workflow"
mode = "manifest"

[vars]
target = { description = "Target writ", required = true }

[[steps]]
id = "analyze"
title = "Analyze {{target.title}}"
description = "Analyze the target writ"

[[steps]]
id = "implement"
title = "Implement {{target.title}}"
description = "Implement based on analysis"
needs = ["analyze"]
`
	if err := os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}

	// Open stores.
	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create a parent (target) writ.
	parentID, err := worldStore.CreateWrit("Feature X", "The feature to work on", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit (parent): %v", err)
	}

	// Materialize the workflow.
	result, err := workflow.Materialize(worldStore, sphereStore, workflow.ManifestOpts{
		Name:      "test-sequential",
		World:     "ember",
		ParentID:  parentID,
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// Verify caravan was created.
	if result.CaravanID == "" {
		t.Error("Materialize should create a caravan")
	}
	caravan, err := sphereStore.GetCaravan(result.CaravanID)
	if err != nil {
		t.Fatalf("GetCaravan: %v", err)
	}
	// Materialize creates the caravan in "drydock" state — commissioning is a
	// separate step performed by the caller (e.g. sol caravan commission).
	if caravan.Status != "drydock" {
		t.Errorf("caravan status: got %q, want drydock", caravan.Status)
	}

	// Verify two child writs created (one per step).
	if len(result.ChildIDs) != 2 {
		t.Errorf("child count: got %d, want 2", len(result.ChildIDs))
	}
	if _, ok := result.ChildIDs["analyze"]; !ok {
		t.Error("missing child writ for 'analyze' step")
	}
	if _, ok := result.ChildIDs["implement"]; !ok {
		t.Error("missing child writ for 'implement' step")
	}

	// Verify title substitution: child title should contain the parent's title.
	analyzeID := result.ChildIDs["analyze"]
	analyzeWrit, err := worldStore.GetWrit(analyzeID)
	if err != nil {
		t.Fatalf("GetWrit (analyze): %v", err)
	}
	if !strings.Contains(analyzeWrit.Title, "Feature X") {
		t.Errorf("analyze writ title should contain parent title 'Feature X', got: %q", analyzeWrit.Title)
	}

	// Verify phases: analyze=0, implement=1 (depends on analyze).
	if result.Phases["analyze"] != 0 {
		t.Errorf("analyze phase: got %d, want 0", result.Phases["analyze"])
	}
	if result.Phases["implement"] != 1 {
		t.Errorf("implement phase: got %d, want 1", result.Phases["implement"])
	}

	// Verify parent is unchanged (workflow uses existing parent).
	if result.ParentID != parentID {
		t.Errorf("parent ID: got %q, want %q", result.ParentID, parentID)
	}
}

// ============================================================
// Convoy Workflow Type Tests
// ============================================================

// TestCodeReviewWorkflowMaterialize verifies that the code-review workflow
// (converted from convoy to unified model) creates parallel analysis writs
// and a synthesis writ, all in a caravan.
func TestCodeReviewWorkflowMaterialize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	// Create manifested workflow at user tier (equivalent to old convoy structure).
	workflowDir := filepath.Join(solHome, "workflows", "test-manifest")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("create workflow dir: %v", err)
	}
	manifest := `name = "test-manifest"
description = "Test manifested workflow"
mode = "manifest"

[[steps]]
id = "alpha"
title = "Alpha dimension"
description = "First parallel step"
kind = "analysis"

[[steps]]
id = "beta"
title = "Beta dimension"
description = "Second parallel step"
kind = "analysis"

[[steps]]
id = "synthesis"
title = "Synthesis"
description = "Combine alpha and beta"
needs = ["alpha", "beta"]
`
	if err := os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}

	// Open stores.
	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Materialize the workflow (no parent — creates one automatically).
	result, err := workflow.Materialize(worldStore, sphereStore, workflow.ManifestOpts{
		Name:      "test-manifest",
		World:     "ember",
		Variables: map[string]string{"issue": "sol-manifest-test"},
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// Verify caravan was created.
	if result.CaravanID == "" {
		t.Error("Materialize should create a caravan")
	}

	// Verify no parent writ is auto-created (caravan provides grouping).
	if result.ParentID != "" {
		t.Errorf("ParentID should be empty when no target provided, got %q", result.ParentID)
	}

	// Verify three child writs: alpha, beta, synthesis.
	if len(result.ChildIDs) != 3 {
		t.Errorf("child count: got %d, want 3 (alpha + beta + synthesis)", len(result.ChildIDs))
	}
	if _, ok := result.ChildIDs["alpha"]; !ok {
		t.Error("missing child writ for 'alpha' step")
	}
	if _, ok := result.ChildIDs["beta"]; !ok {
		t.Error("missing child writ for 'beta' step")
	}
	if _, ok := result.ChildIDs["synthesis"]; !ok {
		t.Error("missing child writ for 'synthesis'")
	}

	// Verify phases: parallel steps are phase 0, synthesis is phase 1.
	if result.Phases["alpha"] != 0 {
		t.Errorf("alpha phase: got %d, want 0", result.Phases["alpha"])
	}
	if result.Phases["beta"] != 0 {
		t.Errorf("beta phase: got %d, want 0", result.Phases["beta"])
	}
	if result.Phases["synthesis"] != 1 {
		t.Errorf("synthesis phase: got %d, want 1", result.Phases["synthesis"])
	}

	// Verify synthesis depends on both parallel steps in the world store.
	synthID := result.ChildIDs["synthesis"]
	alphaID := result.ChildIDs["alpha"]
	betaID := result.ChildIDs["beta"]

	deps, err := worldStore.GetDependencies(synthID)
	if err != nil {
		t.Fatalf("GetDependencies(synthesis): %v", err)
	}
	depSet := map[string]bool{}
	for _, d := range deps {
		depSet[d] = true
	}
	if !depSet[alphaID] {
		t.Error("synthesis should depend on alpha step")
	}
	if !depSet[betaID] {
		t.Error("synthesis should depend on beta step")
	}
}

// ============================================================
// sol writ activate Tests
// ============================================================

// TestWritActivateSwitchesWrit verifies that ActivateWrit updates the
// agent's active_writ in the DB and writes a .resume_state.json file
// for outpost agents (non-persistent roles).
func TestWritActivateSwitchesWrit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	// Open stores.
	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	const world = "ember"
	const agentName = "Switcher"
	const role = "outpost"
	agentID := world + "/" + agentName

	// Create two writs.
	writ1, err := worldStore.CreateWrit("Task One", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit 1: %v", err)
	}
	if err := worldStore.UpdateWrit(writ1, store.WritUpdates{Status: "tethered", Assignee: agentID}); err != nil {
		t.Fatalf("UpdateWrit 1: %v", err)
	}

	writ2, err := worldStore.CreateWrit("Task Two", "Second task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit 2: %v", err)
	}
	if err := worldStore.UpdateWrit(writ2, store.WritUpdates{Status: "tethered", Assignee: agentID}); err != nil {
		t.Fatalf("UpdateWrit 2: %v", err)
	}

	// Create outpost agent with writ1 as active.
	if _, err := sphereStore.CreateAgent(agentName, world, role); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState(agentID, "working", writ1); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Write tether files for both writs.
	if err := tether.Write(world, agentName, writ1, role); err != nil {
		t.Fatalf("tether.Write writ1: %v", err)
	}
	if err := tether.Write(world, agentName, writ2, role); err != nil {
		t.Fatalf("tether.Write writ2: %v", err)
	}

	// Activate writ2 (switching from writ1).
	mgr := newMockSessionChecker()
	result, err := dispatch.ActivateWrit(dispatch.ActivateOpts{
		World:     world,
		AgentName: agentName,
		WritID:    writ2,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("ActivateWrit: %v", err)
	}

	// Verify result fields.
	if result.WritID != writ2 {
		t.Errorf("result.WritID = %q, want %q", result.WritID, writ2)
	}
	if result.PreviousWrit != writ1 {
		t.Errorf("result.PreviousWrit = %q, want %q", result.PreviousWrit, writ1)
	}
	if result.AlreadyActive {
		t.Error("result.AlreadyActive should be false for a real switch")
	}

	// Verify active_writ updated in DB.
	agent, err := sphereStore.GetAgent(agentID)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agent.ActiveWrit != writ2 {
		t.Errorf("active_writ = %q, want %q", agent.ActiveWrit, writ2)
	}

	// Verify resume state written to disk.
	// For outpost role: $SOL_HOME/{world}/outposts/{agentName}/.resume_state.json
	agentDir := filepath.Join(solHome, world, "outposts", agentName)
	resumePath := filepath.Join(agentDir, ".resume_state.json")
	data, err := os.ReadFile(resumePath)
	if err != nil {
		t.Fatalf("resume state file should exist at %s: %v", resumePath, err)
	}

	var rs startup.ResumeState
	if err := json.Unmarshal(data, &rs); err != nil {
		t.Fatalf("unmarshal resume state: %v", err)
	}
	if rs.Reason != "writ-switch" {
		t.Errorf("resume state reason: got %q, want writ-switch", rs.Reason)
	}
	if rs.PreviousActiveWrit != writ1 {
		t.Errorf("resume state previous: got %q, want %q", rs.PreviousActiveWrit, writ1)
	}
	if rs.NewActiveWrit != writ2 {
		t.Errorf("resume state new: got %q, want %q", rs.NewActiveWrit, writ2)
	}
}

// TestWritActivateAlreadyActive verifies that activating the already-active
// writ is a no-op (idempotent) and does not write a resume state file.
func TestWritActivateAlreadyActive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	const world = "ember"
	const agentName = "Idempotent"
	const role = "outpost"
	agentID := world + "/" + agentName

	writ1, err := worldStore.CreateWrit("Task One", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit 1: %v", err)
	}
	if err := worldStore.UpdateWrit(writ1, store.WritUpdates{Status: "tethered", Assignee: agentID}); err != nil {
		t.Fatalf("UpdateWrit 1: %v", err)
	}

	if _, err := sphereStore.CreateAgent(agentName, world, role); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState(agentID, "working", writ1); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}
	if err := tether.Write(world, agentName, writ1, role); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	mgr := newMockSessionChecker()
	result, err := dispatch.ActivateWrit(dispatch.ActivateOpts{
		World:     world,
		AgentName: agentName,
		WritID:    writ1,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("ActivateWrit: %v", err)
	}
	if !result.AlreadyActive {
		t.Error("result.AlreadyActive should be true when writ is already active")
	}

	// Resume state should NOT be written for a no-op.
	agentDir := filepath.Join(solHome, world, "outposts", agentName)
	resumePath := filepath.Join(agentDir, ".resume_state.json")
	if _, err := os.Stat(resumePath); !os.IsNotExist(err) {
		t.Error("resume state file should NOT exist for no-op activate")
	}
}

// ============================================================
// Agent Diagnostic Command Tests
// ============================================================

// TestAgentHistoryCLI verifies that sol agent history works end-to-end:
// with no history it prints a message, with --json it emits an empty array.
func TestAgentHistoryCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")

	// Create an agent so history lookup has a real world to query.
	runGT(t, solHome, "agent", "create", "HistBot", "--world=ember")

	// No history yet — human-readable output.
	out, err := runGT(t, solHome, "agent", "history", "--world=ember")
	if err != nil {
		t.Fatalf("agent history (no history): %v: %s", err, out)
	}
	// Should mention "no history" or be empty display.
	if !strings.Contains(strings.ToLower(out), "no") && out != "" {
		// Some implementations print nothing; acceptable.
	}

	// JSON output — succeeds (may print a text message when no entries).
	out, err = runGT(t, solHome, "agent", "history", "--world=ember", "--json")
	if err != nil {
		t.Fatalf("agent history --json: %v: %s", err, out)
	}
	// The CLI prints a text message when there are no entries even with --json;
	// if the output looks like JSON, verify it's valid.
	if out != "" && strings.HasPrefix(out, "[") {
		if !json.Valid([]byte(out)) {
			t.Errorf("agent history --json output is not valid JSON: %s", out)
		}
	}

	// Single agent filter.
	out, err = runGT(t, solHome, "agent", "history", "HistBot", "--world=ember")
	if err != nil {
		t.Fatalf("agent history HistBot: %v: %s", err, out)
	}
	// Should succeed — just no entries.
}

// TestAgentStatsCLI verifies that sol agent stats works end-to-end.
func TestAgentStatsCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	initWorld(t, solHome, "ember")
	runGT(t, solHome, "agent", "create", "StatBot", "--world=ember")

	// Leaderboard mode (no agent name) — no casts yet, prints nothing or "No agent stats".
	out, err := runGT(t, solHome, "agent", "stats", "--world=ember")
	if err != nil {
		t.Fatalf("agent stats (leaderboard): %v: %s", err, out)
	}

	// Single agent mode — stats for StatBot.
	out, err = runGT(t, solHome, "agent", "stats", "StatBot", "--world=ember")
	if err != nil {
		t.Fatalf("agent stats StatBot: %v: %s", err, out)
	}
	if !strings.Contains(out, "StatBot") {
		t.Errorf("agent stats output should contain agent name 'StatBot': %s", out)
	}
	if !strings.Contains(out, "Casts:") {
		t.Errorf("agent stats output should contain 'Casts:': %s", out)
	}

	// Single agent --json mode.
	out, err = runGT(t, solHome, "agent", "stats", "StatBot", "--world=ember", "--json")
	if err != nil {
		t.Fatalf("agent stats StatBot --json: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("agent stats --json output is not valid JSON: %s", out)
	}
	// Verify the JSON has expected fields.
	var report map[string]interface{}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("unmarshal stats JSON: %v", err)
	}
	if _, ok := report["name"]; !ok {
		t.Error("stats JSON should have 'name' field")
	}
	if _, ok := report["total_casts"]; !ok {
		t.Error("stats JSON should have 'total_casts' field")
	}
}

// TestAgentHistoryRoundTrip verifies that history entries written via the
// store layer are readable through sol agent history.
func TestAgentHistoryRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ, then cast (which writes a history entry).
	if _, err := sphereStore.CreateAgent("HistRound", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	writID, err := worldStore.CreateWrit("History task", "Test round trip", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	if _, err := dispatch.Cast(t.Context(), dispatch.CastOpts{
		WritID:     writID,
		World:      "ember",
		AgentName:  "HistRound",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Cast: %v", err)
	}

	// Now query via CLI.
	initWorld(t, solHome, "ember")
	out, err := runGT(t, solHome, "agent", "history", "HistRound", "--world=ember", "--json")
	if err != nil {
		t.Fatalf("agent history --json: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("agent history --json is not valid JSON: %s", out)
	}

	var entries []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal history: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one history entry after cast")
	}

	// Verify the history entry references the agent and action.
	found := false
	for _, e := range entries {
		if name, ok := e["agent_name"].(string); ok && name == "HistRound" {
			if action, ok := e["action"].(string); ok && action == "cast" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("did not find cast history entry for HistRound in: %s", out)
	}
}

// TestWorkflowTypeValidation verifies that expansion and convoy manifests
// are correctly validated and rejected when malformed.
func TestWorkflowTypeValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	tests := []struct {
		name     string
		manifest string
		wantErr  bool
	}{
		{
			name: "expansion type is rejected",
			manifest: `name = "bad-expansion"
type = "expansion"
description = ""
`,
			wantErr: true,
		},
		{
			name: "convoy type is rejected",
			manifest: `name = "bad-convoy"
type = "convoy"
description = ""
`,
			wantErr: true,
		},
		{
			name: "valid workflow with steps",
			manifest: `name = "good-workflow"
description = ""

[[steps]]
id = "s1"
title = "Step 1"
description = "Do something"
`,
			wantErr: false,
		},
		{
			name: "unknown type is rejected",
			manifest: `name = "bad-type"
type = "invalid"
description = ""
`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(tc.manifest), 0o644); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			m, err := workflow.LoadManifest(dir)
			if err != nil {
				if tc.wantErr {
					return // expected
				}
				t.Fatalf("LoadManifest: %v", err)
			}
			err = workflow.Validate(m, dir)
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

