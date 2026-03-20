package integration

// loop7_test.go — End-to-end integration tests for expansion and convoy
// workflow types covering the full agent execution loop:
// materialize → cast → prime → resolve.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/workflow"
)

// ============================================================
// Expansion Workflow E2E Test
// ============================================================

// TestExpansionWorkflowE2E exercises the full agent execution loop for
// expansion workflow child writs: materialize → cast → prime → advance
// through phases → resolve closes child writs.
func TestExpansionWorkflowE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnvWithRepo(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// --- 1. Create expansion workflow manifest ---
	workflowDir := filepath.Join(gtHome, "workflows", "e2e-expansion")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("create workflow dir: %v", err)
	}
	manifest := `name = "e2e-expansion"
type = "expansion"
description = "E2E expansion workflow test"

[[template]]
id = "analyze"
title = "Analyze {target.title}"
description = "Analyze the target writ to understand scope"

[[template]]
id = "implement"
title = "Implement {target.title}"
description = "Implement the solution based on analysis"
needs = ["analyze"]
`
	if err := os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}

	// --- 2. Create parent (target) writ and materialize ---
	parentID, err := worldStore.CreateWrit("Feature Y", "Parent feature for expansion", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit (parent): %v", err)
	}

	result, err := workflow.Materialize(worldStore, sphereStore, workflow.ManifestOpts{
		Name:      "e2e-expansion",
		World:     "ember",
		ParentID:  parentID,
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// Verify child writs were created.
	analyzeID, ok := result.ChildIDs["analyze"]
	if !ok {
		t.Fatal("missing child writ for 'analyze' template")
	}
	implementID, ok := result.ChildIDs["implement"]
	if !ok {
		t.Fatal("missing child writ for 'implement' template")
	}

	// Verify phases: analyze=0, implement=1.
	if result.Phases["analyze"] != 0 {
		t.Errorf("analyze phase: got %d, want 0", result.Phases["analyze"])
	}
	if result.Phases["implement"] != 1 {
		t.Errorf("implement phase: got %d, want 1", result.Phases["implement"])
	}

	// Commission the caravan.
	if err := sphereStore.UpdateCaravanStatus(result.CaravanID, "open"); err != nil {
		t.Fatalf("commission caravan: %v", err)
	}

	// --- 3. Phase 0: Cast and resolve the analyze writ ---
	const agentName = "ExpBot"

	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent (phase 0): %v", err)
	}

	castResult, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     analyzeID,
		World:      "ember",
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast analyze: %v", err)
	}

	// Verify analyze writ is tethered.
	analyzeWrit, err := worldStore.GetWrit(analyzeID)
	if err != nil {
		t.Fatalf("GetWrit (analyze): %v", err)
	}
	if analyzeWrit.Status != "tethered" {
		t.Errorf("analyze writ status after cast: got %q, want tethered", analyzeWrit.Status)
	}

	// Verify tether exists.
	if !tether.IsTethered("ember", agentName, "outpost") {
		t.Error("tether should exist after cast")
	}

	// Prime — verify writ context and description injected.
	primeResult, err := dispatch.Prime("ember", agentName, "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime (analyze): %v", err)
	}
	if !strings.Contains(primeResult.Output, analyzeID) {
		t.Errorf("prime output missing analyze writ ID %q; got:\n%s", analyzeID, primeResult.Output)
	}
	if !strings.Contains(primeResult.Output, "Feature Y") {
		t.Errorf("prime output should contain target title 'Feature Y'; got:\n%s", primeResult.Output)
	}
	// Description (template description with {target.title} substituted) should appear.
	if !strings.Contains(primeResult.Output, "Analyze the target writ") {
		t.Errorf("prime output should contain analyze template description; got:\n%s", primeResult.Output)
	}

	// Simulate work and resolve.
	if err := os.WriteFile(filepath.Join(castResult.WorktreeDir, "analysis.txt"), []byte("analysis output\n"), 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}
	if _, err := dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:     "ember",
		AgentName: agentName,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve analyze: %v", err)
	}

	// Verify analyze writ is done and agent record deleted.
	analyzeWrit, err = worldStore.GetWrit(analyzeID)
	if err != nil {
		t.Fatalf("GetWrit after resolve (analyze): %v", err)
	}
	if analyzeWrit.Status != "done" {
		t.Errorf("analyze writ status after resolve: got %q, want done", analyzeWrit.Status)
	}
	if _, err := sphereStore.GetAgent("ember/" + agentName); err == nil {
		t.Error("agent record should be deleted after resolve")
	}
	if tether.IsTethered("ember", agentName, "outpost") {
		t.Error("tether should be cleared after resolve")
	}

	// --- 4. Phase 1: Cast and resolve the implement writ ---

	// Recreate agent (previous record deleted by resolve).
	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent (phase 1): %v", err)
	}

	castResult2, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     implementID,
		World:      "ember",
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast implement: %v", err)
	}

	// Verify implement writ is tethered.
	implementWrit, err := worldStore.GetWrit(implementID)
	if err != nil {
		t.Fatalf("GetWrit (implement): %v", err)
	}
	if implementWrit.Status != "tethered" {
		t.Errorf("implement writ status after cast: got %q, want tethered", implementWrit.Status)
	}

	// Prime for implement writ — verify instructions injected.
	primeResult2, err := dispatch.Prime("ember", agentName, "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime (implement): %v", err)
	}
	if !strings.Contains(primeResult2.Output, implementID) {
		t.Errorf("prime output missing implement writ ID; got:\n%s", primeResult2.Output)
	}
	if !strings.Contains(primeResult2.Output, "Implement the solution based on analysis") {
		t.Errorf("prime output should contain implement template description; got:\n%s", primeResult2.Output)
	}

	// Simulate work and resolve.
	if err := os.WriteFile(filepath.Join(castResult2.WorktreeDir, "solution.txt"), []byte("solution\n"), 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}
	if _, err := dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:     "ember",
		AgentName: agentName,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve implement: %v", err)
	}

	// --- 5. Verify cleanup: both child writs resolved ---
	implementWrit, err = worldStore.GetWrit(implementID)
	if err != nil {
		t.Fatalf("GetWrit after resolve (implement): %v", err)
	}
	if implementWrit.Status != "done" {
		t.Errorf("implement writ status after resolve: got %q, want done", implementWrit.Status)
	}

	// Verify the caravan items reflect completed writs.
	items, err := sphereStore.ListCaravanItems(result.CaravanID)
	if err != nil {
		t.Fatalf("ListCaravanItems: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("caravan item count: got %d, want 2", len(items))
	}

	// Both child writs should be in "done" state.
	doneCount := 0
	for _, item := range items {
		w, err := worldStore.GetWrit(item.WritID)
		if err != nil {
			t.Fatalf("GetWrit for caravan item %q: %v", item.WritID, err)
		}
		if w.Status == "done" {
			doneCount++
		}
	}
	if doneCount != 2 {
		t.Errorf("done child writ count: got %d, want 2", doneCount)
	}
}

