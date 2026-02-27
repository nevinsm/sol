package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/workflow"
)

// --- Workflow Integration Tests ---

func TestWorkflowInstantiateAndAdvance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)

	// Create formula directory structure.
	formulaDir := filepath.Join(gtHome, "formulas", "test-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	manifest := `name = "test-formula"
type = "agent"
description = "Test formula"

[variables]
[variables.issue]
required = true

[[steps]]
id = "step1"
title = "First Step"
instructions = "steps/01.md"

[[steps]]
id = "step2"
title = "Second Step"
instructions = "steps/02.md"
needs = ["step1"]

[[steps]]
id = "step3"
title = "Third Step"
instructions = "steps/03.md"
needs = ["step2"]
`
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("# Step 1\nDo the first thing for {{issue}}.\n"), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "02.md"), []byte("# Step 2\nDo the second thing.\n"), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "03.md"), []byte("# Step 3\nDo the third thing.\n"), 0o644)

	// Create outpost dir.
	rig := "testrig"
	agent := "TestBot"

	// 1. Instantiate.
	inst, state, err := workflow.Instantiate(rig, agent, "test-formula", map[string]string{"issue": "sol-12345678"})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if inst.Formula != "test-formula" {
		t.Errorf("formula: got %q, want test-formula", inst.Formula)
	}
	if state.Status != "running" {
		t.Errorf("status: got %q, want running", state.Status)
	}
	if state.CurrentStep != "step1" {
		t.Errorf("current step: got %q, want step1", state.CurrentStep)
	}

	// 2. ReadCurrentStep → first step.
	step, err := workflow.ReadCurrentStep(rig, agent)
	if err != nil {
		t.Fatalf("ReadCurrentStep: %v", err)
	}
	if step.ID != "step1" {
		t.Errorf("current step ID: got %q, want step1", step.ID)
	}
	if !strings.Contains(step.Instructions, "sol-12345678") {
		t.Error("step instructions should contain variable substitution")
	}

	// 3. Advance → second step.
	nextStep, done, err := workflow.Advance(rig, agent)
	if err != nil {
		t.Fatalf("Advance to step2: %v", err)
	}
	if done {
		t.Error("expected not done after first advance")
	}
	if nextStep.ID != "step2" {
		t.Errorf("next step: got %q, want step2", nextStep.ID)
	}

	// 4. Advance → third step.
	nextStep, done, err = workflow.Advance(rig, agent)
	if err != nil {
		t.Fatalf("Advance to step3: %v", err)
	}
	if done {
		t.Error("expected not done after second advance")
	}
	if nextStep.ID != "step3" {
		t.Errorf("next step: got %q, want step3", nextStep.ID)
	}

	// 5. Advance → done.
	_, done, err = workflow.Advance(rig, agent)
	if err != nil {
		t.Fatalf("Advance to done: %v", err)
	}
	if !done {
		t.Error("expected done after final advance")
	}

	// 6. ReadState → status="done".
	state, err = workflow.ReadState(rig, agent)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Status != "done" {
		t.Errorf("status: got %q, want done", state.Status)
	}
	if len(state.Completed) != 3 {
		t.Errorf("completed count: got %d, want 3", len(state.Completed))
	}
}

func TestWorkflowCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)

	// Create formula.
	formulaDir := filepath.Join(gtHome, "formulas", "crash-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	manifest := `name = "crash-formula"
type = "agent"
description = "Crash test"

[variables]
[variables.issue]
required = true

[[steps]]
id = "s1"
title = "Step 1"
instructions = "steps/01.md"

[[steps]]
id = "s2"
title = "Step 2"
instructions = "steps/02.md"
needs = ["s1"]

[[steps]]
id = "s3"
title = "Step 3"
instructions = "steps/03.md"
needs = ["s2"]
`
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Step 1 instructions.\n"), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "02.md"), []byte("Step 2 instructions.\n"), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "03.md"), []byte("Step 3 instructions.\n"), 0o644)

	rig := "testrig"
	agent := "CrashBot"

	// 1. Instantiate and advance to step 2.
	workflow.Instantiate(rig, agent, "crash-formula", map[string]string{"issue": "sol-crash"})
	workflow.Advance(rig, agent) // step1 → step2

	// 2. Simulate crash: read state from disk (no in-memory state to clear).
	state, err := workflow.ReadState(rig, agent)
	if err != nil {
		t.Fatalf("ReadState after crash: %v", err)
	}
	if state.CurrentStep != "s2" {
		t.Errorf("current step after crash: got %q, want s2", state.CurrentStep)
	}

	// 3. ReadCurrentStep → step 2 instructions.
	step, err := workflow.ReadCurrentStep(rig, agent)
	if err != nil {
		t.Fatalf("ReadCurrentStep after crash: %v", err)
	}
	if step.ID != "s2" {
		t.Errorf("step ID: got %q, want s2", step.ID)
	}
	if !strings.Contains(step.Instructions, "Step 2") {
		t.Error("step instructions should contain 'Step 2'")
	}

	// 4. Advance → step 3 (workflow resumed correctly).
	nextStep, done, err := workflow.Advance(rig, agent)
	if err != nil {
		t.Fatalf("Advance after crash: %v", err)
	}
	if done {
		t.Error("expected not done")
	}
	if nextStep.ID != "s3" {
		t.Errorf("next step: got %q, want s3", nextStep.ID)
	}
}

func TestCastWithWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "testrig")
	mgr := newMockSessionChecker()

	// Create formula.
	formulaDir := filepath.Join(gtHome, "formulas", "cast-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	manifest := `name = "cast-formula"
type = "agent"
description = "Cast test"

[variables]
[variables.issue]
required = true

[[steps]]
id = "only-step"
title = "Only Step"
instructions = "steps/01.md"
`
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Do the thing for {{issue}}.\n"), 0o644)

	// Create agent and work item.
	sphereStore.CreateAgent("WorkflowBot", "testrig", "agent")
	itemID, _ := worldStore.CreateWorkItem("WF task", "Workflow test", "operator", 2, nil)

	logger := events.NewLogger(gtHome)

	// Cast with formula.
	result, err := dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "testrig",
		AgentName:  "WorkflowBot",
		SourceRepo: sourceRepo,
		Formula:    "cast-formula",
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("cast with formula: %v", err)
	}

	if result.Formula != "cast-formula" {
		t.Errorf("result formula: got %q, want cast-formula", result.Formula)
	}

	// Verify .workflow/ directory created in agent's outpost dir.
	wfDir := filepath.Join(gtHome, "testrig", "outposts", "WorkflowBot", ".workflow")
	if _, err := os.Stat(wfDir); os.IsNotExist(err) {
		t.Error(".workflow/ directory should exist after cast with formula")
	}

	// Verify state.json exists with current_step set.
	state, err := workflow.ReadState("testrig", "WorkflowBot")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state == nil {
		t.Fatal("state should not be nil")
	}
	if state.CurrentStep != "only-step" {
		t.Errorf("current step: got %q, want only-step", state.CurrentStep)
	}

	// Verify CLAUDE.md includes workflow commands.
	claudeMD := filepath.Join(result.WorktreeDir, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "workflow current") {
		t.Error("CLAUDE.md should contain 'workflow current'")
	}
	if !strings.Contains(content, "workflow advance") {
		t.Error("CLAUDE.md should contain 'workflow advance'")
	}

	// Verify workflow event was emitted.
	assertEventEmitted(t, gtHome, events.EventWorkflowInstantiate)
}

func TestPrimeWithWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "testrig")
	mgr := newMockSessionChecker()

	// Create formula.
	formulaDir := filepath.Join(gtHome, "formulas", "prime-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	manifest := `name = "prime-formula"
type = "agent"
description = "Prime test"

[variables]
[variables.issue]
required = true

[[steps]]
id = "step1"
title = "First Step"
instructions = "steps/01.md"

[[steps]]
id = "step2"
title = "Second Step"
instructions = "steps/02.md"
needs = ["step1"]
`
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Execute step 1 for {{issue}}.\n"), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "02.md"), []byte("Execute step 2.\n"), 0o644)

	// Create agent and work item.
	sphereStore.CreateAgent("PrimeBot", "testrig", "agent")
	itemID, _ := worldStore.CreateWorkItem("Prime WF task", "Prime workflow test", "operator", 2, nil)

	// Cast with formula.
	dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "testrig",
		AgentName:  "PrimeBot",
		SourceRepo: sourceRepo,
		Formula:    "prime-formula",
	}, worldStore, sphereStore, mgr, nil)

	// Call Prime.
	result, err := dispatch.Prime("testrig", "PrimeBot", worldStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Verify output contains current step instructions.
	if !strings.Contains(result.Output, "Execute step 1") {
		t.Error("prime output should contain step 1 instructions")
	}

	// Verify output contains propulsion loop commands.
	if !strings.Contains(result.Output, "sol workflow advance") {
		t.Error("prime output should contain 'sol workflow advance'")
	}
	if !strings.Contains(result.Output, "sol workflow status") {
		t.Error("prime output should contain 'sol workflow status'")
	}
	if !strings.Contains(result.Output, "sol resolve") {
		t.Error("prime output should contain 'sol resolve'")
	}

	// Verify workflow formula name appears.
	if !strings.Contains(result.Output, "prime-formula") {
		t.Error("prime output should contain formula name")
	}
}

func TestPrimeWithoutWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "testrig")
	mgr := newMockSessionChecker()

	// Create agent and work item — cast without formula.
	sphereStore.CreateAgent("PlainBot", "testrig", "agent")
	itemID, _ := worldStore.CreateWorkItem("Plain task", "No workflow test", "operator", 2, nil)

	dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "testrig",
		AgentName:  "PlainBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)

	// Call Prime.
	result, err := dispatch.Prime("testrig", "PlainBot", worldStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Verify standard format — should NOT contain workflow section.
	if strings.Contains(result.Output, "Workflow:") {
		t.Error("prime output should not contain workflow section without formula")
	}
	if strings.Contains(result.Output, "sol workflow advance") {
		t.Error("prime output should not contain workflow commands without formula")
	}

	// Should contain standard instructions.
	if !strings.Contains(result.Output, "sol resolve") {
		t.Error("prime output should contain 'sol resolve'")
	}
	if !strings.Contains(result.Output, itemID) {
		t.Error("prime output should contain work item ID")
	}
}

func TestDoneWithWorkflowCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "testrig")
	mgr := newMockSessionChecker()

	// Create formula.
	formulaDir := filepath.Join(gtHome, "formulas", "done-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	manifest := `name = "done-formula"
type = "agent"
description = "Done test"

[variables]
[variables.issue]
required = true

[[steps]]
id = "only"
title = "Only Step"
instructions = "steps/01.md"
`
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Do it.\n"), 0o644)

	// Create agent and work item.
	sphereStore.CreateAgent("DoneBot", "testrig", "agent")
	itemID, _ := worldStore.CreateWorkItem("Done WF task", "Done workflow test", "operator", 2, nil)

	// Cast with formula.
	result, err := dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "testrig",
		AgentName:  "DoneBot",
		SourceRepo: sourceRepo,
		Formula:    "done-formula",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Verify .workflow/ exists.
	wfDir := filepath.Join(gtHome, "testrig", "outposts", "DoneBot", ".workflow")
	if _, err := os.Stat(wfDir); os.IsNotExist(err) {
		t.Fatal(".workflow/ should exist before done")
	}

	// Simulate agent work in worktree.
	os.WriteFile(filepath.Join(result.WorktreeDir, "work.txt"), []byte("done\n"), 0o644)

	// Call Resolve.
	_, err = dispatch.Resolve(dispatch.ResolveOpts{
		World:       "testrig",
		AgentName: "DoneBot",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Verify .workflow/ directory is removed.
	if _, err := os.Stat(wfDir); !os.IsNotExist(err) {
		t.Error(".workflow/ should be removed after resolve")
	}
}

// --- Caravan Integration Tests ---

func TestCaravanCreateAndCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create work items and deps in rig store, then close it.
	worldStore, err := store.OpenWorld("testrig")
	if err != nil {
		t.Fatalf("open rig store: %v", err)
	}
	idA, _ := worldStore.CreateWorkItem("Task A", "First task", "operator", 2, nil)
	idB, _ := worldStore.CreateWorkItem("Task B", "Second task", "operator", 2, nil)
	idC, _ := worldStore.CreateWorkItem("Task C", "Depends on A and B", "operator", 2, nil)
	worldStore.AddDependency(idC, idA)
	worldStore.AddDependency(idC, idB)
	worldStore.Close()

	// Create caravan with all 3.
	caravanID, err := sphereStore.CreateCaravan("test-convoy", "operator")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	sphereStore.AddCaravanItem(caravanID, idA, "testrig")
	sphereStore.AddCaravanItem(caravanID, idB, "testrig")
	sphereStore.AddCaravanItem(caravanID, idC, "testrig")

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

	// Mark A as done.
	rs, _ := store.OpenWorld("testrig")
	rs.UpdateWorkItem(idA, store.WorkItemUpdates{Status: "done"})
	rs.Close()

	// Check again: B ready, C still blocked (B not done).
	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	for _, st := range statuses {
		if st.WorkItemID == idC && st.Ready {
			t.Error("C should still be blocked (B not done)")
		}
	}

	// Mark B as done.
	rs, _ = store.OpenWorld("testrig")
	rs.UpdateWorkItem(idB, store.WorkItemUpdates{Status: "done"})
	rs.Close()

	// Check again: C now ready.
	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	for _, st := range statuses {
		if st.WorkItemID == idC && !st.Ready {
			t.Error("C should be ready now (A and B done)")
		}
	}
}

func TestCaravanAutoClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create 2 items, no deps.
	worldStore, _ := store.OpenWorld("testrig")
	id1, _ := worldStore.CreateWorkItem("Auto 1", "First", "operator", 2, nil)
	id2, _ := worldStore.CreateWorkItem("Auto 2", "Second", "operator", 2, nil)

	// Mark both as closed.
	worldStore.UpdateWorkItem(id1, store.WorkItemUpdates{Status: "closed"})
	worldStore.UpdateWorkItem(id2, store.WorkItemUpdates{Status: "closed"})
	worldStore.Close()

	// Create caravan.
	caravanID, _ := sphereStore.CreateCaravan("auto-close-test", "operator")
	sphereStore.AddCaravanItem(caravanID, id1, "testrig")
	sphereStore.AddCaravanItem(caravanID, id2, "testrig")

	// TryCloseCaravan → should return true.
	closed, err := sphereStore.TryCloseCaravan(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("TryCloseCaravan: %v", err)
	}
	if !closed {
		t.Error("caravan should auto-close when all items are done/closed")
	}

	// Verify caravan status.
	caravan, _ := sphereStore.GetCaravan(caravanID)
	if caravan.Status != "closed" {
		t.Errorf("caravan status: got %q, want closed", caravan.Status)
	}
	if caravan.ClosedAt == nil {
		t.Error("closed_at should be set")
	}
}

func TestCaravanMultiRig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create work items in rig "alpha".
	alphaStore, err := store.OpenWorld("alpha")
	if err != nil {
		t.Fatalf("open alpha rig: %v", err)
	}
	idA, _ := alphaStore.CreateWorkItem("Alpha task", "Task in alpha rig", "operator", 2, nil)
	alphaStore.Close()

	// Create work items in rig "beta".
	betaStore, err := store.OpenWorld("beta")
	if err != nil {
		t.Fatalf("open beta rig: %v", err)
	}
	idB, _ := betaStore.CreateWorkItem("Beta task 1", "First task in beta", "operator", 2, nil)
	idC, _ := betaStore.CreateWorkItem("Beta task 2", "Second task in beta", "operator", 2, nil)
	// C depends on B within beta rig.
	betaStore.AddDependency(idC, idB)
	betaStore.Close()

	// Create caravan spanning both rigs.
	caravanID, err := sphereStore.CreateCaravan("multi-rig-convoy", "operator")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	sphereStore.AddCaravanItem(caravanID, idA, "alpha")
	sphereStore.AddCaravanItem(caravanID, idB, "beta")
	sphereStore.AddCaravanItem(caravanID, idC, "beta")

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
		statusMap[st.WorkItemID] = st
	}

	// A (alpha) should be ready.
	if st, ok := statusMap[idA]; !ok {
		t.Error("missing status for alpha item")
	} else {
		if st.World != "alpha" {
			t.Errorf("alpha item rig: got %q, want alpha", st.World)
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

	// Mark B as done → C should become ready.
	bs, _ := store.OpenWorld("beta")
	bs.UpdateWorkItem(idB, store.WorkItemUpdates{Status: "done"})
	bs.Close()

	statuses, _ = sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	for _, st := range statuses {
		if st.WorkItemID == idC && !st.Ready {
			t.Error("beta item C should be ready after B is done")
		}
	}

	// Mark all items done → caravan should auto-close.
	as, _ := store.OpenWorld("alpha")
	as.UpdateWorkItem(idA, store.WorkItemUpdates{Status: "done"})
	as.Close()

	bs, _ = store.OpenWorld("beta")
	bs.UpdateWorkItem(idC, store.WorkItemUpdates{Status: "done"})
	bs.Close()

	closed, err := sphereStore.TryCloseCaravan(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("TryCloseCaravan: %v", err)
	}
	if !closed {
		t.Error("multi-rig caravan should auto-close when all items done")
	}
}

// --- End-to-End Workflow Test ---

func TestWorkflowPropulsionLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "testrig")
	mgr := newMockSessionChecker()

	// Create formula with 3 steps.
	formulaDir := filepath.Join(gtHome, "formulas", "propulsion-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	manifest := `name = "propulsion-formula"
type = "agent"
description = "Propulsion loop test"

[variables]
[variables.issue]
required = true

[[steps]]
id = "load"
title = "Load Context"
instructions = "steps/01-load.md"

[[steps]]
id = "implement"
title = "Implement"
instructions = "steps/02-implement.md"
needs = ["load"]

[[steps]]
id = "verify"
title = "Verify"
instructions = "steps/03-verify.md"
needs = ["implement"]
`
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "01-load.md"), []byte("Load context for {{issue}}.\n"), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "02-implement.md"), []byte("Implement the feature.\n"), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "03-verify.md"), []byte("Run tests and verify.\n"), 0o644)

	// Create agent and work item.
	sphereStore.CreateAgent("PropBot", "testrig", "agent")
	itemID, _ := worldStore.CreateWorkItem("Propulsion task", "E2E test", "operator", 2, nil)

	logger := events.NewLogger(gtHome)

	// 1. Cast with formula (mock session).
	result, err := dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "testrig",
		AgentName:  "PropBot",
		SourceRepo: sourceRepo,
		Formula:    "propulsion-formula",
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	// 2. Prime → get step 1 instructions.
	primeResult, err := dispatch.Prime("testrig", "PropBot", worldStore)
	if err != nil {
		t.Fatalf("prime 1: %v", err)
	}
	if !strings.Contains(primeResult.Output, "Load context") {
		t.Error("prime 1 should contain step 1 instructions")
	}

	// 3. workflow advance → step 2.
	nextStep, done, err := workflow.Advance("testrig", "PropBot")
	if err != nil {
		t.Fatalf("advance 1: %v", err)
	}
	if done {
		t.Error("should not be done after step 1")
	}
	if nextStep.ID != "implement" {
		t.Errorf("step 2: got %q, want implement", nextStep.ID)
	}

	// 4. Prime again → get step 2 instructions (crash recovery sim).
	primeResult, err = dispatch.Prime("testrig", "PropBot", worldStore)
	if err != nil {
		t.Fatalf("prime 2: %v", err)
	}
	if !strings.Contains(primeResult.Output, "Implement the feature") {
		t.Error("prime 2 should contain step 2 instructions")
	}

	// 5. workflow advance → step 3.
	nextStep, done, err = workflow.Advance("testrig", "PropBot")
	if err != nil {
		t.Fatalf("advance 2: %v", err)
	}
	if done {
		t.Error("should not be done after step 2")
	}
	if nextStep.ID != "verify" {
		t.Errorf("step 3: got %q, want verify", nextStep.ID)
	}

	// 6. workflow advance → complete.
	_, done, err = workflow.Advance("testrig", "PropBot")
	if err != nil {
		t.Fatalf("advance 3: %v", err)
	}
	if !done {
		t.Error("should be done after step 3")
	}

	// 7. Simulate work in worktree.
	os.WriteFile(filepath.Join(result.WorktreeDir, "feature.go"), []byte("package main\n"), 0o644)

	// 8. Resolve → workflow cleaned up, work item marked done.
	_, err = dispatch.Resolve(dispatch.ResolveOpts{
		World:       "testrig",
		AgentName: "PropBot",
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Verify work item is done.
	item, _ := worldStore.GetWorkItem(itemID)
	if item.Status != "done" {
		t.Errorf("work item status: got %q, want done", item.Status)
	}

	// Verify workflow cleaned up.
	wfDir := filepath.Join(gtHome, "testrig", "outposts", "PropBot", ".workflow")
	if _, err := os.Stat(wfDir); !os.IsNotExist(err) {
		t.Error(".workflow/ should be removed after resolve")
	}

	// Verify events.
	assertEventEmitted(t, gtHome, events.EventCast)
	assertEventEmitted(t, gtHome, events.EventWorkflowInstantiate)
	assertEventEmitted(t, gtHome, events.EventResolve)
}

// --- CLAUDE.md Tests ---

func TestClaudeMDWithWorkflow(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "testrig",
		WorkItemID:  "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
		HasWorkflow: true,
	}

	content := protocol.GenerateClaudeMD(ctx)

	// Should contain workflow commands.
	if !strings.Contains(content, "sol workflow current") {
		t.Error("CLAUDE.md should contain 'sol workflow current'")
	}
	if !strings.Contains(content, "sol workflow advance") {
		t.Error("CLAUDE.md should contain 'sol workflow advance'")
	}
	if !strings.Contains(content, "sol workflow status") {
		t.Error("CLAUDE.md should contain 'sol workflow status'")
	}

	// Should have workflow protocol.
	if !strings.Contains(content, "Repeat from step 1") {
		t.Error("CLAUDE.md should contain workflow protocol")
	}
}

func TestClaudeMDWithoutWorkflow(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "testrig",
		WorkItemID:  "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
		HasWorkflow: false,
	}

	content := protocol.GenerateClaudeMD(ctx)

	// Should NOT contain workflow commands.
	if strings.Contains(content, "sol workflow current") {
		t.Error("CLAUDE.md should not contain workflow commands without workflow")
	}

	// Should have standard protocol.
	if !strings.Contains(content, "sol resolve") {
		t.Error("CLAUDE.md should contain 'sol resolve'")
	}
}

// --- CLI Smoke Tests ---

func TestCLICastFormulaHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "cast", "--help")
	if err != nil {
		t.Fatalf("sol cast --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "--formula") {
		t.Errorf("cast help missing --formula flag: %s", out)
	}
	if !strings.Contains(out, "--var") {
		t.Errorf("cast help missing --var flag: %s", out)
	}
}
