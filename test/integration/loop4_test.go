package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/workflow"
)

// --- Workflow Integration Tests ---

func TestWorkflowInstantiateAndAdvance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Create formula directory structure.
	formulaDir := filepath.Join(solHome, "formulas", "test-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create steps dir: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("# Step 1\nDo the first thing for {{issue}}.\n"), 0o644); err != nil {
		t.Fatalf("write 01.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "02.md"), []byte("# Step 2\nDo the second thing.\n"), 0o644); err != nil {
		t.Fatalf("write 02.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "03.md"), []byte("# Step 3\nDo the third thing.\n"), 0o644); err != nil {
		t.Fatalf("write 03.md: %v", err)
	}

	// Create outpost dir.
	world := "ember"
	agent := "TestBot"

	// 1. Instantiate.
	inst, state, err := workflow.Instantiate(world, agent, "agent", "test-formula", map[string]string{"issue": "sol-12345678"})
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
	step, err := workflow.ReadCurrentStep(world, agent, "agent")
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
	nextStep, done, err := workflow.Advance(world, agent, "agent")
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
	nextStep, done, err = workflow.Advance(world, agent, "agent")
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
	_, done, err = workflow.Advance(world, agent, "agent")
	if err != nil {
		t.Fatalf("Advance to done: %v", err)
	}
	if !done {
		t.Error("expected done after final advance")
	}

	// 6. ReadState → status="done".
	state, err = workflow.ReadState(world, agent, "agent")
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

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Create formula.
	formulaDir := filepath.Join(solHome, "formulas", "crash-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create steps dir: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Step 1 instructions.\n"), 0o644); err != nil {
		t.Fatalf("write 01.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "02.md"), []byte("Step 2 instructions.\n"), 0o644); err != nil {
		t.Fatalf("write 02.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "03.md"), []byte("Step 3 instructions.\n"), 0o644); err != nil {
		t.Fatalf("write 03.md: %v", err)
	}

	world := "ember"
	agent := "CrashBot"

	// 1. Instantiate and advance to step 2.
	if _, _, err := workflow.Instantiate(world, agent, "agent", "crash-formula", map[string]string{"issue": "sol-crash"}); err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if _, _, err := workflow.Advance(world, agent, "agent"); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	// 2. Simulate crash: read state from disk (no in-memory state to clear).
	state, err := workflow.ReadState(world, agent, "agent")
	if err != nil {
		t.Fatalf("ReadState after crash: %v", err)
	}
	if state.CurrentStep != "s2" {
		t.Errorf("current step after crash: got %q, want s2", state.CurrentStep)
	}

	// 3. ReadCurrentStep → step 2 instructions.
	step, err := workflow.ReadCurrentStep(world, agent, "agent")
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
	nextStep, done, err := workflow.Advance(world, agent, "agent")
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

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create formula.
	formulaDir := filepath.Join(solHome, "formulas", "cast-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create steps dir: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Do the thing for {{issue}}.\n"), 0o644); err != nil {
		t.Fatalf("write 01.md: %v", err)
	}

	// Create agent and work item.
	if _, err := sphereStore.CreateAgent("WorkflowBot", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWorkItem("WF task", "Workflow test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}

	logger := events.NewLogger(solHome)

	// Cast with formula.
	result, err := dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "ember",
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
	wfDir := filepath.Join(solHome, "ember", "outposts", "WorkflowBot", ".workflow")
	if _, err := os.Stat(wfDir); os.IsNotExist(err) {
		t.Error(".workflow/ directory should exist after cast with formula")
	}

	// Verify state.json exists with current_step set.
	state, err := workflow.ReadState("ember", "WorkflowBot", "agent")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state == nil {
		t.Fatal("state should not be nil")
	}
	if state.CurrentStep != "only-step" {
		t.Errorf("current step: got %q, want only-step", state.CurrentStep)
	}

	// Verify CLAUDE.local.md includes workflow commands.
	claudeMD := filepath.Join(result.WorktreeDir, ".claude", "CLAUDE.local.md")
	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("read CLAUDE.local.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "workflow current") {
		t.Error("CLAUDE.md should contain 'workflow current'")
	}
	if !strings.Contains(content, "workflow advance") {
		t.Error("CLAUDE.md should contain 'workflow advance'")
	}

	// Verify workflow event was emitted.
	assertEventEmitted(t, solHome, events.EventWorkflowInstantiate)
}

func TestPrimeWithWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create formula.
	formulaDir := filepath.Join(solHome, "formulas", "prime-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create steps dir: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Execute step 1 for {{issue}}.\n"), 0o644); err != nil {
		t.Fatalf("write 01.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "02.md"), []byte("Execute step 2.\n"), 0o644); err != nil {
		t.Fatalf("write 02.md: %v", err)
	}

	// Create agent and work item.
	if _, err := sphereStore.CreateAgent("PrimeBot", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWorkItem("Prime WF task", "Prime workflow test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}

	// Cast with formula.
	if _, err := dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		AgentName:  "PrimeBot",
		SourceRepo: sourceRepo,
		Formula:    "prime-formula",
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Call Prime.
	result, err := dispatch.Prime("ember", "PrimeBot", "agent", worldStore)
	if err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Verify output contains current step instructions.
	if !strings.Contains(result.Output, "Execute step 1") {
		t.Error("prime output should contain step 1 instructions")
	}

	// Verify output contains propulsion commands.
	if !strings.Contains(result.Output, "sol workflow advance") {
		t.Error("prime output should contain 'sol workflow advance'")
	}
	if !strings.Contains(result.Output, "sol resolve") {
		t.Error("prime output should contain 'sol resolve'")
	}

	// Verify checklist is present.
	if !strings.Contains(result.Output, "[>]") {
		t.Error("prime output should contain current step marker '[>]'")
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
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and work item — cast without formula.
	if _, err := sphereStore.CreateAgent("PlainBot", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWorkItem("Plain task", "No workflow test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}

	if _, err := dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		AgentName:  "PlainBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Call Prime.
	result, err := dispatch.Prime("ember", "PlainBot", "agent", worldStore)
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

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create formula.
	formulaDir := filepath.Join(solHome, "formulas", "done-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create steps dir: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Do it.\n"), 0o644); err != nil {
		t.Fatalf("write 01.md: %v", err)
	}

	// Create agent and work item.
	if _, err := sphereStore.CreateAgent("DoneBot", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWorkItem("Done WF task", "Done workflow test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}

	// Cast with formula.
	result, err := dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		AgentName:  "DoneBot",
		SourceRepo: sourceRepo,
		Formula:    "done-formula",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Verify .workflow/ exists.
	wfDir := filepath.Join(solHome, "ember", "outposts", "DoneBot", ".workflow")
	if _, err := os.Stat(wfDir); os.IsNotExist(err) {
		t.Fatal(".workflow/ should exist before done")
	}

	// Simulate agent work in worktree.
	if err := os.WriteFile(filepath.Join(result.WorktreeDir, "work.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}

	// Call Resolve.
	_, err = dispatch.Resolve(dispatch.ResolveOpts{
		World:       "ember",
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

	// Create work items and deps in world store, then close it.
	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	idA, err := worldStore.CreateWorkItem("Task A", "First task", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem A: %v", err)
	}
	idB, err := worldStore.CreateWorkItem("Task B", "Second task", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem B: %v", err)
	}
	idC, err := worldStore.CreateWorkItem("Task C", "Depends on A and B", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem C: %v", err)
	}
	if err := worldStore.AddDependency(idC, idA); err != nil {
		t.Fatalf("AddDependency C→A: %v", err)
	}
	if err := worldStore.AddDependency(idC, idB); err != nil {
		t.Fatalf("AddDependency C→B: %v", err)
	}
	worldStore.Close()

	// Create caravan with all 3.
	caravanID, err := sphereStore.CreateCaravan("test-caravan", "operator")
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
	if err := rs.CloseWorkItem(idA); err != nil {
		t.Fatalf("close work item A: %v", err)
	}
	rs.Close()

	// Check again: B ready, C still blocked (B not closed).
	statuses, err = sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness after A closed: %v", err)
	}
	for _, st := range statuses {
		if st.WorkItemID == idC && st.Ready {
			t.Error("C should still be blocked (B not closed)")
		}
	}

	// Close B (merged).
	rs, err = store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	if err := rs.CloseWorkItem(idB); err != nil {
		t.Fatalf("close work item B: %v", err)
	}
	rs.Close()

	// Check again: C now ready.
	statuses, err = sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness after B closed: %v", err)
	}
	for _, st := range statuses {
		if st.WorkItemID == idC && !st.Ready {
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
	id1, err := worldStore.CreateWorkItem("Auto 1", "First", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem 1: %v", err)
	}
	id2, err := worldStore.CreateWorkItem("Auto 2", "Second", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem 2: %v", err)
	}

	// Mark both as closed.
	if err := worldStore.UpdateWorkItem(id1, store.WorkItemUpdates{Status: "closed"}); err != nil {
		t.Fatalf("update work item 1: %v", err)
	}
	if err := worldStore.UpdateWorkItem(id2, store.WorkItemUpdates{Status: "closed"}); err != nil {
		t.Fatalf("update work item 2: %v", err)
	}
	worldStore.Close()

	// Create caravan.
	caravanID, err := sphereStore.CreateCaravan("auto-close-test", "operator")
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

	// Create work items in world "alpha".
	alphaStore, err := store.OpenWorld("alpha")
	if err != nil {
		t.Fatalf("open alpha world: %v", err)
	}
	idA, err := alphaStore.CreateWorkItem("Alpha task", "Task in alpha world", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem alpha: %v", err)
	}
	alphaStore.Close()

	// Create work items in world "beta".
	betaStore, err := store.OpenWorld("beta")
	if err != nil {
		t.Fatalf("open beta world: %v", err)
	}
	idB, err := betaStore.CreateWorkItem("Beta task 1", "First task in beta", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem beta 1: %v", err)
	}
	idC, err := betaStore.CreateWorkItem("Beta task 2", "Second task in beta", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem beta 2: %v", err)
	}
	// C depends on B within beta world.
	if err := betaStore.AddDependency(idC, idB); err != nil {
		t.Fatalf("AddDependency C→B: %v", err)
	}
	betaStore.Close()

	// Create caravan spanning both worlds.
	caravanID, err := sphereStore.CreateCaravan("multi-world-caravan", "operator")
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
		statusMap[st.WorkItemID] = st
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
	if err := bs.CloseWorkItem(idB); err != nil {
		t.Fatalf("close work item B: %v", err)
	}
	bs.Close()

	statuses, err = sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness after B closed: %v", err)
	}
	for _, st := range statuses {
		if st.WorkItemID == idC && !st.Ready {
			t.Error("beta item C should be ready after B is closed")
		}
	}

	// Mark all items closed (merged) → caravan should auto-close.
	// Note: "done" (code complete) is NOT sufficient — items must be "closed" (merged).
	as, err := store.OpenWorld("alpha")
	if err != nil {
		t.Fatalf("open alpha world: %v", err)
	}
	if err := as.CloseWorkItem(idA); err != nil {
		t.Fatalf("close work item A: %v", err)
	}
	as.Close()

	bs, err = store.OpenWorld("beta")
	if err != nil {
		t.Fatalf("open beta world: %v", err)
	}
	if err := bs.CloseWorkItem(idB); err != nil {
		t.Fatalf("close work item B: %v", err)
	}
	if err := bs.CloseWorkItem(idC); err != nil {
		t.Fatalf("close work item C: %v", err)
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

	// Create 3 work items: A and B are independent, C depends on A.
	idA, err := worldStore.CreateWorkItem("Task A", "First task", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem A: %v", err)
	}
	idB, err := worldStore.CreateWorkItem("Task B", "Second task", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem B: %v", err)
	}
	idC, err := worldStore.CreateWorkItem("Task C", "Depends on A", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem C: %v", err)
	}
	if err := worldStore.AddDependency(idC, idA); err != nil {
		t.Fatalf("AddDependency C→A: %v", err)
	}

	// Create a caravan with all 3 items.
	caravanID, err := sphereStore.CreateCaravan("launch-test", "operator")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	for _, id := range []string{idA, idB, idC} {
		if err := sphereStore.CreateCaravanItem(caravanID, id, "ember", 0); err != nil {
			t.Fatalf("CreateCaravanItem %s: %v", id, err)
		}
	}

	// Pre-create 2 agents. C is blocked, so only A and B should dispatch.
	if _, err := sphereStore.CreateAgent("Alpha", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent Alpha: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Beta", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent Beta: %v", err)
	}

	// Check readiness: A and B ready, C blocked.
	statuses, err := sphereStore.CheckCaravanReadiness(caravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness: %v", err)
	}
	readyCount := 0
	for _, st := range statuses {
		if st.WorkItemStatus == "open" && st.Ready {
			readyCount++
		}
	}
	if readyCount != 2 {
		t.Fatalf("expected 2 ready items, got %d", readyCount)
	}

	// Dispatch ready items (simulates caravan launch logic).
	dispatched := 0
	for _, st := range statuses {
		if st.WorkItemStatus != "open" || !st.Ready {
			continue
		}
		result, err := dispatch.Cast(dispatch.CastOpts{
			WorkItemID: st.WorkItemID,
			World:      "ember",
			SourceRepo: sourceRepo,
		}, worldStore, sphereStore, mgr, logger)
		if err != nil {
			t.Fatalf("Cast %s: %v", st.WorkItemID, err)
		}
		if result.AgentName == "" {
			t.Errorf("Cast %s: empty agent name", st.WorkItemID)
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
	itemA, _ := worldStore.GetWorkItem(idA)
	itemB, _ := worldStore.GetWorkItem(idB)
	itemC, _ := worldStore.GetWorkItem(idC)
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

func TestWorkflowPropulsionLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create formula with 3 steps.
	formulaDir := filepath.Join(solHome, "formulas", "propulsion-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create steps dir: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01-load.md"), []byte("Load context for {{issue}}.\n"), 0o644); err != nil {
		t.Fatalf("write 01-load.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "02-implement.md"), []byte("Implement the feature.\n"), 0o644); err != nil {
		t.Fatalf("write 02-implement.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "03-verify.md"), []byte("Run tests and verify.\n"), 0o644); err != nil {
		t.Fatalf("write 03-verify.md: %v", err)
	}

	// Create agent and work item.
	if _, err := sphereStore.CreateAgent("PropBot", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWorkItem("Propulsion task", "E2E test", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}

	logger := events.NewLogger(solHome)

	// 1. Cast with formula (mock session).
	result, err := dispatch.Cast(dispatch.CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		AgentName:  "PropBot",
		SourceRepo: sourceRepo,
		Formula:    "propulsion-formula",
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	// 2. Prime → get step 1 instructions.
	primeResult, err := dispatch.Prime("ember", "PropBot", "agent", worldStore)
	if err != nil {
		t.Fatalf("prime 1: %v", err)
	}
	if !strings.Contains(primeResult.Output, "Load context") {
		t.Error("prime 1 should contain step 1 instructions")
	}

	// 3. workflow advance → step 2.
	nextStep, done, err := workflow.Advance("ember", "PropBot", "agent")
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
	primeResult, err = dispatch.Prime("ember", "PropBot", "agent", worldStore)
	if err != nil {
		t.Fatalf("prime 2: %v", err)
	}
	if !strings.Contains(primeResult.Output, "Implement the feature") {
		t.Error("prime 2 should contain step 2 instructions")
	}

	// 5. workflow advance → step 3.
	nextStep, done, err = workflow.Advance("ember", "PropBot", "agent")
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
	_, done, err = workflow.Advance("ember", "PropBot", "agent")
	if err != nil {
		t.Fatalf("advance 3: %v", err)
	}
	if !done {
		t.Error("should be done after step 3")
	}

	// 7. Simulate work in worktree.
	if err := os.WriteFile(filepath.Join(result.WorktreeDir, "feature.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write feature.go: %v", err)
	}

	// 8. Resolve → workflow cleaned up, work item marked done.
	_, err = dispatch.Resolve(dispatch.ResolveOpts{
		World:       "ember",
		AgentName: "PropBot",
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Verify work item is done.
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("work item status: got %q, want done", item.Status)
	}

	// Verify workflow cleaned up.
	wfDir := filepath.Join(solHome, "ember", "outposts", "PropBot", ".workflow")
	if _, err := os.Stat(wfDir); !os.IsNotExist(err) {
		t.Error(".workflow/ should be removed after resolve")
	}

	// Verify events.
	assertEventEmitted(t, solHome, events.EventCast)
	assertEventEmitted(t, solHome, events.EventWorkflowInstantiate)
	assertEventEmitted(t, solHome, events.EventResolve)
}

// --- CLI Smoke Tests ---

func TestCLICastFormulaHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	solHome := t.TempDir()

	out, err := runGT(t, solHome, "cast", "--help")
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