// ============================================================
// Convoy Workflow E2E Test
// ============================================================

// TestConvoyWorkflowE2E exercises the full agent execution loop for convoy
// workflow writs: materialize → cast legs → prime injects leg instructions
// → resolve legs → synthesis step cast/prime/resolve → cleanup.
func TestConvoyWorkflowE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnvWithRepo(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// --- 1. Create convoy workflow manifest ---
	workflowDir := filepath.Join(gtHome, "workflows", "e2e-convoy")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatalf("create workflow dir: %v", err)
	}
	manifest := `name = "e2e-convoy"
type = "convoy"
description = "E2E convoy workflow test"

[[legs]]
id = "alpha"
title = "Alpha analysis leg"
description = "Analyze the alpha dimension of the problem"

[[legs]]
id = "beta"
title = "Beta analysis leg"
description = "Analyze the beta dimension of the problem"

[synthesis]
title = "Synthesis"
description = "Synthesize findings from alpha and beta legs"
depends_on = ["alpha", "beta"]
`
	if err := os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}

	// --- 2. Materialize the convoy workflow ---
	result, err := workflow.Materialize(worldStore, sphereStore, workflow.ManifestOpts{
		Name:      "e2e-convoy",
		World:     "ember",
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// Verify parent writ created automatically.
	if result.ParentID == "" {
		t.Fatal("Materialize convoy should create a parent writ")
	}

	// Verify child writs: alpha, beta (phase 0), synthesis (phase 1).
	alphaID, ok := result.ChildIDs["alpha"]
	if !ok {
		t.Fatal("missing child writ for 'alpha' leg")
	}
	betaID, ok := result.ChildIDs["beta"]
	if !ok {
		t.Fatal("missing child writ for 'beta' leg")
	}
	synthID, ok := result.ChildIDs["synthesis"]
	if !ok {
		t.Fatal("missing child writ for 'synthesis'")
	}

	if result.Phases["alpha"] != 0 {
		t.Errorf("alpha phase: got %d, want 0", result.Phases["alpha"])
	}
	if result.Phases["beta"] != 0 {
		t.Errorf("beta phase: got %d, want 0", result.Phases["beta"])
	}
	if result.Phases["synthesis"] != 1 {
		t.Errorf("synthesis phase: got %d, want 1", result.Phases["synthesis"])
	}

	// Verify synthesis depends on both legs.
	deps, err := worldStore.GetDependencies(synthID)
	if err != nil {
		t.Fatalf("GetDependencies(synthesis): %v", err)
	}
	depSet := make(map[string]bool, len(deps))
	for _, d := range deps {
		depSet[d] = true
	}
	if !depSet[alphaID] {
		t.Error("synthesis should depend on alpha leg")
	}
	if !depSet[betaID] {
		t.Error("synthesis should depend on beta leg")
	}

	// Commission the caravan.
	if err := sphereStore.UpdateCaravanStatus(result.CaravanID, "open"); err != nil {
		t.Fatalf("commission caravan: %v", err)
	}

	const agentName = "ConvoyBot"

	// --- 3. Phase 0a: Cast alpha leg and verify prime injects leg instructions ---
	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent (alpha): %v", err)
	}

	alphaResult, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     alphaID,
		World:      "ember",
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast alpha: %v", err)
	}

	// Verify alpha writ is tethered.
	alphaWrit, err := worldStore.GetWrit(alphaID)
	if err != nil {
		t.Fatalf("GetWrit (alpha): %v", err)
	}
	if alphaWrit.Status != "tethered" {
		t.Errorf("alpha writ status after cast: got %q, want tethered", alphaWrit.Status)
	}

	// Prime — verify leg instructions are injected.
	alphaPrime, err := dispatch.Prime("ember", agentName, "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime (alpha): %v", err)
	}
	if !strings.Contains(alphaPrime.Output, alphaID) {
		t.Errorf("prime output missing alpha writ ID; got:\n%s", alphaPrime.Output)
	}
	// The leg's description should appear in prime output.
	if !strings.Contains(alphaPrime.Output, "Analyze the alpha dimension") {
		t.Errorf("prime output should contain alpha leg description; got:\n%s", alphaPrime.Output)
	}
	if !strings.Contains(alphaPrime.Output, "sol resolve") {
		t.Errorf("prime output should contain 'sol resolve'; got:\n%s", alphaPrime.Output)
	}

	// Simulate work and resolve alpha.
	if err := os.WriteFile(filepath.Join(alphaResult.WorktreeDir, "alpha.txt"), []byte("alpha findings\n"), 0o644); err != nil {
		t.Fatalf("write alpha work file: %v", err)
	}
	if _, err := dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:     "ember",
		AgentName: agentName,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve alpha: %v", err)
	}

	// Verify alpha is done.
	alphaWrit, err = worldStore.GetWrit(alphaID)
	if err != nil {
		t.Fatalf("GetWrit after resolve (alpha): %v", err)
	}
	if alphaWrit.Status != "done" {
		t.Errorf("alpha writ status after resolve: got %q, want done", alphaWrit.Status)
	}

	// --- 4. Phase 0b: Cast beta leg ---
	// Recreate agent (previous record deleted by resolve).
	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent (beta): %v", err)
	}

	betaResult, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     betaID,
		World:      "ember",
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast beta: %v", err)
	}

	// Prime for beta — verify leg instructions injected.
	betaPrime, err := dispatch.Prime("ember", agentName, "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime (beta): %v", err)
	}
	if !strings.Contains(betaPrime.Output, betaID) {
		t.Errorf("prime output missing beta writ ID; got:\n%s", betaPrime.Output)
	}
	if !strings.Contains(betaPrime.Output, "Analyze the beta dimension") {
		t.Errorf("prime output should contain beta leg description; got:\n%s", betaPrime.Output)
	}

	// Simulate work and resolve beta.
	if err := os.WriteFile(filepath.Join(betaResult.WorktreeDir, "beta.txt"), []byte("beta findings\n"), 0o644); err != nil {
		t.Fatalf("write beta work file: %v", err)
	}
	if _, err := dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:     "ember",
		AgentName: agentName,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve beta: %v", err)
	}

	// Verify beta is done.
	betaWrit, err := worldStore.GetWrit(betaID)
	if err != nil {
		t.Fatalf("GetWrit after resolve (beta): %v", err)
	}
	if betaWrit.Status != "done" {
		t.Errorf("beta writ status after resolve: got %q, want done", betaWrit.Status)
	}

	// --- 5. Phase 1: Cast synthesis writ (depends on completed legs) ---
	// Recreate agent.
	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent (synthesis): %v", err)
	}

	synthResult, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     synthID,
		World:      "ember",
		AgentName:  agentName,
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast synthesis: %v", err)
	}

	// Verify synthesis writ is tethered.
	synthWrit, err := worldStore.GetWrit(synthID)
	if err != nil {
		t.Fatalf("GetWrit (synthesis): %v", err)
	}
	if synthWrit.Status != "tethered" {
		t.Errorf("synthesis writ status after cast: got %q, want tethered", synthWrit.Status)
	}

	// Prime for synthesis — verify synthesis description injected.
	synthPrime, err := dispatch.Prime("ember", agentName, "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime (synthesis): %v", err)
	}
	if !strings.Contains(synthPrime.Output, synthID) {
		t.Errorf("prime output missing synthesis writ ID; got:\n%s", synthPrime.Output)
	}
	if !strings.Contains(synthPrime.Output, "Synthesize findings from alpha and beta legs") {
		t.Errorf("prime output should contain synthesis description; got:\n%s", synthPrime.Output)
	}

	// Simulate synthesis work and resolve.
	if err := os.WriteFile(filepath.Join(synthResult.WorktreeDir, "synthesis.txt"), []byte("synthesis report\n"), 0o644); err != nil {
		t.Fatalf("write synthesis work file: %v", err)
	}
	if _, err := dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:     "ember",
		AgentName: agentName,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("Resolve synthesis: %v", err)
	}

	// --- 6. Verify cleanup: all convoy writs resolved ---
	synthWrit, err = worldStore.GetWrit(synthID)
	if err != nil {
		t.Fatalf("GetWrit after resolve (synthesis): %v", err)
	}
	if synthWrit.Status != "done" {
		t.Errorf("synthesis writ status after resolve: got %q, want done", synthWrit.Status)
	}

	// All 3 child writs should be done.
	childIDs := []string{alphaID, betaID, synthID}
	for _, id := range childIDs {
		w, err := worldStore.GetWrit(id)
		if err != nil {
			t.Fatalf("GetWrit %q: %v", id, err)
		}
		if w.Status != "done" {
			t.Errorf("child writ %q status: got %q, want done", id, w.Status)
		}
	}

	// Caravan should have 3 items.
	items, err := sphereStore.ListCaravanItems(result.CaravanID)
	if err != nil {
		t.Fatalf("ListCaravanItems: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("caravan item count: got %d, want 3 (alpha + beta + synthesis)", len(items))
	}
}
