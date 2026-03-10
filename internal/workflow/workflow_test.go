package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

func setupTestWorkflow(t *testing.T, steps []StepDef, vars map[string]VariableDecl) string {
	t.Helper()
	dir := t.TempDir()

	if vars == nil {
		vars = map[string]VariableDecl{
			"issue":       {Required: true},
			"base_branch": {Default: "main"},
		}
	}

	os.MkdirAll(filepath.Join(dir, "steps"), 0o755)
	writeTOMLManifest(t, dir, "test-workflow", steps, vars)

	for _, s := range steps {
		content := "# " + s.Title + "\n\nWork on {{issue}} from {{base_branch}}.\n"
		if err := os.WriteFile(filepath.Join(dir, s.Instructions), []byte(content), 0o644); err != nil {
			t.Fatalf("write step %q: %v", s.ID, err)
		}
	}

	return dir
}

func linearSteps() []StepDef {
	return []StepDef{
		{ID: "load-context", Title: "Load work context", Instructions: "steps/01-load.md"},
		{ID: "implement", Title: "Implement", Instructions: "steps/02-impl.md", Needs: []string{"load-context"}},
		{ID: "verify", Title: "Verify", Instructions: "steps/03-verify.md", Needs: []string{"implement"}},
	}
}

func TestLoadManifest(t *testing.T) {
	dir := setupTestWorkflow(t, linearSteps(), nil)

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadMaterialize() error: %v", err)
	}

	if m.Name != "test-workflow" {
		t.Errorf("name: got %q, want %q", m.Name, "test-workflow")
	}
	if m.Type != "workflow" {
		t.Errorf("type: got %q, want %q", m.Type, "workflow")
	}
	if len(m.Steps) != 3 {
		t.Fatalf("steps: got %d, want 3", len(m.Steps))
	}
	if m.Steps[0].ID != "load-context" {
		t.Errorf("step[0].ID: got %q, want %q", m.Steps[0].ID, "load-context")
	}
	if len(m.Steps[1].Needs) != 1 || m.Steps[1].Needs[0] != "load-context" {
		t.Errorf("step[1].Needs: got %v, want [load-context]", m.Steps[1].Needs)
	}
}

func TestLoadManifestMissing(t *testing.T) {
	_, err := LoadManifest("/nonexistent/path")
	if err == nil {
		t.Fatal("LoadMaterialize() expected error for missing directory")
	}
}

func TestValidateValid(t *testing.T) {
	m := &Manifest{
		Steps: linearSteps(),
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}

func TestValidateDuplicateStepID(t *testing.T) {
	m := &Manifest{
		Steps: []StepDef{
			{ID: "step-a", Title: "A"},
			{ID: "step-a", Title: "A duplicate"},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for duplicate step ID")
	}
	if got := err.Error(); got != `duplicate step ID "step-a"` {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateMissingNeed(t *testing.T) {
	m := &Manifest{
		Steps: []StepDef{
			{ID: "step-a", Title: "A", Needs: []string{"nonexistent"}},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for missing need")
	}
}

func TestValidateCycle(t *testing.T) {
	m := &Manifest{
		Steps: []StepDef{
			{ID: "a", Title: "A", Needs: []string{"b"}},
			{ID: "b", Title: "B", Needs: []string{"a"}},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for cycle")
	}
	if got := err.Error(); got != "dependency cycle detected in steps" {
		t.Errorf("error: got %q", got)
	}
}

func TestResolveVariables(t *testing.T) {
	m := &Manifest{
		Variables: map[string]VariableDecl{
			"issue":       {Required: true},
			"base_branch": {Default: "main"},
		},
	}

	// Required variable provided, default not overridden.
	resolved, err := ResolveVariables(m, map[string]string{"issue": "sol-abc12345"})
	if err != nil {
		t.Fatalf("ResolveVariables() error: %v", err)
	}
	if resolved["issue"] != "sol-abc12345" {
		t.Errorf("issue: got %q, want %q", resolved["issue"], "sol-abc12345")
	}
	if resolved["base_branch"] != "main" {
		t.Errorf("base_branch: got %q, want %q", resolved["base_branch"], "main")
	}

	// Override default.
	resolved, err = ResolveVariables(m, map[string]string{"issue": "sol-abc12345", "base_branch": "develop"})
	if err != nil {
		t.Fatalf("ResolveVariables() error: %v", err)
	}
	if resolved["base_branch"] != "develop" {
		t.Errorf("base_branch override: got %q, want %q", resolved["base_branch"], "develop")
	}

	// Missing required variable.
	_, err = ResolveVariables(m, map[string]string{})
	if err == nil {
		t.Fatal("ResolveVariables() expected error for missing required variable")
	}
}

func TestRenderStepInstructions(t *testing.T) {
	dir := t.TempDir()
	stepsDir := filepath.Join(dir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	content := "Work on {{issue}} from {{base_branch}}. Also {{unknown}}.\n"
	os.WriteFile(filepath.Join(stepsDir, "step.md"), []byte(content), 0o644)

	step := StepDef{ID: "test", Instructions: "steps/step.md"}
	vars := map[string]string{"issue": "sol-abc12345", "base_branch": "main"}

	rendered, err := RenderStepInstructions(dir, step, vars)
	if err != nil {
		t.Fatalf("RenderStepInstructions() error: %v", err)
	}
	if rendered != "Work on sol-abc12345 from main. Also {{unknown}}.\n" {
		t.Errorf("rendered: got %q", rendered)
	}
}

func TestInstantiate(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Create workflow dir.
	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue":       {Required: true},
		"base_branch": {Default: "main"},
	})
	for _, s := range steps {
		content := "# " + s.Title + "\n\nWork on {{issue}} from {{base_branch}}.\n"
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte(content), 0o644)
	}

	// Create outpost dir.
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	// Instantiate.
	inst, state, err := Instantiate("haven", "Toast", "agent", "test-wf",
		map[string]string{"issue": "sol-abc12345"})
	if err != nil {
		t.Fatalf("Instantiate() error: %v", err)
	}

	if inst.Workflow != "test-wf" {
		t.Errorf("workflow: got %q, want %q", inst.Workflow, "test-wf")
	}
	if state.CurrentStep != "load-context" {
		t.Errorf("current_step: got %q, want %q", state.CurrentStep, "load-context")
	}
	if state.Status != "running" {
		t.Errorf("status: got %q, want %q", state.Status, "running")
	}

	// Verify files created.
	wfDir := InstanceDir("haven", "Toast", "agent")
	for _, name := range []string{"manifest.json", "state.json"} {
		if _, err := os.Stat(filepath.Join(wfDir, name)); err != nil {
			t.Errorf("missing file %q: %v", name, err)
		}
	}
	for _, s := range steps {
		if _, err := os.Stat(filepath.Join(wfDir, "steps", s.ID+".json")); err != nil {
			t.Errorf("missing step file %q: %v", s.ID+".json", err)
		}
	}

	// Verify step instructions rendered.
	stepData, err := os.ReadFile(filepath.Join(wfDir, "steps", "load-context.json"))
	if err != nil {
		t.Fatalf("read step: %v", err)
	}
	var step Step
	json.Unmarshal(stepData, &step)
	if step.Status != "executing" {
		t.Errorf("first step status: got %q, want %q", step.Status, "executing")
	}
	if step.Instructions == "" {
		t.Error("step instructions empty")
	}
	if step.StartedAt == nil {
		t.Error("step startedAt nil")
	}
}

func TestInstantiateRequiredVariableMissing(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}

	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	_, _, err := Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{})
	if err == nil {
		t.Fatal("Instantiate() expected error for missing required variable")
	}

	// Verify no directory created.
	wfDir := InstanceDir("haven", "Toast", "agent")
	if _, err := os.Stat(wfDir); !os.IsNotExist(err) {
		t.Errorf("workflow directory should not exist after error, but stat returned: %v", err)
	}
}

func TestReadState(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Non-existent workflow.
	state, err := ReadState("haven", "Ghost", "agent")
	if err != nil {
		t.Fatalf("ReadState() error: %v", err)
	}
	if state != nil {
		t.Error("ReadState() expected nil for non-existent workflow")
	}

	// Create a workflow.
	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	state, err = ReadState("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("ReadState() error: %v", err)
	}
	if state == nil {
		t.Fatal("ReadState() returned nil for existing workflow")
	}
	if state.Status != "running" {
		t.Errorf("status: got %q, want %q", state.Status, "running")
	}
}

func TestReadCurrentStep(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("Do {{issue}}"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-abc"})

	step, err := ReadCurrentStep("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("ReadCurrentStep() error: %v", err)
	}
	if step == nil {
		t.Fatal("ReadCurrentStep() returned nil")
	}
	if step.ID != "load-context" {
		t.Errorf("step ID: got %q, want %q", step.ID, "load-context")
	}
	if step.Instructions != "Do sol-abc" {
		t.Errorf("instructions: got %q, want %q", step.Instructions, "Do sol-abc")
	}
}

func TestAdvance(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	// Advance 1: load-context → implement.
	next, done, err := Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() 1 error: %v", err)
	}
	if done {
		t.Error("Advance() 1: unexpected done")
	}
	if next == nil || next.ID != "implement" {
		t.Errorf("Advance() 1: got step %v, want implement", next)
	}

	// Verify previous step marked complete.
	state, _ := ReadState("haven", "Toast", "agent")
	if len(state.Completed) != 1 || state.Completed[0] != "load-context" {
		t.Errorf("completed: got %v, want [load-context]", state.Completed)
	}

	// Advance 2: implement → verify.
	next, done, err = Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() 2 error: %v", err)
	}
	if done {
		t.Error("Advance() 2: unexpected done")
	}
	if next == nil || next.ID != "verify" {
		t.Errorf("Advance() 2: got step %v, want verify", next)
	}

	// Advance 3: verify → done.
	next, done, err = Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() 3 error: %v", err)
	}
	if !done {
		t.Error("Advance() 3: expected done")
	}
	if next != nil {
		t.Errorf("Advance() 3: got step %v, want nil", next)
	}

	// Verify final state.
	state, _ = ReadState("haven", "Toast", "agent")
	if state.Status != "done" {
		t.Errorf("final status: got %q, want %q", state.Status, "done")
	}
	if state.CurrentStep != "" {
		t.Errorf("final current_step: got %q, want empty", state.CurrentStep)
	}
	if state.CompletedAt == nil {
		t.Error("completedAt nil")
	}
}

func TestAdvanceDAG(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// DAG: A (no deps), B needs A, C needs A, D needs B and C.
	dagSteps := []StepDef{
		{ID: "a", Title: "Step A", Instructions: "steps/a.md"},
		{ID: "b", Title: "Step B", Instructions: "steps/b.md", Needs: []string{"a"}},
		{ID: "c", Title: "Step C", Instructions: "steps/c.md", Needs: []string{"a"}},
		{ID: "d", Title: "Step D", Instructions: "steps/d.md", Needs: []string{"b", "c"}},
	}

	workflowDir := filepath.Join(solHome, "workflows", "dag-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	writeTOMLManifest(t, workflowDir, "dag-wf", dagSteps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range dagSteps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	_, state, err := Instantiate("haven", "Toast", "agent", "dag-wf", map[string]string{"issue": "sol-test"})
	if err != nil {
		t.Fatalf("Instantiate() error: %v", err)
	}
	if state.CurrentStep != "a" {
		t.Fatalf("initial step: got %q, want %q", state.CurrentStep, "a")
	}

	// After A: both B and C ready, pick B (first in manifest order).
	next, done, err := Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() after A error: %v", err)
	}
	if done {
		t.Error("unexpected done after A")
	}
	if next.ID != "b" {
		t.Errorf("after A: got %q, want %q", next.ID, "b")
	}

	// After B: C ready (D still needs C).
	next, done, err = Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() after B error: %v", err)
	}
	if done {
		t.Error("unexpected done after B")
	}
	if next.ID != "c" {
		t.Errorf("after B: got %q, want %q", next.ID, "c")
	}

	// After C: D ready (B and C both done).
	next, done, err = Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() after C error: %v", err)
	}
	if done {
		t.Error("unexpected done after C")
	}
	if next.ID != "d" {
		t.Errorf("after C: got %q, want %q", next.ID, "d")
	}

	// After D: done.
	next, done, err = Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() after D error: %v", err)
	}
	if !done {
		t.Error("expected done after D")
	}
	if next != nil {
		t.Errorf("expected nil step after done, got %v", next)
	}
}

func TestNextReadySteps(t *testing.T) {
	steps := []StepDef{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B", Needs: []string{"a"}},
		{ID: "c", Title: "C", Needs: []string{"a"}},
		{ID: "d", Title: "D", Needs: []string{"b", "c"}},
	}

	// Nothing completed: only A is ready.
	ready := NextReadySteps(steps, nil)
	if len(ready) != 1 || ready[0] != "a" {
		t.Errorf("none completed: got %v, want [a]", ready)
	}

	// A completed: B and C ready.
	ready = NextReadySteps(steps, []string{"a"})
	if len(ready) != 2 || ready[0] != "b" || ready[1] != "c" {
		t.Errorf("A completed: got %v, want [b, c]", ready)
	}

	// A, B completed: C ready, D not yet.
	ready = NextReadySteps(steps, []string{"a", "b"})
	if len(ready) != 1 || ready[0] != "c" {
		t.Errorf("A,B completed: got %v, want [c]", ready)
	}

	// A, B, C completed: D ready.
	ready = NextReadySteps(steps, []string{"a", "b", "c"})
	if len(ready) != 1 || ready[0] != "d" {
		t.Errorf("A,B,C completed: got %v, want [d]", ready)
	}

	// All completed: none ready.
	ready = NextReadySteps(steps, []string{"a", "b", "c", "d"})
	if len(ready) != 0 {
		t.Errorf("all completed: got %v, want []", ready)
	}
}

func TestSkipMidWorkflow(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	// Skip first step (load-context) → should advance to implement.
	next, done, err := Skip("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Skip() error: %v", err)
	}
	if done {
		t.Error("Skip(): unexpected done")
	}
	if next == nil || next.ID != "implement" {
		t.Errorf("Skip(): got step %v, want implement", next)
	}

	// Verify skipped step status.
	wfDir := InstanceDir("haven", "Toast", "agent")
	stepData, err := os.ReadFile(filepath.Join(wfDir, "steps", "load-context.json"))
	if err != nil {
		t.Fatalf("read skipped step: %v", err)
	}
	var skippedStep Step
	json.Unmarshal(stepData, &skippedStep)
	if skippedStep.Status != "skipped" {
		t.Errorf("skipped step status: got %q, want %q", skippedStep.Status, "skipped")
	}
	if skippedStep.CompletedAt == nil {
		t.Error("skipped step completedAt nil")
	}

	// Verify state: load-context in completed list.
	state, _ := ReadState("haven", "Toast", "agent")
	if len(state.Completed) != 1 || state.Completed[0] != "load-context" {
		t.Errorf("completed: got %v, want [load-context]", state.Completed)
	}
	if state.CurrentStep != "implement" {
		t.Errorf("current step: got %q, want %q", state.CurrentStep, "implement")
	}
	if state.Status != "running" {
		t.Errorf("status: got %q, want %q", state.Status, "running")
	}
}

func TestSkipLastStep(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	// Advance through first two steps normally.
	Advance("haven", "Toast", "agent") // load-context → implement
	Advance("haven", "Toast", "agent") // implement → verify

	// Skip last step (verify) → workflow should complete.
	next, done, err := Skip("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Skip() last step error: %v", err)
	}
	if !done {
		t.Error("Skip() last step: expected done")
	}
	if next != nil {
		t.Errorf("Skip() last step: got step %v, want nil", next)
	}

	// Verify final state.
	state, _ := ReadState("haven", "Toast", "agent")
	if state.Status != "done" {
		t.Errorf("final status: got %q, want %q", state.Status, "done")
	}
	if state.CurrentStep != "" {
		t.Errorf("final current_step: got %q, want empty", state.CurrentStep)
	}
	if state.CompletedAt == nil {
		t.Error("completedAt nil")
	}
}

func TestSkipWithDependents(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// DAG: A (no deps), B needs A, C needs A, D needs B and C.
	dagSteps := []StepDef{
		{ID: "a", Title: "Step A", Instructions: "steps/a.md"},
		{ID: "b", Title: "Step B", Instructions: "steps/b.md", Needs: []string{"a"}},
		{ID: "c", Title: "Step C", Instructions: "steps/c.md", Needs: []string{"a"}},
		{ID: "d", Title: "Step D", Instructions: "steps/d.md", Needs: []string{"b", "c"}},
	}

	workflowDir := filepath.Join(solHome, "workflows", "dag-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	writeTOMLManifest(t, workflowDir, "dag-wf", dagSteps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range dagSteps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "dag-wf", map[string]string{"issue": "sol-test"})

	// Skip A → B and C should become ready, B picked first.
	next, done, err := Skip("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Skip() A error: %v", err)
	}
	if done {
		t.Error("unexpected done after skipping A")
	}
	if next.ID != "b" {
		t.Errorf("after skip A: got %q, want %q", next.ID, "b")
	}

	// Skip B → C should become ready.
	next, done, err = Skip("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Skip() B error: %v", err)
	}
	if done {
		t.Error("unexpected done after skipping B")
	}
	if next.ID != "c" {
		t.Errorf("after skip B: got %q, want %q", next.ID, "c")
	}

	// Advance C normally → D should become ready (B was skipped, C completed).
	next, done, err = Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() C error: %v", err)
	}
	if done {
		t.Error("unexpected done after C")
	}
	if next.ID != "d" {
		t.Errorf("after C: got %q, want %q", next.ID, "d")
	}

	// Verify skipped steps have correct status.
	wfDir := InstanceDir("haven", "Toast", "agent")
	for _, id := range []string{"a", "b"} {
		data, _ := os.ReadFile(filepath.Join(wfDir, "steps", id+".json"))
		var s Step
		json.Unmarshal(data, &s)
		if s.Status != "skipped" {
			t.Errorf("step %q status: got %q, want %q", id, s.Status, "skipped")
		}
	}
}

func TestFail(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	// Fail the first step.
	failedStep, err := Fail("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Fail() error: %v", err)
	}
	if failedStep == nil || failedStep.ID != "load-context" {
		t.Errorf("Fail(): got step %v, want load-context", failedStep)
	}
	if failedStep.Status != "failed" {
		t.Errorf("failed step status: got %q, want %q", failedStep.Status, "failed")
	}

	// Verify state.
	state, _ := ReadState("haven", "Toast", "agent")
	if state.Status != "failed" {
		t.Errorf("workflow status: got %q, want %q", state.Status, "failed")
	}
	// Current step should still be set (not cleared).
	if state.CurrentStep != "load-context" {
		t.Errorf("current step: got %q, want %q", state.CurrentStep, "load-context")
	}
	// Step should NOT be in completed list.
	if len(state.Completed) != 0 {
		t.Errorf("completed: got %v, want empty", state.Completed)
	}
	if state.CompletedAt == nil {
		t.Error("completedAt nil after fail")
	}

	// Verify step file has failed status.
	wfDir := InstanceDir("haven", "Toast", "agent")
	stepData, _ := os.ReadFile(filepath.Join(wfDir, "steps", "load-context.json"))
	var step Step
	json.Unmarshal(stepData, &step)
	if step.Status != "failed" {
		t.Errorf("step file status: got %q, want %q", step.Status, "failed")
	}
}

func TestFailNoWorkflow(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	_, err := Fail("haven", "Ghost", "agent")
	if err == nil {
		t.Fatal("Fail() expected error for non-existent workflow")
	}
	if !strings.Contains(err.Error(), "no workflow found") {
		t.Errorf("error: got %q, want containing 'no workflow found'", err.Error())
	}
}

func TestSkipNoWorkflow(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	_, _, err := Skip("haven", "Ghost", "agent")
	if err == nil {
		t.Fatal("Skip() expected error for non-existent workflow")
	}
	if !strings.Contains(err.Error(), "no workflow found") {
		t.Errorf("error: got %q, want containing 'no workflow found'", err.Error())
	}
}

func TestFailAlreadyFailed(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	// Fail once.
	Fail("haven", "Toast", "agent")

	// Fail again — should error because workflow is already failed.
	_, err := Fail("haven", "Toast", "agent")
	if err == nil {
		t.Fatal("Fail() expected error for already-failed workflow")
	}
	if !strings.Contains(err.Error(), "workflow status is \"failed\"") {
		t.Errorf("error: got %q", err.Error())
	}
}

func TestSkipAfterFail(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	// Fail the workflow.
	Fail("haven", "Toast", "agent")

	// Skip should error because workflow is already failed.
	_, _, err := Skip("haven", "Toast", "agent")
	if err == nil {
		t.Fatal("Skip() expected error after workflow failed")
	}
	if !strings.Contains(err.Error(), "workflow status is \"failed\"") {
		t.Errorf("error: got %q", err.Error())
	}
}

func TestRemove(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	wfDir := InstanceDir("haven", "Toast", "agent")
	if _, err := os.Stat(wfDir); err != nil {
		t.Fatalf("workflow dir should exist: %v", err)
	}

	if err := Remove("haven", "Toast", "agent"); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if _, err := os.Stat(wfDir); !os.IsNotExist(err) {
		t.Errorf("workflow dir should not exist after Remove, stat: %v", err)
	}
}

func TestResolve(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Known workflow not on disk → extracted (embedded tier).
	res, err := Resolve("default-work", "")
	if err != nil {
		t.Fatalf("Resolve(default-work) error: %v", err)
	}
	if res.Tier != TierEmbedded {
		t.Errorf("tier: got %q, want %q", res.Tier, TierEmbedded)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "steps", "01-load-context.md")); err != nil {
		t.Errorf("step file not found after extraction: %v", err)
	}

	// Already exists → user tier (extracted to $SOL_HOME/workflows/).
	res2, err := Resolve("default-work", "")
	if err != nil {
		t.Fatalf("Resolve(default-work) second call error: %v", err)
	}
	if res.Path != res2.Path {
		t.Errorf("paths differ: %q vs %q", res.Path, res2.Path)
	}
	if res2.Tier != TierUser {
		t.Errorf("second call tier: got %q, want %q", res2.Tier, TierUser)
	}

	// Unknown workflow → error.
	_, err = Resolve("nonexistent-workflow", "")
	if err == nil {
		t.Fatal("Resolve(nonexistent) expected error")
	}
}

func TestResolveProjectTier(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	repoDir := t.TempDir()

	// Create a project-level workflow.
	projectWF := filepath.Join(repoDir, ".sol", "workflows", "custom-deploy")
	if err := os.MkdirAll(projectWF, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte("[workflow]\nname = \"custom-deploy\"\ntype = \"workflow\"\n")
	if err := os.WriteFile(filepath.Join(projectWF, "manifest.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	// Project tier resolves first.
	res, err := Resolve("custom-deploy", repoDir)
	if err != nil {
		t.Fatalf("Resolve(custom-deploy) error: %v", err)
	}
	if res.Tier != TierProject {
		t.Errorf("tier: got %q, want %q", res.Tier, TierProject)
	}
	if res.Path != projectWF {
		t.Errorf("path: got %q, want %q", res.Path, projectWF)
	}
}

func TestResolveProjectOverridesUser(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	repoDir := t.TempDir()

	// Create both project-level and user-level workflows with the same name.
	projectDir := filepath.Join(repoDir, ".sol", "workflows", "default-work")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "manifest.toml"), []byte("# project"), 0o644); err != nil {
		t.Fatal(err)
	}

	userDir := Dir("default-work")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "manifest.toml"), []byte("# user"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Project tier wins over user tier.
	res, err := Resolve("default-work", repoDir)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if res.Tier != TierProject {
		t.Errorf("tier: got %q, want %q", res.Tier, TierProject)
	}
	if res.Path != projectDir {
		t.Errorf("path: got %q, want %q", res.Path, projectDir)
	}

	// Without repoPath, user tier wins.
	res2, err := Resolve("default-work", "")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if res2.Tier != TierUser {
		t.Errorf("tier without repo: got %q, want %q", res2.Tier, TierUser)
	}
}

func TestAdvanceIdempotentOnCompletedStep(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	workflowDir := filepath.Join(solHome, "workflows", "test-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, workflowDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	// Advance 1: load-context → implement (normal advance).
	next, done, err := Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() 1 error: %v", err)
	}
	if done || next == nil || next.ID != "implement" {
		t.Fatalf("Advance() 1: expected implement, got done=%v step=%v", done, next)
	}

	// Verify step 1 appears exactly once in Completed.
	state, err := ReadState("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("ReadState() error: %v", err)
	}
	if len(state.Completed) != 1 || state.Completed[0] != "load-context" {
		t.Fatalf("completed after advance 1: got %v, want [load-context]", state.Completed)
	}

	// Simulate crash recovery: rewrite state.json to set CurrentStep back to
	// "load-context" (the step file is already marked complete, but the state
	// wasn't fully committed before the crash).
	state.CurrentStep = "load-context"
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	statePath := filepath.Join(InstanceDir("haven", "Toast", "agent"), "state.json")
	if err := os.WriteFile(statePath, stateData, 0o644); err != nil {
		t.Fatalf("write state.json: %v", err)
	}

	// Call Advance() again — should recover without duplicating "load-context".
	next, done, err = Advance("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("Advance() recovery error: %v", err)
	}
	if done {
		t.Fatal("Advance() recovery: unexpected done")
	}
	if next == nil || next.ID != "implement" {
		t.Fatalf("Advance() recovery: expected implement, got %v", next)
	}

	// Verify "load-context" still appears exactly ONCE (not duplicated).
	state, err = ReadState("haven", "Toast", "agent")
	if err != nil {
		t.Fatalf("ReadState() after recovery error: %v", err)
	}
	count := 0
	for _, c := range state.Completed {
		if c == "load-context" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("load-context appears %d times in Completed (want 1): %v", count, state.Completed)
	}
}

func TestLoadManifestExpansion(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "test-expansion"
type = "expansion"
description = "Test expansion workflow"

[[template]]
id = "{target}.draft"
title = "Draft: {target.title}"
description = "First pass."

[[template]]
id = "{target}.refine"
title = "Refine: {target.title}"
description = "Second pass."
needs = ["{target}.draft"]
`
	os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(toml), 0o644)

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadMaterialize() error: %v", err)
	}

	if m.Type != "expansion" {
		t.Errorf("type: got %q, want %q", m.Type, "expansion")
	}
	if len(m.Templates) != 2 {
		t.Fatalf("templates: got %d, want 2", len(m.Templates))
	}
	if m.Templates[0].ID != "{target}.draft" {
		t.Errorf("template[0].ID: got %q, want %q", m.Templates[0].ID, "{target}.draft")
	}
	if m.Templates[1].Description != "Second pass." {
		t.Errorf("template[1].Description: got %q", m.Templates[1].Description)
	}
	if len(m.Templates[1].Needs) != 1 || m.Templates[1].Needs[0] != "{target}.draft" {
		t.Errorf("template[1].Needs: got %v, want [{target}.draft]", m.Templates[1].Needs)
	}
	if len(m.Steps) != 0 {
		t.Errorf("steps should be empty for expansion, got %d", len(m.Steps))
	}
}

func TestValidateExpansionValid(t *testing.T) {
	m := &Manifest{
		Type: "expansion",
		Templates: []Template{
			{ID: "draft", Title: "Draft"},
			{ID: "refine", Title: "Refine", Needs: []string{"draft"}},
		},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}

func TestValidateExpansionDuplicateID(t *testing.T) {
	m := &Manifest{
		Type: "expansion",
		Templates: []Template{
			{ID: "draft", Title: "Draft"},
			{ID: "draft", Title: "Draft again"},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for duplicate template ID")
	}
	if got := err.Error(); got != `duplicate template ID "draft"` {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateExpansionMissingNeed(t *testing.T) {
	m := &Manifest{
		Type: "expansion",
		Templates: []Template{
			{ID: "draft", Title: "Draft", Needs: []string{"nonexistent"}},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for missing need")
	}
}

func TestValidateExpansionCycle(t *testing.T) {
	m := &Manifest{
		Type: "expansion",
		Templates: []Template{
			{ID: "a", Title: "A", Needs: []string{"b"}},
			{ID: "b", Title: "B", Needs: []string{"a"}},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for cycle")
	}
	if got := err.Error(); got != "dependency cycle detected in templates" {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateExpansionEmpty(t *testing.T) {
	m := &Manifest{
		Type: "expansion",
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for empty expansion")
	}
	if got := err.Error(); got != "expansion workflow requires at least one [[template]] entry" {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateExpansionWithSteps(t *testing.T) {
	m := &Manifest{
		Type: "expansion",
		Steps: []StepDef{{ID: "s1", Title: "Step 1"}},
		Templates: []Template{{ID: "t1", Title: "Template 1"}},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for expansion with steps")
	}
	if got := err.Error(); got != "expansion workflow must not contain [[steps]] entries" {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateWorkflowWithTemplates(t *testing.T) {
	m := &Manifest{
		Type: "workflow",
		Steps:     []StepDef{{ID: "s1", Title: "Step 1"}},
		Templates: []Template{{ID: "t1", Title: "Template 1"}},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for workflow with templates")
	}
	if got := err.Error(); got != `type "workflow" must not contain [[template]] entries` {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateOtherTypeWithTemplates(t *testing.T) {
	m := &Manifest{
		Type:      "agent",
		Steps:     []StepDef{{ID: "s1", Title: "Step 1"}},
		Templates: []Template{{ID: "t1", Title: "Template 1"}},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for non-expansion type with templates")
	}
	if got := err.Error(); got != `type "agent" must not contain [[template]] entries` {
		t.Errorf("error: got %q", got)
	}
}

func TestResolveRuleOfFive(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("rule-of-five", "")
	if err != nil {
		t.Fatalf("Resolve(rule-of-five) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}

	// Load and validate the extracted workflow.
	m, err := LoadManifest(res.Path)
	if err != nil {
		t.Fatalf("LoadMaterialize() error: %v", err)
	}
	if m.Type != "expansion" {
		t.Errorf("type: got %q, want %q", m.Type, "expansion")
	}
	if len(m.Templates) != 5 {
		t.Fatalf("templates: got %d, want 5", len(m.Templates))
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error on rule-of-five: %v", err)
	}

	// Second call is no-op.
	res2, err := Resolve("rule-of-five", "")
	if err != nil {
		t.Fatalf("Resolve(rule-of-five) second call error: %v", err)
	}
	if res.Path != res2.Path {
		t.Errorf("paths differ: %q vs %q", res.Path, res2.Path)
	}
}

// setupStores creates a temporary world and sphere store for testing.
func setupStores(t *testing.T) (worldStore, sphereStore *store.Store) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := store.OpenWorld("test-world")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })

	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })

	return ws, ss
}

func TestComputePhases(t *testing.T) {
	// DAG: A (no deps), B needs A, C needs A, D needs B and C.
	steps := []StepDef{
		{ID: "a", Title: "A"},
		{ID: "b", Title: "B", Needs: []string{"a"}},
		{ID: "c", Title: "C", Needs: []string{"a"}},
		{ID: "d", Title: "D", Needs: []string{"b", "c"}},
	}

	phases := ComputePhases(steps)

	if phases["a"] != 0 {
		t.Errorf("phase[a]: got %d, want 0", phases["a"])
	}
	if phases["b"] != 1 {
		t.Errorf("phase[b]: got %d, want 1", phases["b"])
	}
	if phases["c"] != 1 {
		t.Errorf("phase[c]: got %d, want 1", phases["c"])
	}
	if phases["d"] != 2 {
		t.Errorf("phase[d]: got %d, want 2", phases["d"])
	}
}

func TestComputePhasesLinear(t *testing.T) {
	steps := linearSteps() // load-context → implement → verify

	phases := ComputePhases(steps)

	if phases["load-context"] != 0 {
		t.Errorf("phase[load-context]: got %d, want 0", phases["load-context"])
	}
	if phases["implement"] != 1 {
		t.Errorf("phase[implement]: got %d, want 1", phases["implement"])
	}
	if phases["verify"] != 2 {
		t.Errorf("phase[verify]: got %d, want 2", phases["verify"])
	}
}

func TestShouldManifest(t *testing.T) {
	// Workflow without manifest flag.
	m := &Manifest{Type: "workflow"}
	if ShouldManifest(m) {
		t.Error("ShouldManifest() = true for workflow without manifest flag")
	}

	// Workflow with manifest = true.
	m = &Manifest{Type: "workflow", Manifest: true}
	if !ShouldManifest(m) {
		t.Error("ShouldManifest() = false for workflow with manifest = true")
	}

	// Expansion always manifests.
	m = &Manifest{Type: "expansion"}
	if !ShouldManifest(m) {
		t.Error("ShouldManifest() = false for expansion type")
	}
}

func TestManifestWorkflow(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create a manifested workflow.
	workflowDir := filepath.Join(solHome, "workflows", "manifest-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	steps := linearSteps()
	writeTOMLManifestWithFlag(t, workflowDir, "manifest-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	}, true)
	for _, s := range steps {
		content := "# " + s.Title + "\n\nWork on {{issue}}.\n"
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte(content), 0o644)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "manifest-wf",
		World:       "test-world",
		Variables:   map[string]string{"issue": "sol-test123"},
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify result structure.
	if result.CaravanID == "" {
		t.Error("CaravanID is empty")
	}
	if result.ParentID == "" {
		t.Error("ParentID is empty")
	}
	if len(result.ChildIDs) != 3 {
		t.Fatalf("ChildIDs: got %d, want 3", len(result.ChildIDs))
	}

	// Verify child writs were created with correct titles and parent.
	for _, stepDef := range steps {
		childID, ok := result.ChildIDs[stepDef.ID]
		if !ok {
			t.Fatalf("missing child for step %q", stepDef.ID)
		}
		item, err := ws.GetWrit(childID)
		if err != nil {
			t.Fatalf("GetWrit(%q) error: %v", childID, err)
		}
		if item.Title != stepDef.Title {
			t.Errorf("step %q title: got %q, want %q", stepDef.ID, item.Title, stepDef.Title)
		}
		if item.ParentID != result.ParentID {
			t.Errorf("step %q parent_id: got %q, want %q", stepDef.ID, item.ParentID, result.ParentID)
		}
		if item.Status != "open" {
			t.Errorf("step %q status: got %q, want open", stepDef.ID, item.Status)
		}
	}

	// Verify rendered instructions in description.
	loadItem, _ := ws.GetWrit(result.ChildIDs["load-context"])
	if loadItem.Description == "" {
		t.Error("load-context description is empty")
	}
	if !strings.Contains(loadItem.Description, "sol-test123") {
		t.Errorf("load-context description missing variable substitution: %q", loadItem.Description)
	}

	// Verify dependencies mirror the DAG.
	// implement depends on load-context.
	implDeps, err := ws.GetDependencies(result.ChildIDs["implement"])
	if err != nil {
		t.Fatalf("GetDependencies(implement) error: %v", err)
	}
	if len(implDeps) != 1 || implDeps[0] != result.ChildIDs["load-context"] {
		t.Errorf("implement deps: got %v, want [%s]", implDeps, result.ChildIDs["load-context"])
	}

	// verify depends on implement.
	verifyDeps, err := ws.GetDependencies(result.ChildIDs["verify"])
	if err != nil {
		t.Fatalf("GetDependencies(verify) error: %v", err)
	}
	if len(verifyDeps) != 1 || verifyDeps[0] != result.ChildIDs["implement"] {
		t.Errorf("verify deps: got %v, want [%s]", verifyDeps, result.ChildIDs["implement"])
	}

	// load-context has no deps.
	loadDeps, err := ws.GetDependencies(result.ChildIDs["load-context"])
	if err != nil {
		t.Fatalf("GetDependencies(load-context) error: %v", err)
	}
	if len(loadDeps) != 0 {
		t.Errorf("load-context deps: got %v, want []", loadDeps)
	}

	// Verify phases.
	if result.Phases["load-context"] != 0 {
		t.Errorf("phase[load-context]: got %d, want 0", result.Phases["load-context"])
	}
	if result.Phases["implement"] != 1 {
		t.Errorf("phase[implement]: got %d, want 1", result.Phases["implement"])
	}
	if result.Phases["verify"] != 2 {
		t.Errorf("phase[verify]: got %d, want 2", result.Phases["verify"])
	}

	// Verify caravan was created and is ready.
	caravan, err := ss.GetCaravan(result.CaravanID)
	if err != nil {
		t.Fatalf("GetCaravan() error: %v", err)
	}
	if caravan.Status != "ready" {
		t.Errorf("caravan status: got %q, want ready", caravan.Status)
	}

	// Verify caravan items.
	items, err := ss.ListCaravanItems(result.CaravanID)
	if err != nil {
		t.Fatalf("ListCaravanItems() error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("caravan items: got %d, want 3", len(items))
	}

	// Verify caravan item phases match.
	itemPhases := make(map[string]int)
	for _, ci := range items {
		itemPhases[ci.WritID] = ci.Phase
	}
	for stepID, writID := range result.ChildIDs {
		expectedPhase := result.Phases[stepID]
		gotPhase := itemPhases[writID]
		if gotPhase != expectedPhase {
			t.Errorf("caravan item phase for %q: got %d, want %d", stepID, gotPhase, expectedPhase)
		}
	}
}

func TestManifestExpansion(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create expansion workflow.
	workflowDir := filepath.Join(solHome, "workflows", "test-expand")
	os.MkdirAll(workflowDir, 0o755)

	toml := `name = "test-expand"
type = "expansion"
description = "Test expansion workflow"

[[template]]
id = "draft"
title = "Draft: {target.title}"
description = "Initial attempt at {target.title}."

[[template]]
id = "refine"
title = "Refine: {target.title}"
description = "Second pass on {target.id}."
needs = ["draft"]
`
	os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(toml), 0o644)

	// Create a target writ.
	targetID, err := ws.CreateWrit("Build auth system", "Implement OAuth2", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit() error: %v", err)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-expand",
		World:       "test-world",
		ParentID:    targetID,
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify children.
	if len(result.ChildIDs) != 2 {
		t.Fatalf("ChildIDs: got %d, want 2", len(result.ChildIDs))
	}

	// Verify template variable substitution in titles.
	draftItem, err := ws.GetWrit(result.ChildIDs["draft"])
	if err != nil {
		t.Fatalf("GetWrit(draft) error: %v", err)
	}
	if draftItem.Title != "Draft: Build auth system" {
		t.Errorf("draft title: got %q, want %q", draftItem.Title, "Draft: Build auth system")
	}
	if draftItem.Description != "Initial attempt at Build auth system." {
		t.Errorf("draft description: got %q", draftItem.Description)
	}

	refineItem, err := ws.GetWrit(result.ChildIDs["refine"])
	if err != nil {
		t.Fatalf("GetWrit(refine) error: %v", err)
	}
	if refineItem.Title != "Refine: Build auth system" {
		t.Errorf("refine title: got %q, want %q", refineItem.Title, "Refine: Build auth system")
	}
	if !strings.Contains(refineItem.Description, targetID) {
		t.Errorf("refine description missing target ID: got %q", refineItem.Description)
	}

	// Verify parent is the target.
	if result.ParentID != targetID {
		t.Errorf("ParentID: got %q, want %q", result.ParentID, targetID)
	}
	if draftItem.ParentID != targetID {
		t.Errorf("draft parent_id: got %q, want %q", draftItem.ParentID, targetID)
	}

	// Verify dependencies: refine depends on draft.
	refineDeps, err := ws.GetDependencies(result.ChildIDs["refine"])
	if err != nil {
		t.Fatalf("GetDependencies(refine) error: %v", err)
	}
	if len(refineDeps) != 1 || refineDeps[0] != result.ChildIDs["draft"] {
		t.Errorf("refine deps: got %v, want [%s]", refineDeps, result.ChildIDs["draft"])
	}

	// Verify phases.
	if result.Phases["draft"] != 0 {
		t.Errorf("phase[draft]: got %d, want 0", result.Phases["draft"])
	}
	if result.Phases["refine"] != 1 {
		t.Errorf("phase[refine]: got %d, want 1", result.Phases["refine"])
	}
}

func TestManifestExpansionRequiresParent(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "test-expand")
	os.MkdirAll(workflowDir, 0o755)

	toml := `name = "test-expand"
type = "expansion"
description = "Test expansion workflow"

[[template]]
id = "draft"
title = "Draft"
description = "First pass."
`
	os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(toml), 0o644)

	_, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-expand",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err == nil {
		t.Fatal("Materialize() expected error for expansion without parent")
	}
	if !strings.Contains(err.Error(), "requires a parent writ") {
		t.Errorf("error: got %q", err.Error())
	}
}

func TestManifestRejectsNonManifest(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "plain-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	steps := []StepDef{{ID: "s1", Title: "Step 1", Instructions: "steps/s1.md"}}
	writeTOMLManifest(t, workflowDir, "plain-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	os.WriteFile(filepath.Join(workflowDir, "steps", "s1.md"), []byte("test"), 0o644)

	_, err := Materialize(ws, ss, ManifestOpts{
		Name: "plain-wf",
		World:       "test-world",
		Variables:   map[string]string{"issue": "sol-test"},
		CreatedBy:   "autarch",
	})
	if err == nil {
		t.Fatal("Materialize() expected error for non-manifest workflow")
	}
	if !strings.Contains(err.Error(), "not configured for manifestation") {
		t.Errorf("error: got %q", err.Error())
	}
}

func TestManifestDAGPhases(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// DAG: A (no deps), B needs A, C needs A, D needs B and C.
	dagSteps := []StepDef{
		{ID: "a", Title: "Step A", Instructions: "steps/a.md"},
		{ID: "b", Title: "Step B", Instructions: "steps/b.md", Needs: []string{"a"}},
		{ID: "c", Title: "Step C", Instructions: "steps/c.md", Needs: []string{"a"}},
		{ID: "d", Title: "Step D", Instructions: "steps/d.md", Needs: []string{"b", "c"}},
	}

	workflowDir := filepath.Join(solHome, "workflows", "dag-manifest")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)
	writeTOMLManifestWithFlag(t, workflowDir, "dag-manifest", dagSteps, map[string]VariableDecl{
		"issue": {Required: true},
	}, true)
	for _, s := range dagSteps {
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte("test"), 0o644)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "dag-manifest",
		World:       "test-world",
		Variables:   map[string]string{"issue": "sol-dag-test"},
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify 4 children.
	if len(result.ChildIDs) != 4 {
		t.Fatalf("ChildIDs: got %d, want 4", len(result.ChildIDs))
	}

	// Verify phases: A=0, B=1, C=1, D=2.
	expected := map[string]int{"a": 0, "b": 1, "c": 1, "d": 2}
	for id, wantPhase := range expected {
		if result.Phases[id] != wantPhase {
			t.Errorf("phase[%s]: got %d, want %d", id, result.Phases[id], wantPhase)
		}
	}

	// Verify D depends on both B and C.
	dDeps, err := ws.GetDependencies(result.ChildIDs["d"])
	if err != nil {
		t.Fatalf("GetDependencies(d) error: %v", err)
	}
	if len(dDeps) != 2 {
		t.Fatalf("d deps: got %d, want 2", len(dDeps))
	}
	depSet := map[string]bool{dDeps[0]: true, dDeps[1]: true}
	if !depSet[result.ChildIDs["b"]] || !depSet[result.ChildIDs["c"]] {
		t.Errorf("d deps: got %v, want [%s, %s]", dDeps, result.ChildIDs["b"], result.ChildIDs["c"])
	}
}

func TestManifestWithExistingParent(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "parent-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	steps := []StepDef{
		{ID: "only-step", Title: "The only step", Instructions: "steps/only.md"},
	}
	writeTOMLManifestWithFlag(t, workflowDir, "parent-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	}, true)
	os.WriteFile(filepath.Join(workflowDir, "steps", "only.md"), []byte("Do the thing."), 0o644)

	// Create parent first.
	parentID, err := ws.CreateWrit("Parent item", "Top-level work", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit() error: %v", err)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "parent-wf",
		World:       "test-world",
		ParentID:    parentID,
		Variables:   map[string]string{"issue": "sol-test"},
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify parent is the provided one.
	if result.ParentID != parentID {
		t.Errorf("ParentID: got %q, want %q", result.ParentID, parentID)
	}

	// Verify child's parent.
	child, err := ws.GetWrit(result.ChildIDs["only-step"])
	if err != nil {
		t.Fatalf("GetWrit() error: %v", err)
	}
	if child.ParentID != parentID {
		t.Errorf("child parent_id: got %q, want %q", child.ParentID, parentID)
	}
}

func TestLoadManifestWithManifestFlag(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "flagged-wf"
type = "workflow"
manifest = true
description = "A manifested workflow"

[[steps]]
id = "s1"
title = "Step 1"
instructions = "steps/s1.md"
`
	os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(toml), 0o644)

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadMaterialize() error: %v", err)
	}
	if !m.Manifest {
		t.Error("Manifest field should be true")
	}
	if !ShouldManifest(m) {
		t.Error("ShouldMaterialize() should return true")
	}
}

// writeTOMLManifestWithFlag writes a manifest.toml with the manifest flag.
func writeTOMLManifestWithFlag(t *testing.T, dir, name string, steps []StepDef, vars map[string]VariableDecl, manifest bool) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, "manifest.toml"))
	if err != nil {
		t.Fatalf("create manifest.toml: %v", err)
	}
	defer f.Close()

	f.WriteString("name = \"" + name + "\"\n")
	f.WriteString("type = \"workflow\"\n")
	if manifest {
		f.WriteString("manifest = true\n")
	}
	f.WriteString("description = \"Test workflow\"\n\n")

	if len(vars) > 0 {
		f.WriteString("[variables]\n")
		for k, v := range vars {
			if v.Required {
				f.WriteString(k + " = { required = true }\n")
			} else if v.Default != "" {
				f.WriteString(k + " = { default = \"" + v.Default + "\" }\n")
			}
		}
		f.WriteString("\n")
	}

	for _, s := range steps {
		f.WriteString("[[steps]]\n")
		f.WriteString("id = \"" + s.ID + "\"\n")
		f.WriteString("title = \"" + s.Title + "\"\n")
		f.WriteString("instructions = \"" + s.Instructions + "\"\n")
		if len(s.Needs) > 0 {
			f.WriteString("needs = [")
			for i, n := range s.Needs {
				if i > 0 {
					f.WriteString(", ")
				}
				f.WriteString("\"" + n + "\"")
			}
			f.WriteString("]\n")
		}
		f.WriteString("\n")
	}
}

// writeTOMLConvoyManifest writes a convoy manifest.toml file for testing.
// writeTOMLConvoyManifestOpts holds optional sections for convoy manifest generation.
type writeTOMLConvoyManifestOpts struct {
	vars map[string]VariableDecl
}

func writeTOMLConvoyManifest(t *testing.T, dir, name string, legs []Leg, synth *Synthesis, opts ...writeTOMLConvoyManifestOpts) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, "manifest.toml"))
	if err != nil {
		t.Fatalf("create manifest.toml: %v", err)
	}
	defer f.Close()

	f.WriteString("name = \"" + name + "\"\n")
	f.WriteString("type = \"convoy\"\n")
	f.WriteString("description = \"Test convoy workflow\"\n\n")

	if len(opts) > 0 && len(opts[0].vars) > 0 {
		f.WriteString("[vars]\n")
		for k, v := range opts[0].vars {
			if v.Required {
				f.WriteString(k + " = { required = true }\n")
			} else if v.Default != "" {
				f.WriteString(k + " = { default = \"" + v.Default + "\" }\n")
			}
		}
		f.WriteString("\n")
	}

	for _, leg := range legs {
		f.WriteString("[[legs]]\n")
		f.WriteString("id = \"" + leg.ID + "\"\n")
		f.WriteString("title = \"" + leg.Title + "\"\n")
		f.WriteString("description = \"" + leg.Description + "\"\n")
		if leg.Focus != "" {
			f.WriteString("focus = \"" + leg.Focus + "\"\n")
		}
		if leg.Kind != "" {
			f.WriteString("kind = \"" + leg.Kind + "\"\n")
		}
		if leg.Instructions != "" {
			f.WriteString("instructions = \"" + leg.Instructions + "\"\n")
		}
		f.WriteString("\n")
	}

	if synth != nil {
		f.WriteString("[synthesis]\n")
		f.WriteString("title = \"" + synth.Title + "\"\n")
		f.WriteString("description = \"" + synth.Description + "\"\n")
		if synth.Kind != "" {
			f.WriteString("kind = \"" + synth.Kind + "\"\n")
		}
		if synth.Instructions != "" {
			f.WriteString("instructions = \"" + synth.Instructions + "\"\n")
		}
		if len(synth.DependsOn) > 0 {
			f.WriteString("depends_on = [")
			for i, dep := range synth.DependsOn {
				if i > 0 {
					f.WriteString(", ")
				}
				f.WriteString("\"" + dep + "\"")
			}
			f.WriteString("]\n")
		}
	}
}

// writeTOMLManifest writes a manifest.toml file for testing.
func writeTOMLManifest(t *testing.T, dir, name string, steps []StepDef, vars map[string]VariableDecl) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, "manifest.toml"))
	if err != nil {
		t.Fatalf("create manifest.toml: %v", err)
	}
	defer f.Close()

	f.WriteString("name = \"" + name + "\"\n")
	f.WriteString("type = \"workflow\"\n")
	f.WriteString("description = \"Test workflow\"\n\n")

	if len(vars) > 0 {
		f.WriteString("[variables]\n")
		for k, v := range vars {
			if v.Required {
				f.WriteString(k + " = { required = true }\n")
			} else if v.Default != "" {
				f.WriteString(k + " = { default = \"" + v.Default + "\" }\n")
			}
		}
		f.WriteString("\n")
	}

	for _, s := range steps {
		f.WriteString("[[steps]]\n")
		f.WriteString("id = \"" + s.ID + "\"\n")
		f.WriteString("title = \"" + s.Title + "\"\n")
		f.WriteString("instructions = \"" + s.Instructions + "\"\n")
		if len(s.Needs) > 0 {
			f.WriteString("needs = [")
			for i, n := range s.Needs {
				if i > 0 {
					f.WriteString(", ")
				}
				f.WriteString("\"" + n + "\"")
			}
			f.WriteString("]\n")
		}
		if s.Kind != "" {
			f.WriteString("kind = \"" + s.Kind + "\"\n")
		}
		f.WriteString("\n")
	}
}

// --- Convoy tests ---

func testLegs() []Leg {
	return []Leg{
		{ID: "requirements", Title: "Requirements Analysis", Description: "Assess requirements.", Focus: "Are success criteria defined?"},
		{ID: "feasibility", Title: "Feasibility Assessment", Description: "Evaluate feasibility.", Focus: "Is this buildable?"},
	}
}

func testSynthesis() *Synthesis {
	return &Synthesis{
		Title:       "Consolidate Findings",
		Description: "Combine all analyses.",
		DependsOn:   []string{"requirements", "feasibility"},
	}
}

func TestLoadManifestConvoy(t *testing.T) {
	dir := t.TempDir()
	writeTOMLConvoyManifest(t, dir, "test-convoy", testLegs(), testSynthesis())

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadMaterialize() error: %v", err)
	}

	if m.Type != "convoy" {
		t.Errorf("type: got %q, want %q", m.Type, "convoy")
	}
	if len(m.Legs) != 2 {
		t.Fatalf("legs: got %d, want 2", len(m.Legs))
	}
	if m.Legs[0].ID != "requirements" {
		t.Errorf("leg[0].ID: got %q, want %q", m.Legs[0].ID, "requirements")
	}
	if m.Legs[1].Focus != "Is this buildable?" {
		t.Errorf("leg[1].Focus: got %q", m.Legs[1].Focus)
	}
	if m.Synth == nil {
		t.Fatal("synthesis is nil")
	}
	if m.Synth.Title != "Consolidate Findings" {
		t.Errorf("synthesis.Title: got %q", m.Synth.Title)
	}
	if len(m.Synth.DependsOn) != 2 {
		t.Fatalf("synthesis.DependsOn: got %v, want 2 entries", m.Synth.DependsOn)
	}
	if len(m.Steps) != 0 {
		t.Errorf("steps should be empty for convoy, got %d", len(m.Steps))
	}
	if len(m.Templates) != 0 {
		t.Errorf("templates should be empty for convoy, got %d", len(m.Templates))
	}
}

func TestValidateConvoyValid(t *testing.T) {
	m := &Manifest{
		Type:  "convoy",
		Legs:  testLegs(),
		Synth: testSynthesis(),
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
}

func TestValidateConvoyNoLegs(t *testing.T) {
	m := &Manifest{
		Type:  "convoy",
		Synth: testSynthesis(),
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for convoy without legs")
	}
	if got := err.Error(); got != "convoy workflow requires at least one [[legs]] entry" {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateConvoyNoSynthesis(t *testing.T) {
	m := &Manifest{
		Type: "convoy",
		Legs: testLegs(),
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for convoy without synthesis")
	}
	if got := err.Error(); got != "convoy workflow requires a [synthesis] section" {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateConvoyDuplicateLegID(t *testing.T) {
	m := &Manifest{
		Type: "convoy",
		Legs: []Leg{
			{ID: "dup", Title: "First"},
			{ID: "dup", Title: "Second"},
		},
		Synth: &Synthesis{Title: "Synth", DependsOn: []string{"dup"}},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for duplicate leg ID")
	}
	if got := err.Error(); got != `duplicate leg ID "dup"` {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateConvoySynthesisBadDependsOn(t *testing.T) {
	m := &Manifest{
		Type: "convoy",
		Legs: testLegs(),
		Synth: &Synthesis{
			Title:     "Synth",
			DependsOn: []string{"requirements", "nonexistent"},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for bad depends_on")
	}
	if got := err.Error(); got != `synthesis depends_on references unknown leg "nonexistent"` {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateConvoyWithSteps(t *testing.T) {
	m := &Manifest{
		Type:  "convoy",
		Legs:  testLegs(),
		Synth: testSynthesis(),
		Steps: []StepDef{{ID: "s1", Title: "Step 1"}},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for convoy with steps")
	}
	if got := err.Error(); got != "convoy workflow must not contain [[steps]] entries" {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateConvoyWithTemplates(t *testing.T) {
	m := &Manifest{
		Type:      "convoy",
		Legs:      testLegs(),
		Synth:     testSynthesis(),
		Templates: []Template{{ID: "t1", Title: "Template 1"}},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for convoy with templates")
	}
	if got := err.Error(); got != "convoy workflow must not contain [[template]] entries" {
		t.Errorf("error: got %q", got)
	}
}

func TestShouldManifestConvoy(t *testing.T) {
	m := &Manifest{Type: "convoy"}
	if !ShouldManifest(m) {
		t.Error("ShouldManifest() = false for convoy type")
	}
}

func TestManifestConvoy(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create convoy workflow.
	workflowDir := filepath.Join(solHome, "workflows", "test-convoy")
	os.MkdirAll(workflowDir, 0o755)

	writeTOMLConvoyManifest(t, workflowDir, "test-convoy", testLegs(), testSynthesis())

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-convoy",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify result structure.
	if result.CaravanID == "" {
		t.Error("CaravanID is empty")
	}
	if result.ParentID == "" {
		t.Error("ParentID is empty")
	}

	// 2 legs + 1 synthesis = 3 children.
	if len(result.ChildIDs) != 3 {
		t.Fatalf("ChildIDs: got %d, want 3", len(result.ChildIDs))
	}

	// Verify leg writs.
	for _, leg := range testLegs() {
		childID, ok := result.ChildIDs[leg.ID]
		if !ok {
			t.Fatalf("missing child for leg %q", leg.ID)
		}
		item, err := ws.GetWrit(childID)
		if err != nil {
			t.Fatalf("GetWrit(%q) error: %v", childID, err)
		}
		if item.Title != leg.Title {
			t.Errorf("leg %q title: got %q, want %q", leg.ID, item.Title, leg.Title)
		}
		if item.ParentID != result.ParentID {
			t.Errorf("leg %q parent_id: got %q, want %q", leg.ID, item.ParentID, result.ParentID)
		}
		// Legs should include focus in description.
		if !strings.Contains(item.Description, leg.Focus) {
			t.Errorf("leg %q description missing focus: got %q", leg.ID, item.Description)
		}
	}

	// Verify synthesis writ.
	synthID, ok := result.ChildIDs["synthesis"]
	if !ok {
		t.Fatal("missing child for synthesis")
	}
	synthItem, err := ws.GetWrit(synthID)
	if err != nil {
		t.Fatalf("GetWrit(synthesis) error: %v", err)
	}
	if synthItem.Title != "Consolidate Findings" {
		t.Errorf("synthesis title: got %q, want %q", synthItem.Title, "Consolidate Findings")
	}
	if synthItem.ParentID != result.ParentID {
		t.Errorf("synthesis parent_id: got %q, want %q", synthItem.ParentID, result.ParentID)
	}

	// Verify legs have no dependencies.
	for _, leg := range testLegs() {
		deps, err := ws.GetDependencies(result.ChildIDs[leg.ID])
		if err != nil {
			t.Fatalf("GetDependencies(%q) error: %v", leg.ID, err)
		}
		if len(deps) != 0 {
			t.Errorf("leg %q deps: got %v, want []", leg.ID, deps)
		}
	}

	// Verify synthesis depends on both legs.
	synthDeps, err := ws.GetDependencies(synthID)
	if err != nil {
		t.Fatalf("GetDependencies(synthesis) error: %v", err)
	}
	if len(synthDeps) != 2 {
		t.Fatalf("synthesis deps: got %d, want 2", len(synthDeps))
	}
	depSet := map[string]bool{synthDeps[0]: true, synthDeps[1]: true}
	if !depSet[result.ChildIDs["requirements"]] || !depSet[result.ChildIDs["feasibility"]] {
		t.Errorf("synthesis deps: got %v, want [%s, %s]",
			synthDeps, result.ChildIDs["requirements"], result.ChildIDs["feasibility"])
	}

	// Verify phases: legs = 0, synthesis = 1.
	if result.Phases["requirements"] != 0 {
		t.Errorf("phase[requirements]: got %d, want 0", result.Phases["requirements"])
	}
	if result.Phases["feasibility"] != 0 {
		t.Errorf("phase[feasibility]: got %d, want 0", result.Phases["feasibility"])
	}
	if result.Phases["synthesis"] != 1 {
		t.Errorf("phase[synthesis]: got %d, want 1", result.Phases["synthesis"])
	}

	// Verify caravan was created and is ready.
	caravan, err := ss.GetCaravan(result.CaravanID)
	if err != nil {
		t.Fatalf("GetCaravan() error: %v", err)
	}
	if caravan.Status != "ready" {
		t.Errorf("caravan status: got %q, want ready", caravan.Status)
	}

	// Verify caravan items.
	items, err := ss.ListCaravanItems(result.CaravanID)
	if err != nil {
		t.Fatalf("ListCaravanItems() error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("caravan items: got %d, want 3", len(items))
	}

	// Verify caravan item phases match.
	itemPhases := make(map[string]int)
	for _, ci := range items {
		itemPhases[ci.WritID] = ci.Phase
	}
	for childID, writID := range result.ChildIDs {
		expectedPhase := result.Phases[childID]
		gotPhase := itemPhases[writID]
		if gotPhase != expectedPhase {
			t.Errorf("caravan item phase for %q: got %d, want %d", childID, gotPhase, expectedPhase)
		}
	}
}

// TestConvoyLifecycle tests the full convoy lifecycle:
// manifest → verify structure → simulate leg merges → verify synthesis becomes ready.
func TestConvoyLifecycle(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create convoy workflow.
	workflowDir := filepath.Join(solHome, "workflows", "lifecycle-convoy")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "api", Title: "API Design", Description: "Design the API surface.", Focus: "REST endpoints"},
		{ID: "data", Title: "Data Model", Description: "Design the data model.", Focus: "Schema design"},
		{ID: "ux", Title: "UX Research", Description: "Research UX patterns.", Focus: "User workflows"},
	}
	synth := &Synthesis{
		Title:       "Integration Plan",
		Description: "Combine all design dimensions into a unified plan.",
		DependsOn:   []string{"api", "data", "ux"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "lifecycle-convoy", legs, synth)

	// --- Phase 1: Manifest the convoy ---
	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "lifecycle-convoy",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify 3 legs + 1 synthesis = 4 children.
	if len(result.ChildIDs) != 4 {
		t.Fatalf("ChildIDs: got %d, want 4", len(result.ChildIDs))
	}

	// Verify convoy-leg labels on leg items.
	for _, leg := range legs {
		childID := result.ChildIDs[leg.ID]
		item, err := ws.GetWrit(childID)
		if err != nil {
			t.Fatalf("GetWrit(%q) error: %v", childID, err)
		}
		if !item.HasLabel("convoy-leg") {
			t.Errorf("leg %q missing convoy-leg label", leg.ID)
		}
		if !item.HasLabel("manifest-child") {
			t.Errorf("leg %q missing manifest-child label", leg.ID)
		}
	}

	// Verify convoy-synthesis label and enriched description on synthesis item.
	synthID := result.ChildIDs["synthesis"]
	synthItem, err := ws.GetWrit(synthID)
	if err != nil {
		t.Fatalf("GetWrit(synthesis) error: %v", err)
	}
	if !synthItem.HasLabel("convoy-synthesis") {
		t.Error("synthesis missing convoy-synthesis label")
	}
	// Synthesis description should reference all leg writs.
	for _, leg := range legs {
		legItemID := result.ChildIDs[leg.ID]
		if !strings.Contains(synthItem.Description, legItemID) {
			t.Errorf("synthesis description missing leg reference for %q (%s)", leg.ID, legItemID)
		}
		if !strings.Contains(synthItem.Description, leg.Title) {
			t.Errorf("synthesis description missing leg title %q", leg.Title)
		}
	}

	// Verify phases: legs = 0, synthesis = 1.
	for _, leg := range legs {
		if result.Phases[leg.ID] != 0 {
			t.Errorf("phase[%s]: got %d, want 0", leg.ID, result.Phases[leg.ID])
		}
	}
	if result.Phases["synthesis"] != 1 {
		t.Errorf("phase[synthesis]: got %d, want 1", result.Phases["synthesis"])
	}

	// Close the initial world store — CheckCaravanReadiness opens its own.
	ws.Close()

	// --- Phase 2: Check initial caravan readiness ---
	statuses, err := ss.CheckCaravanReadiness(result.CaravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("CheckCaravanReadiness() error: %v", err)
	}

	// All legs should be ready (phase 0, no dependencies).
	// Synthesis should NOT be ready (phase 1, legs not yet closed).
	for _, s := range statuses {
		if s.WritID == synthID {
			if s.Ready {
				t.Error("synthesis should not be ready before legs are merged")
			}
		} else {
			if !s.Ready {
				t.Errorf("leg %s should be ready", s.WritID)
			}
		}
	}

	// --- Phase 3: Simulate leg merges (close leg writs) ---
	// Close legs one at a time and verify synthesis stays blocked until all are done.
	for i, leg := range legs {
		legItemID := result.ChildIDs[leg.ID]
		ws2, err := store.OpenWorld("test-world")
		if err != nil {
			t.Fatalf("OpenWorld() error: %v", err)
		}
		if _, err := ws2.CloseWrit(legItemID); err != nil {
			t.Fatalf("CloseWrit(%q) error: %v", legItemID, err)
		}
		ws2.Close()

		statuses, err = ss.CheckCaravanReadiness(result.CaravanID, store.OpenWorld)
		if err != nil {
			t.Fatalf("CheckCaravanReadiness() after closing leg %d error: %v", i, err)
		}

		for _, s := range statuses {
			if s.WritID == synthID {
				if i < len(legs)-1 {
					// Not all legs closed yet — synthesis should still be blocked.
					if s.Ready {
						t.Errorf("synthesis became ready after only %d/%d legs closed", i+1, len(legs))
					}
				} else {
					// All legs closed — synthesis should now be ready.
					if !s.Ready {
						t.Error("synthesis should be ready after all legs are closed")
					}
				}
			}
		}
	}

	// --- Phase 4: Close synthesis → caravan should close ---
	ws3, err := store.OpenWorld("test-world")
	if err != nil {
		t.Fatalf("OpenWorld() error: %v", err)
	}
	if _, err := ws3.CloseWrit(synthID); err != nil {
		t.Fatalf("CloseWrit(synthesis) error: %v", err)
	}
	ws3.Close()

	closed, err := ss.TryCloseCaravan(result.CaravanID, store.OpenWorld)
	if err != nil {
		t.Fatalf("TryCloseCaravan() error: %v", err)
	}
	if !closed {
		t.Error("caravan should close after all items (legs + synthesis) are closed")
	}

	caravan, err := ss.GetCaravan(result.CaravanID)
	if err != nil {
		t.Fatalf("GetCaravan() error: %v", err)
	}
	if caravan.Status != "closed" {
		t.Errorf("caravan status: got %q, want closed", caravan.Status)
	}
}

func TestResolvePlanReview(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("plan-review", "")
	if err != nil {
		t.Fatalf("Resolve(plan-review) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}

	// Load and validate the extracted workflow.
	m, err := LoadManifest(res.Path)
	if err != nil {
		t.Fatalf("LoadMaterialize() error: %v", err)
	}
	if m.Type != "convoy" {
		t.Errorf("type: got %q, want %q", m.Type, "convoy")
	}
	if len(m.Legs) != 5 {
		t.Fatalf("legs: got %d, want 5", len(m.Legs))
	}
	if m.Synth == nil {
		t.Fatal("synthesis is nil")
	}
	if len(m.Synth.DependsOn) != 5 {
		t.Errorf("synthesis depends_on: got %d, want 5", len(m.Synth.DependsOn))
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error on plan-review: %v", err)
	}

	// Verify leg IDs.
	wantIDs := map[string]bool{
		"completeness": true,
		"sequencing":   true,
		"risk":         true,
		"scope-creep":  true,
		"testability":  true,
	}
	for _, leg := range m.Legs {
		if !wantIDs[leg.ID] {
			t.Errorf("unexpected leg ID %q", leg.ID)
		}
		delete(wantIDs, leg.ID)
	}
	for id := range wantIDs {
		t.Errorf("missing leg ID %q", id)
	}
}

func TestResolveCodeReview(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve(code-review) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}

	// Load and validate the extracted workflow.
	m, err := LoadManifest(res.Path)
	if err != nil {
		t.Fatalf("LoadMaterialize() error: %v", err)
	}
	if m.Type != "convoy" {
		t.Errorf("type: got %q, want %q", m.Type, "convoy")
	}
	if len(m.Legs) != 2 {
		t.Fatalf("legs: got %d, want 2", len(m.Legs))
	}
	if m.Synth == nil {
		t.Fatal("synthesis is nil")
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error on code-review: %v", err)
	}
}

func TestResolveGuidedDesign(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("guided-design", "")
	if err != nil {
		t.Fatalf("Resolve(guided-design) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}

	// Load and validate the extracted workflow.
	m, err := LoadManifest(res.Path)
	if err != nil {
		t.Fatalf("LoadMaterialize() error: %v", err)
	}
	if m.Type != "convoy" {
		t.Errorf("type: got %q, want %q", m.Type, "convoy")
	}
	if len(m.Legs) != 6 {
		t.Fatalf("legs: got %d, want 6", len(m.Legs))
	}

	// Verify all expected leg IDs.
	expectedLegs := map[string]bool{
		"api-design":     true,
		"data-model":     true,
		"ux-interaction": true,
		"scalability":    true,
		"security":       true,
		"integration":    true,
	}
	for _, leg := range m.Legs {
		if !expectedLegs[leg.ID] {
			t.Errorf("unexpected leg ID: %q", leg.ID)
		}
		delete(expectedLegs, leg.ID)
	}
	for id := range expectedLegs {
		t.Errorf("missing leg ID: %q", id)
	}

	if m.Synth == nil {
		t.Fatal("synthesis is nil")
	}
	if len(m.Synth.DependsOn) != 6 {
		t.Errorf("synthesis depends_on: got %d, want 6", len(m.Synth.DependsOn))
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error on guided-design: %v", err)
	}
}

func TestResolveThoroughWork(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	res, err := Resolve("thorough-work", "")
	if err != nil {
		t.Fatalf("Resolve(thorough-work) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found: %v", err)
	}

	// All 5 step files must be present.
	stepFiles := []string{
		"01-design.md",
		"02-implement.md",
		"03-review.md",
		"04-test.md",
		"05-submit.md",
	}
	for _, f := range stepFiles {
		if _, err := os.Stat(filepath.Join(res.Path, "steps", f)); err != nil {
			t.Errorf("step file %q not found: %v", f, err)
		}
	}

	// Load and validate the manifest.
	m, err := LoadManifest(res.Path)
	if err != nil {
		t.Fatalf("LoadManifest(thorough-work) error: %v", err)
	}
	if m.Name != "thorough-work" {
		t.Errorf("name: got %q, want %q", m.Name, "thorough-work")
	}
	if len(m.Steps) != 5 {
		t.Fatalf("steps: got %d, want 5", len(m.Steps))
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error on thorough-work: %v", err)
	}

	// Verify the DAG: design → implement → review → test → submit.
	expectedIDs := []string{"design", "implement", "review", "test", "submit"}
	for i, s := range m.Steps {
		if s.ID != expectedIDs[i] {
			t.Errorf("step %d: got ID %q, want %q", i, s.ID, expectedIDs[i])
		}
	}
	if len(m.Steps[0].Needs) != 0 {
		t.Errorf("design should have no dependencies, got %v", m.Steps[0].Needs)
	}
	if m.Steps[1].Needs[0] != "design" {
		t.Errorf("implement should need design, got %v", m.Steps[1].Needs)
	}
	if m.Steps[4].Needs[0] != "test" {
		t.Errorf("submit should need test, got %v", m.Steps[4].Needs)
	}
}

func TestListEmbeddedOnly(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	entries, err := List("")
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	// Should find all 8 known defaults.
	if len(entries) != len(knownDefaults) {
		t.Errorf("got %d entries, want %d", len(entries), len(knownDefaults))
	}

	// All should be embedded tier.
	for _, e := range entries {
		if e.Tier != TierEmbedded {
			t.Errorf("entry %q: tier = %q, want %q", e.Name, e.Tier, TierEmbedded)
		}
		if e.Shadowed {
			t.Errorf("entry %q: should not be shadowed", e.Name)
		}
	}

	// Should be sorted by name.
	for i := 1; i < len(entries); i++ {
		if entries[i].Name < entries[i-1].Name {
			t.Errorf("entries not sorted: %q before %q", entries[i-1].Name, entries[i].Name)
		}
	}
}

func TestListUserTier(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Create a custom user workflow.
	userDir := filepath.Join(solHome, "workflows", "my-custom")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "manifest.toml"), []byte(
		"name = \"my-custom\"\ntype = \"workflow\"\ndescription = \"Custom workflow\"\n",
	), 0o644)

	entries, err := List("")
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	// Should have all embedded + 1 custom.
	if len(entries) != len(knownDefaults)+1 {
		t.Errorf("got %d entries, want %d", len(entries), len(knownDefaults)+1)
	}

	// Find the custom entry.
	var found bool
	for _, e := range entries {
		if e.Name == "my-custom" {
			found = true
			if e.Tier != TierUser {
				t.Errorf("my-custom tier: got %q, want %q", e.Tier, TierUser)
			}
			if e.Description != "Custom workflow" {
				t.Errorf("my-custom description: got %q, want %q", e.Description, "Custom workflow")
			}
			if e.Type != "workflow" {
				t.Errorf("my-custom type: got %q, want %q", e.Type, "workflow")
			}
		}
	}
	if !found {
		t.Error("my-custom entry not found")
	}
}

func TestListProjectTier(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	repoDir := t.TempDir()

	// Create a project-level workflow.
	projectDir := filepath.Join(repoDir, ".sol", "workflows", "deploy")
	os.MkdirAll(projectDir, 0o755)
	os.WriteFile(filepath.Join(projectDir, "manifest.toml"), []byte(
		"name = \"deploy\"\ntype = \"workflow\"\ndescription = \"Deploy workflow\"\n",
	), 0o644)

	entries, err := List(repoDir)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	// Should have all embedded + 1 project.
	if len(entries) != len(knownDefaults)+1 {
		t.Errorf("got %d entries, want %d", len(entries), len(knownDefaults)+1)
	}

	var found bool
	for _, e := range entries {
		if e.Name == "deploy" {
			found = true
			if e.Tier != TierProject {
				t.Errorf("deploy tier: got %q, want %q", e.Tier, TierProject)
			}
		}
	}
	if !found {
		t.Error("deploy entry not found")
	}
}

func TestListShadowing(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	repoDir := t.TempDir()

	// Create a project-level workflow that shadows an embedded one.
	projectDir := filepath.Join(repoDir, ".sol", "workflows", "default-work")
	os.MkdirAll(projectDir, 0o755)
	os.WriteFile(filepath.Join(projectDir, "manifest.toml"), []byte(
		"name = \"default-work\"\ntype = \"workflow\"\ndescription = \"Project override\"\n",
	), 0o644)

	entries, err := List(repoDir)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	// Count default-work entries.
	var projectEntry, embeddedEntry *Entry
	for i := range entries {
		if entries[i].Name == "default-work" {
			if entries[i].Tier == TierProject {
				projectEntry = &entries[i]
			} else if entries[i].Tier == TierEmbedded {
				embeddedEntry = &entries[i]
			}
		}
	}

	if projectEntry == nil {
		t.Fatal("project tier default-work not found")
	}
	if projectEntry.Shadowed {
		t.Error("project tier entry should not be shadowed")
	}

	if embeddedEntry == nil {
		t.Fatal("embedded tier default-work not found")
	}
	if !embeddedEntry.Shadowed {
		t.Error("embedded tier entry should be shadowed")
	}
}

func TestListUserShadowsEmbedded(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Extract a default to user tier (simulates Resolve extraction).
	_, err := Resolve("default-work", "")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	entries, err := List("")
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	// default-work should appear twice: user (winning) and embedded (shadowed).
	var userEntry, embeddedEntry *Entry
	for i := range entries {
		if entries[i].Name == "default-work" {
			if entries[i].Tier == TierUser {
				userEntry = &entries[i]
			} else if entries[i].Tier == TierEmbedded {
				embeddedEntry = &entries[i]
			}
		}
	}

	if userEntry == nil {
		t.Fatal("user tier default-work not found")
	}
	if userEntry.Shadowed {
		t.Error("user tier entry should not be shadowed (it wins)")
	}

	if embeddedEntry == nil {
		t.Fatal("embedded tier default-work not found")
	}
	if !embeddedEntry.Shadowed {
		t.Error("embedded tier entry should be shadowed by user tier")
	}
}

func TestManifestConvoyWithKind(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create convoy workflow with analysis kind legs.
	workflowDir := filepath.Join(solHome, "workflows", "test-convoy-kind")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "reqs", Title: "Requirements", Description: "Gather requirements.", Kind: "analysis"},
		{ID: "impl", Title: "Implementation", Description: "Build the thing.", Kind: "code"},
	}
	synth := &Synthesis{
		Title:       "Consolidate",
		Description: "Combine results.",
		DependsOn:   []string{"reqs", "impl"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-convoy-kind", legs, synth)

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-convoy-kind",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify analysis leg has kind=analysis.
	reqsID := result.ChildIDs["reqs"]
	reqsItem, err := ws.GetWrit(reqsID)
	if err != nil {
		t.Fatalf("GetWrit(reqs) error: %v", err)
	}
	if reqsItem.Kind != "analysis" {
		t.Errorf("reqs kind: got %q, want %q", reqsItem.Kind, "analysis")
	}

	// Verify code leg has kind=code.
	implID := result.ChildIDs["impl"]
	implItem, err := ws.GetWrit(implID)
	if err != nil {
		t.Fatalf("GetWrit(impl) error: %v", err)
	}
	if implItem.Kind != "code" {
		t.Errorf("impl kind: got %q, want %q", implItem.Kind, "code")
	}

	// Verify synthesis defaults to code (no kind specified).
	synthID := result.ChildIDs["synthesis"]
	synthItem, err := ws.GetWrit(synthID)
	if err != nil {
		t.Fatalf("GetWrit(synthesis) error: %v", err)
	}
	if synthItem.Kind != "code" {
		t.Errorf("synthesis kind: got %q, want %q", synthItem.Kind, "code")
	}
}

func TestManifestConvoyKindDefaultsToCode(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create convoy workflow with no kind specified (should default to code).
	workflowDir := filepath.Join(solHome, "workflows", "test-convoy-default")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "leg1", Title: "Leg One", Description: "First leg."},
	}
	synth := &Synthesis{
		Title:       "Finish",
		Description: "Wrap up.",
		DependsOn:   []string{"leg1"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-convoy-default", legs, synth)

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-convoy-default",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	legID := result.ChildIDs["leg1"]
	legItem, err := ws.GetWrit(legID)
	if err != nil {
		t.Fatalf("GetWrit(leg1) error: %v", err)
	}
	if legItem.Kind != "code" {
		t.Errorf("leg1 kind: got %q, want %q (default)", legItem.Kind, "code")
	}
}

func TestManifestConvoySynthesisKind(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Test 1: Synthesis with explicit kind = "analysis".
	t.Run("analysis", func(t *testing.T) {
		workflowDir := filepath.Join(solHome, "workflows", "test-synth-analysis")
		os.MkdirAll(workflowDir, 0o755)

		legs := []Leg{
			{ID: "review", Title: "Review", Description: "Review the code.", Kind: "analysis"},
		}
		synth := &Synthesis{
			Title:       "Consolidate Review",
			Description: "Combine review findings.",
			DependsOn:   []string{"review"},
			Kind:        "analysis",
		}
		writeTOMLConvoyManifest(t, workflowDir, "test-synth-analysis", legs, synth)

		result, err := Materialize(ws, ss, ManifestOpts{
			Name: "test-synth-analysis",
			World:       "test-world",
			CreatedBy:   "autarch",
		})
		if err != nil {
			t.Fatalf("Materialize() error: %v", err)
		}

		synthID := result.ChildIDs["synthesis"]
		synthItem, err := ws.GetWrit(synthID)
		if err != nil {
			t.Fatalf("GetWrit(synthesis) error: %v", err)
		}
		if synthItem.Kind != "analysis" {
			t.Errorf("synthesis kind: got %q, want %q", synthItem.Kind, "analysis")
		}
	})

	// Test 2: Synthesis with no kind defaults to "code".
	t.Run("default_code", func(t *testing.T) {
		workflowDir := filepath.Join(solHome, "workflows", "test-synth-default")
		os.MkdirAll(workflowDir, 0o755)

		legs := []Leg{
			{ID: "build", Title: "Build", Description: "Build the thing."},
		}
		synth := &Synthesis{
			Title:       "Finalize",
			Description: "Finalize the build.",
			DependsOn:   []string{"build"},
		}
		writeTOMLConvoyManifest(t, workflowDir, "test-synth-default", legs, synth)

		result, err := Materialize(ws, ss, ManifestOpts{
			Name: "test-synth-default",
			World:       "test-world",
			CreatedBy:   "autarch",
		})
		if err != nil {
			t.Fatalf("Materialize() error: %v", err)
		}

		synthID := result.ChildIDs["synthesis"]
		synthItem, err := ws.GetWrit(synthID)
		if err != nil {
			t.Fatalf("GetWrit(synthesis) error: %v", err)
		}
		if synthItem.Kind != "code" {
			t.Errorf("synthesis kind: got %q, want %q (default)", synthItem.Kind, "code")
		}
	})
}

func TestCaravanPhaseGatingWithAnalysisWrit(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create convoy: phase 0 = analysis writ, phase 1 = code writ.
	workflowDir := filepath.Join(solHome, "workflows", "test-phase-gate")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "analyze", Title: "Analysis Phase", Description: "Analyze.", Kind: "analysis"},
	}
	synth := &Synthesis{
		Title:       "Build Phase",
		Description: "Build based on analysis.",
		DependsOn:   []string{"analyze"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-phase-gate", legs, synth)

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-phase-gate",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	analyzeID := result.ChildIDs["analyze"]
	synthID := result.ChildIDs["synthesis"]

	// worldOpener returns the existing world store (same test-world).
	worldOpener := func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	}

	// Phase 1 (synthesis) should be blocked while phase 0 (analyze) is open.
	blocked, err := ss.IsWritBlockedByCaravan(synthID, "test-world", worldOpener)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if !blocked {
		t.Error("synthesis should be blocked while analysis writ is open")
	}

	// Close the analysis writ directly (simulating resolve for analysis writs).
	if _, err := ws.CloseWrit(analyzeID); err != nil {
		t.Fatalf("CloseWrit(analyze) error: %v", err)
	}

	// Phase 1 should now be unblocked.
	blocked, err = ss.IsWritBlockedByCaravan(synthID, "test-world", worldOpener)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan error: %v", err)
	}
	if blocked {
		t.Error("synthesis should be unblocked after analysis writ is closed")
	}
}

func TestManifestConvoyTargetSubstitution(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create convoy workflow with {target.*} placeholders in leg titles/descriptions.
	workflowDir := filepath.Join(solHome, "workflows", "test-convoy-target")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "review", Title: "Review: {target.title}", Description: "Review the changes for {target.title} ({target.id})."},
		{ID: "test", Title: "Test: {target.title}", Description: "Write tests for {target.description}."},
	}
	synth := &Synthesis{
		Title:       "Merge Reviews",
		Description: "Combine review and test results.",
		DependsOn:   []string{"review", "test"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-convoy-target", legs, synth)

	// Create a target writ to act as the parent.
	targetID, err := ws.CreateWrit("Add login page", "Implement the OAuth login flow", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit() error: %v", err)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-convoy-target",
		World:       "test-world",
		ParentID:    targetID,
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify target substitution in leg titles.
	reviewItem, err := ws.GetWrit(result.ChildIDs["review"])
	if err != nil {
		t.Fatalf("GetWrit(review) error: %v", err)
	}
	if reviewItem.Title != "Review: Add login page" {
		t.Errorf("review title: got %q, want %q", reviewItem.Title, "Review: Add login page")
	}
	wantReviewDesc := "Review the changes for Add login page (" + targetID + ")."
	if reviewItem.Description != wantReviewDesc {
		t.Errorf("review description: got %q, want %q", reviewItem.Description, wantReviewDesc)
	}

	testItem, err := ws.GetWrit(result.ChildIDs["test"])
	if err != nil {
		t.Fatalf("GetWrit(test) error: %v", err)
	}
	if testItem.Title != "Test: Add login page" {
		t.Errorf("test title: got %q, want %q", testItem.Title, "Test: Add login page")
	}
	if testItem.Description != "Write tests for Implement the OAuth login flow." {
		t.Errorf("test description: got %q, want %q", testItem.Description, "Write tests for Implement the OAuth login flow.")
	}
}

func TestManifestConvoyAnalysisSynthesisDescription(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Test 1: All-analysis legs — synthesis should reference output directories.
	workflowDir := filepath.Join(solHome, "workflows", "test-convoy-analysis")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "explore-api", Title: "API Exploration", Description: "Explore the API surface.", Kind: "analysis"},
		{ID: "explore-data", Title: "Data Model", Description: "Explore data model.", Kind: "analysis"},
	}
	synth := &Synthesis{
		Title:       "Design Synthesis",
		Description: "Combine findings.",
		DependsOn:   []string{"explore-api", "explore-data"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-convoy-analysis", legs, synth)

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-convoy-analysis",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	synthItem, err := ws.GetWrit(result.ChildIDs["synthesis"])
	if err != nil {
		t.Fatalf("GetWrit(synthesis) error: %v", err)
	}

	// All-analysis: should mention output directories, NOT merged branches.
	if strings.Contains(synthItem.Description, "merged to the target branch") {
		t.Error("all-analysis synthesis should not mention merged branches")
	}
	if !strings.Contains(synthItem.Description, "Read findings from leg output directories") {
		t.Error("all-analysis synthesis should reference leg output directories")
	}
	// Each analysis leg should have its output path listed.
	apiID := result.ChildIDs["explore-api"]
	dataID := result.ChildIDs["explore-data"]
	if !strings.Contains(synthItem.Description, config.WritOutputDir("test-world", apiID)) {
		t.Errorf("synthesis description missing output dir for explore-api: %s", synthItem.Description)
	}
	if !strings.Contains(synthItem.Description, config.WritOutputDir("test-world", dataID)) {
		t.Errorf("synthesis description missing output dir for explore-data: %s", synthItem.Description)
	}

	// Test 2: Mixed legs — synthesis should mention both branches and output dirs.
	workflowDir2 := filepath.Join(solHome, "workflows", "test-convoy-mixed")
	os.MkdirAll(workflowDir2, 0o755)

	mixedLegs := []Leg{
		{ID: "reqs", Title: "Requirements", Description: "Gather requirements.", Kind: "analysis"},
		{ID: "impl", Title: "Implementation", Description: "Build the thing.", Kind: "code"},
	}
	mixedSynth := &Synthesis{
		Title:       "Consolidate",
		Description: "Combine results.",
		DependsOn:   []string{"reqs", "impl"},
	}
	writeTOMLConvoyManifest(t, workflowDir2, "test-convoy-mixed", mixedLegs, mixedSynth)

	result2, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-convoy-mixed",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() mixed error: %v", err)
	}

	synthItem2, err := ws.GetWrit(result2.ChildIDs["synthesis"])
	if err != nil {
		t.Fatalf("GetWrit(synthesis) mixed error: %v", err)
	}

	// Mixed: should mention both merged branches (for code) and output dirs (for analysis).
	if !strings.Contains(synthItem2.Description, "code leg branches have been merged") {
		t.Error("mixed synthesis should mention merged code branches")
	}
	if !strings.Contains(synthItem2.Description, "analysis leg output directories") {
		t.Error("mixed synthesis should reference analysis output directories")
	}
	reqsID := result2.ChildIDs["reqs"]
	if !strings.Contains(synthItem2.Description, config.WritOutputDir("test-world", reqsID)) {
		t.Errorf("mixed synthesis description missing output dir for reqs: %s", synthItem2.Description)
	}
}

// TestMixedKindConvoyEndToEnd validates the full lifecycle of a caravan with
// mixed code and analysis legs:
//   - Code legs (kind=code) → forge path (branch, MR, merge)
//   - Analysis legs (kind=analysis) → close directly (no MR)
//   - Synthesis waits for ALL legs regardless of kind
//   - Phase gating works correctly across mixed kinds
func TestMixedKindConvoyEndToEnd(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create convoy workflow with mixed code + analysis legs.
	workflowDir := filepath.Join(solHome, "workflows", "test-e2e-mixed")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "code-impl", Title: "Implementation", Description: "Build the feature.", Kind: "code"},
		{ID: "code-tests", Title: "Test Coverage", Description: "Write tests.", Kind: "code"},
		{ID: "design-review", Title: "Design Review", Description: "Review the design.", Kind: "analysis"},
		{ID: "risk-assessment", Title: "Risk Assessment", Description: "Assess risks.", Kind: "analysis"},
	}
	synth := &Synthesis{
		Title:       "Final Integration",
		Description: "Integrate code changes with review findings.",
		DependsOn:   []string{"code-impl", "code-tests", "design-review", "risk-assessment"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-e2e-mixed", legs, synth)

	// Manifest the workflow into writs + caravan.
	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "test-e2e-mixed",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// --- Verify writ kinds are set correctly ---

	codeImplID := result.ChildIDs["code-impl"]
	codeTestsID := result.ChildIDs["code-tests"]
	designID := result.ChildIDs["design-review"]
	riskID := result.ChildIDs["risk-assessment"]
	synthID := result.ChildIDs["synthesis"]

	codeImplWrit, err := ws.GetWrit(codeImplID)
	if err != nil {
		t.Fatalf("GetWrit(code-impl) error: %v", err)
	}
	if codeImplWrit.Kind != "code" {
		t.Errorf("code-impl kind: got %q, want %q", codeImplWrit.Kind, "code")
	}

	codeTestsWrit, err := ws.GetWrit(codeTestsID)
	if err != nil {
		t.Fatalf("GetWrit(code-tests) error: %v", err)
	}
	if codeTestsWrit.Kind != "code" {
		t.Errorf("code-tests kind: got %q, want %q", codeTestsWrit.Kind, "code")
	}

	designWrit, err := ws.GetWrit(designID)
	if err != nil {
		t.Fatalf("GetWrit(design-review) error: %v", err)
	}
	if designWrit.Kind != "analysis" {
		t.Errorf("design-review kind: got %q, want %q", designWrit.Kind, "analysis")
	}

	riskWrit, err := ws.GetWrit(riskID)
	if err != nil {
		t.Fatalf("GetWrit(risk-assessment) error: %v", err)
	}
	if riskWrit.Kind != "analysis" {
		t.Errorf("risk-assessment kind: got %q, want %q", riskWrit.Kind, "analysis")
	}

	// Synthesis defaults to code kind.
	synthWrit, err := ws.GetWrit(synthID)
	if err != nil {
		t.Fatalf("GetWrit(synthesis) error: %v", err)
	}
	if synthWrit.Kind != "code" {
		t.Errorf("synthesis kind: got %q, want %q", synthWrit.Kind, "code")
	}

	// --- Verify phase structure ---
	// All legs should be phase 0, synthesis phase 1.
	for _, legID := range []string{"code-impl", "code-tests", "design-review", "risk-assessment"} {
		if result.Phases[legID] != 0 {
			t.Errorf("phase[%s]: got %d, want 0", legID, result.Phases[legID])
		}
	}
	if result.Phases["synthesis"] != 1 {
		t.Errorf("phase[synthesis]: got %d, want 1", result.Phases["synthesis"])
	}

	// --- Verify labels ---
	codeImplWrit, _ = ws.GetWrit(codeImplID)
	hasConvoyLeg := false
	for _, l := range codeImplWrit.Labels {
		if l == "convoy-leg" {
			hasConvoyLeg = true
		}
	}
	if !hasConvoyLeg {
		t.Error("code leg should have convoy-leg label")
	}

	synthWrit, _ = ws.GetWrit(synthID)
	hasConvoySynthesis := false
	for _, l := range synthWrit.Labels {
		if l == "convoy-synthesis" {
			hasConvoySynthesis = true
		}
	}
	if !hasConvoySynthesis {
		t.Error("synthesis should have convoy-synthesis label")
	}

	// --- Phase gating: synthesis blocked while ANY leg is open ---
	worldOpener := func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	}

	blocked, err := ss.IsWritBlockedByCaravan(synthID, "test-world", worldOpener)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan initial check error: %v", err)
	}
	if !blocked {
		t.Error("synthesis should be blocked while all legs are open")
	}

	// --- Close analysis legs directly (simulating resolve for analysis writs) ---
	if _, err := ws.CloseWrit(designID); err != nil {
		t.Fatalf("CloseWrit(design-review) error: %v", err)
	}
	if _, err := ws.CloseWrit(riskID); err != nil {
		t.Fatalf("CloseWrit(risk-assessment) error: %v", err)
	}

	// Synthesis still blocked — code legs are still open.
	blocked, err = ss.IsWritBlockedByCaravan(synthID, "test-world", worldOpener)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan after analysis close error: %v", err)
	}
	if !blocked {
		t.Error("synthesis should still be blocked while code legs are open")
	}

	// --- Close code legs (simulating forge merge completion) ---
	if _, err := ws.CloseWrit(codeImplID); err != nil {
		t.Fatalf("CloseWrit(code-impl) error: %v", err)
	}

	// Still blocked — one code leg remains.
	blocked, err = ss.IsWritBlockedByCaravan(synthID, "test-world", worldOpener)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan after partial code close error: %v", err)
	}
	if !blocked {
		t.Error("synthesis should be blocked while code-tests is still open")
	}

	// Close last code leg.
	if _, err := ws.CloseWrit(codeTestsID); err != nil {
		t.Fatalf("CloseWrit(code-tests) error: %v", err)
	}

	// --- Synthesis should now be unblocked ---
	blocked, err = ss.IsWritBlockedByCaravan(synthID, "test-world", worldOpener)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan after all legs closed error: %v", err)
	}
	if blocked {
		t.Error("synthesis should be unblocked after all legs (both code and analysis) are closed")
	}

	// --- Verify synthesis description has both code branch and analysis output references ---
	synthWrit, err = ws.GetWrit(synthID)
	if err != nil {
		t.Fatalf("GetWrit(synthesis) final error: %v", err)
	}
	if !strings.Contains(synthWrit.Description, "code leg branches have been merged") {
		t.Error("synthesis description should mention merged code branches")
	}
	if !strings.Contains(synthWrit.Description, "analysis leg output directories") {
		t.Error("synthesis description should reference analysis output directories")
	}
	// Analysis legs should have output directory paths listed.
	if !strings.Contains(synthWrit.Description, config.WritOutputDir("test-world", designID)) {
		t.Errorf("synthesis description missing output dir for design-review")
	}
	if !strings.Contains(synthWrit.Description, config.WritOutputDir("test-world", riskID)) {
		t.Errorf("synthesis description missing output dir for risk-assessment")
	}
}

// --- Code-Review Convoy Workflow Integration Test ---
//
// This test exercises the rebuilt code-review embedded workflow end-to-end:
// 1. Creates a target writ (the code being reviewed).
// 2. Manifests the code-review workflow against the target.
// 3. Verifies legs are created with kind=analysis and target-substituted titles.
// 4. Simulates resolve of analysis legs (close directly, write output files).
// 5. Verifies synthesis unblocks after leg writs close.
// 6. Verifies synthesis agent can read leg output directories.

func TestCodeReviewConvoyWorkflow(t *testing.T) {
	ws, ss := setupStores(t)

	// --- Setup: create the target writ (the code being reviewed) ---
	targetID, err := ws.CreateWrit(
		"Implement OAuth2 login flow",
		"Add OAuth2 authentication with Google and GitHub providers.",
		"autarch", 2, nil,
	)
	if err != nil {
		t.Fatalf("CreateWrit(target) error: %v", err)
	}

	// --- Manifest the code-review workflow against the target ---
	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "code-review",
		World:       "test-world",
		ParentID:    targetID,
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify two legs + one synthesis = three children.
	if len(result.ChildIDs) != 3 {
		t.Fatalf("expected 3 children (2 legs + synthesis), got %d", len(result.ChildIDs))
	}

	// --- Verify legs: kind=analysis and target-substituted titles ---
	reqsID := result.ChildIDs["requirements"]
	reqsItem, err := ws.GetWrit(reqsID)
	if err != nil {
		t.Fatalf("GetWrit(requirements) error: %v", err)
	}
	if reqsItem.Kind != "analysis" {
		t.Errorf("requirements kind: got %q, want %q", reqsItem.Kind, "analysis")
	}
	if reqsItem.Title != "Requirements Analysis: Implement OAuth2 login flow" {
		t.Errorf("requirements title: got %q, want %q", reqsItem.Title, "Requirements Analysis: Implement OAuth2 login flow")
	}
	if !reqsItem.HasLabel("convoy-leg") {
		t.Error("requirements missing convoy-leg label")
	}
	if !strings.Contains(reqsItem.Description, "requirements completeness") {
		t.Error("requirements description should mention requirements completeness")
	}
	if !strings.Contains(reqsItem.Description, "Focus:") {
		t.Error("requirements description should contain Focus section")
	}

	feasID := result.ChildIDs["feasibility"]
	feasItem, err := ws.GetWrit(feasID)
	if err != nil {
		t.Fatalf("GetWrit(feasibility) error: %v", err)
	}
	if feasItem.Kind != "analysis" {
		t.Errorf("feasibility kind: got %q, want %q", feasItem.Kind, "analysis")
	}
	if feasItem.Title != "Feasibility Assessment: Implement OAuth2 login flow" {
		t.Errorf("feasibility title: got %q, want %q", feasItem.Title, "Feasibility Assessment: Implement OAuth2 login flow")
	}
	if !feasItem.HasLabel("convoy-leg") {
		t.Error("feasibility missing convoy-leg label")
	}

	// --- Verify synthesis: target-substituted title and analysis output references ---
	synthID := result.ChildIDs["synthesis"]
	synthItem, err := ws.GetWrit(synthID)
	if err != nil {
		t.Fatalf("GetWrit(synthesis) error: %v", err)
	}
	if synthItem.Title != "Consolidate Review: Implement OAuth2 login flow" {
		t.Errorf("synthesis title: got %q, want %q", synthItem.Title, "Consolidate Review: Implement OAuth2 login flow")
	}
	if !synthItem.HasLabel("convoy-synthesis") {
		t.Error("synthesis missing convoy-synthesis label")
	}
	// All legs are analysis — synthesis should reference output directories, not merged branches.
	if strings.Contains(synthItem.Description, "merged to the target branch") {
		t.Error("synthesis should NOT mention merged branches (all legs are analysis)")
	}
	if !strings.Contains(synthItem.Description, "Read findings from leg output directories") {
		t.Error("synthesis should reference leg output directories")
	}
	// Each analysis leg's output path should be listed in the synthesis description.
	if !strings.Contains(synthItem.Description, config.WritOutputDir("test-world", reqsID)) {
		t.Errorf("synthesis description missing output dir for requirements leg")
	}
	if !strings.Contains(synthItem.Description, config.WritOutputDir("test-world", feasID)) {
		t.Errorf("synthesis description missing output dir for feasibility leg")
	}

	// --- Verify phases: legs at phase 0, synthesis at phase 1 ---
	if result.Phases["requirements"] != 0 {
		t.Errorf("requirements phase: got %d, want 0", result.Phases["requirements"])
	}
	if result.Phases["feasibility"] != 0 {
		t.Errorf("feasibility phase: got %d, want 0", result.Phases["feasibility"])
	}
	if result.Phases["synthesis"] != 1 {
		t.Errorf("synthesis phase: got %d, want 1", result.Phases["synthesis"])
	}

	// --- Verify synthesis is blocked while legs are open ---
	worldOpener := func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	}
	blocked, err := ss.IsWritBlockedByCaravan(synthID, "test-world", worldOpener)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan (before close) error: %v", err)
	}
	if !blocked {
		t.Error("synthesis should be blocked while analysis legs are open")
	}

	// --- Simulate resolve of analysis legs ---
	// Analysis writs close directly (no merge/forge) and write findings to output dir.

	// Close requirements leg + write findings.
	reqsOutputDir := config.WritOutputDir("test-world", reqsID)
	if err := os.MkdirAll(reqsOutputDir, 0o755); err != nil {
		t.Fatalf("create requirements output dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(reqsOutputDir, "findings.md"), []byte(
		"# Requirements Analysis\n\n"+
			"- Success criteria are well-defined.\n"+
			"- Edge case: token expiry not handled.\n",
	), 0o644); err != nil {
		t.Fatalf("write requirements findings: %v", err)
	}
	if _, err := ws.CloseWrit(reqsID, "completed"); err != nil {
		t.Fatalf("CloseWrit(requirements) error: %v", err)
	}

	// Synthesis should still be blocked (feasibility still open).
	blocked, err = ss.IsWritBlockedByCaravan(synthID, "test-world", worldOpener)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan (after requirements close) error: %v", err)
	}
	if !blocked {
		t.Error("synthesis should still be blocked (feasibility leg still open)")
	}

	// Close feasibility leg + write findings.
	feasOutputDir := config.WritOutputDir("test-world", feasID)
	if err := os.MkdirAll(feasOutputDir, 0o755); err != nil {
		t.Fatalf("create feasibility output dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(feasOutputDir, "findings.md"), []byte(
		"# Feasibility Assessment\n\n"+
			"- Follows existing auth patterns.\n"+
			"- OAuth2 library dependency is acceptable.\n",
	), 0o644); err != nil {
		t.Fatalf("write feasibility findings: %v", err)
	}
	if _, err := ws.CloseWrit(feasID, "completed"); err != nil {
		t.Fatalf("CloseWrit(feasibility) error: %v", err)
	}

	// --- Verify synthesis unblocks after both legs close ---
	blocked, err = ss.IsWritBlockedByCaravan(synthID, "test-world", worldOpener)
	if err != nil {
		t.Fatalf("IsWritBlockedByCaravan (after all legs close) error: %v", err)
	}
	if blocked {
		t.Error("synthesis should be unblocked after all analysis legs are closed")
	}

	// --- Verify synthesis agent can read leg output directories ---
	// The synthesis description contains the output paths. Verify the files are readable.
	reqsFindings, err := os.ReadFile(filepath.Join(reqsOutputDir, "findings.md"))
	if err != nil {
		t.Fatalf("read requirements findings: %v", err)
	}
	if !strings.Contains(string(reqsFindings), "token expiry") {
		t.Error("requirements findings should contain expected content")
	}

	feasFindings, err := os.ReadFile(filepath.Join(feasOutputDir, "findings.md"))
	if err != nil {
		t.Fatalf("read feasibility findings: %v", err)
	}
	if !strings.Contains(string(feasFindings), "OAuth2 library") {
		t.Error("feasibility findings should contain expected content")
	}

	// Verify the synthesis description contains paths that actually exist.
	for _, path := range []string{reqsOutputDir, feasOutputDir} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("output directory referenced in synthesis should exist: %s", path)
		}
	}
}

func TestCodeReviewConvoyWorkflowSynthesisTargetSubstitution(t *testing.T) {
	ws, ss := setupStores(t)

	// Create target writ.
	targetID, err := ws.CreateWrit("Fix broken pagination", "The pagination component breaks on page 3.", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit error: %v", err)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name: "code-review",
		World:       "test-world",
		ParentID:    targetID,
		CreatedBy:   "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// All titles should contain the target title.
	for childID, writID := range result.ChildIDs {
		item, err := ws.GetWrit(writID)
		if err != nil {
			t.Fatalf("GetWrit(%s) error: %v", childID, err)
		}
		if !strings.Contains(item.Title, "Fix broken pagination") {
			t.Errorf("%s title should contain target title, got: %q", childID, item.Title)
		}
	}
}

func TestCodeReviewConvoyWorkflowRequiresTarget(t *testing.T) {
	ws, ss := setupStores(t)

	// Manifesting code-review without a ParentID should fail because
	// the manifest declares target as a required variable.
	_, err := Materialize(ws, ss, ManifestOpts{
		Name: "code-review",
		World:       "test-world",
		CreatedBy:   "autarch",
	})
	if err == nil {
		t.Fatal("Materialize() should fail without a target (ParentID)")
	}
	if !strings.Contains(err.Error(), "required variable") {
		t.Errorf("error should mention required variable, got: %v", err)
	}
}

// --- Schema parity tests ---

func TestConvoyVariableSubstitution(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "test-convoy-vars")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "analyze", Title: "Analyze {{project}}", Description: "Review {{project}} code on {{branch}}.", Focus: "Check {{project}} patterns"},
	}
	synth := &Synthesis{
		Title:       "Consolidate {{project}}",
		Description: "Merge findings for {{project}} on {{branch}}.",
		DependsOn:   []string{"analyze"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-convoy-vars", legs, synth, writeTOMLConvoyManifestOpts{
		vars: map[string]VariableDecl{
			"project": {Default: "myapp"},
			"branch":  {Default: "main"},
		},
	})

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "test-convoy-vars",
		World:     "test-world",
		Variables: map[string]string{"project": "sol", "branch": "develop"},
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify leg variable substitution.
	legItem, err := ws.GetWrit(result.ChildIDs["analyze"])
	if err != nil {
		t.Fatalf("GetWrit(analyze) error: %v", err)
	}
	if legItem.Title != "Analyze sol" {
		t.Errorf("leg title: got %q, want %q", legItem.Title, "Analyze sol")
	}
	if !strings.Contains(legItem.Description, "Review sol code on develop") {
		t.Errorf("leg description should contain substituted vars, got: %q", legItem.Description)
	}
	if !strings.Contains(legItem.Description, "Check sol patterns") {
		t.Errorf("leg description should contain substituted focus, got: %q", legItem.Description)
	}

	// Verify synthesis variable substitution.
	synthItem, err := ws.GetWrit(result.ChildIDs["synthesis"])
	if err != nil {
		t.Fatalf("GetWrit(synthesis) error: %v", err)
	}
	if synthItem.Title != "Consolidate sol" {
		t.Errorf("synthesis title: got %q, want %q", synthItem.Title, "Consolidate sol")
	}
	if !strings.Contains(synthItem.Description, "Merge findings for sol on develop") {
		t.Errorf("synthesis description should contain substituted vars, got: %q", synthItem.Description)
	}
}

func TestConvoyVariableAndTargetSubstitutionCompose(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "test-convoy-compose")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "review", Title: "Review {target.title} in {{scope}}", Description: "Check {target.id} with {{scope}}.", Focus: "{target.title} {{scope}}"},
	}
	synth := &Synthesis{
		Title:       "Synthesis {target.title} {{scope}}",
		Description: "Consolidate {target.id} review of {{scope}}.",
		DependsOn:   []string{"review"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-convoy-compose", legs, synth, writeTOMLConvoyManifestOpts{
		vars: map[string]VariableDecl{
			"target": {Required: true},
			"scope":  {Default: "full"},
		},
	})

	// Create target writ.
	targetID, err := ws.CreateWrit("Build API", "API implementation", "autarch", 0, nil)
	if err != nil {
		t.Fatalf("CreateWrit() error: %v", err)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "test-convoy-compose",
		World:     "test-world",
		ParentID:  targetID,
		Variables: map[string]string{"scope": "security"},
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify both substitution types compose: {target.*} first, then {{var}}.
	legItem, err := ws.GetWrit(result.ChildIDs["review"])
	if err != nil {
		t.Fatalf("GetWrit(review) error: %v", err)
	}
	if legItem.Title != "Review Build API in security" {
		t.Errorf("leg title: got %q, want %q", legItem.Title, "Review Build API in security")
	}
	if !strings.Contains(legItem.Description, targetID) {
		t.Errorf("leg description should contain target ID %q, got: %q", targetID, legItem.Description)
	}
	if !strings.Contains(legItem.Description, "security") {
		t.Errorf("leg description should contain 'security', got: %q", legItem.Description)
	}

	// Verify synthesis.
	synthItem, err := ws.GetWrit(result.ChildIDs["synthesis"])
	if err != nil {
		t.Fatalf("GetWrit(synthesis) error: %v", err)
	}
	if synthItem.Title != "Synthesis Build API security" {
		t.Errorf("synthesis title: got %q, want %q", synthItem.Title, "Synthesis Build API security")
	}
}

func TestConvoyLegWithInstructions(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "test-convoy-instr")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	// Write external instruction file.
	instrContent := "# Review Guide\n\nAnalyze {{project}} changes.\n\nCheck all endpoints.\n"
	os.WriteFile(filepath.Join(workflowDir, "steps", "01-review.md"), []byte(instrContent), 0o644)

	legs := []Leg{
		{
			ID:           "review",
			Title:        "Review {{project}}",
			Description:  "Short summary",
			Focus:        "API patterns",
			Instructions: "steps/01-review.md",
		},
	}
	synth := &Synthesis{
		Title:       "Consolidate",
		Description: "Merge findings.",
		DependsOn:   []string{"review"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-convoy-instr", legs, synth, writeTOMLConvoyManifestOpts{
		vars: map[string]VariableDecl{
			"project": {Default: "myapp"},
		},
	})

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "test-convoy-instr",
		World:     "test-world",
		Variables: map[string]string{"project": "sol"},
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	legItem, err := ws.GetWrit(result.ChildIDs["review"])
	if err != nil {
		t.Fatalf("GetWrit(review) error: %v", err)
	}

	// Instructions content should be used as description (not inline description).
	if !strings.Contains(legItem.Description, "# Review Guide") {
		t.Errorf("leg description should contain instructions content, got: %q", legItem.Description)
	}
	// Variable substitution should be applied to instructions content.
	if !strings.Contains(legItem.Description, "Analyze sol changes") {
		t.Errorf("leg description should have {{project}} substituted, got: %q", legItem.Description)
	}
	// Focus should be appended as ## Focus section when instructions is set.
	if !strings.Contains(legItem.Description, "## Focus") {
		t.Errorf("leg description should contain ## Focus section, got: %q", legItem.Description)
	}
	if !strings.Contains(legItem.Description, "API patterns") {
		t.Errorf("leg description should contain focus text, got: %q", legItem.Description)
	}
	// The inline description ("Short summary") should NOT be used as the writ description.
	if strings.HasPrefix(legItem.Description, "Short summary") {
		t.Errorf("leg description should use instructions, not inline description")
	}
}

func TestConvoyLegWithoutInstructionsUnchanged(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "test-convoy-noinstr")
	os.MkdirAll(workflowDir, 0o755)

	legs := []Leg{
		{ID: "review", Title: "Review", Description: "Inline description.", Focus: "patterns"},
	}
	synth := &Synthesis{
		Title:       "Consolidate",
		Description: "Merge.",
		DependsOn:   []string{"review"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-convoy-noinstr", legs, synth)

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "test-convoy-noinstr",
		World:     "test-world",
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	legItem, err := ws.GetWrit(result.ChildIDs["review"])
	if err != nil {
		t.Fatalf("GetWrit(review) error: %v", err)
	}

	// Without instructions, should use description + "Focus: " format (old behavior).
	if !strings.Contains(legItem.Description, "Inline description.") {
		t.Errorf("leg description should start with inline description, got: %q", legItem.Description)
	}
	if !strings.Contains(legItem.Description, "Focus: patterns") {
		t.Errorf("leg description should contain 'Focus: patterns' (old format), got: %q", legItem.Description)
	}
}

func TestConvoySynthesisWithInstructions(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "test-convoy-synth-instr")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	// Write external synthesis instruction file.
	instrContent := "# Synthesis Guide\n\nConsolidate {{project}} findings.\n"
	os.WriteFile(filepath.Join(workflowDir, "steps", "synthesis.md"), []byte(instrContent), 0o644)

	legs := []Leg{
		{ID: "analyze", Title: "Analyze", Description: "Do analysis.", Kind: "analysis"},
	}
	synth := &Synthesis{
		Title:        "Synthesize {{project}}",
		Description:  "Short synthesis summary.",
		Kind:         "analysis",
		Instructions: "steps/synthesis.md",
		DependsOn:    []string{"analyze"},
	}
	writeTOMLConvoyManifest(t, workflowDir, "test-convoy-synth-instr", legs, synth, writeTOMLConvoyManifestOpts{
		vars: map[string]VariableDecl{
			"project": {Default: "myapp"},
		},
	})

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "test-convoy-synth-instr",
		World:     "test-world",
		Variables: map[string]string{"project": "sol"},
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	synthItem, err := ws.GetWrit(result.ChildIDs["synthesis"])
	if err != nil {
		t.Fatalf("GetWrit(synthesis) error: %v", err)
	}

	// Instructions content should be used as base description.
	if !strings.Contains(synthItem.Description, "# Synthesis Guide") {
		t.Errorf("synthesis description should contain instructions content, got: %q", synthItem.Description)
	}
	// Variable substitution should be applied.
	if !strings.Contains(synthItem.Description, "Consolidate sol findings") {
		t.Errorf("synthesis description should have {{project}} substituted, got: %q", synthItem.Description)
	}
	// Leg enrichment (leg references) should be appended AFTER instructions content.
	if !strings.Contains(synthItem.Description, "## Leg Writs") {
		t.Errorf("synthesis description should contain leg enrichment, got: %q", synthItem.Description)
	}
	// Title should also have variables substituted.
	if synthItem.Title != "Synthesize sol" {
		t.Errorf("synthesis title: got %q, want %q", synthItem.Title, "Synthesize sol")
	}
}

func TestExpansionTemplateKind(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "test-expand-kind")
	os.MkdirAll(workflowDir, 0o755)

	toml := `name = "test-expand-kind"
type = "expansion"
description = "Test expansion with kind"

[[template]]
id = "analyze"
title = "Analyze: {target.title}"
description = "Analyze {target.title}."
kind = "analysis"

[[template]]
id = "implement"
title = "Implement: {target.title}"
description = "Implement based on analysis."
needs = ["analyze"]
`
	os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(toml), 0o644)

	targetID, err := ws.CreateWrit("Build feature X", "Feature X implementation", "autarch", 0, nil)
	if err != nil {
		t.Fatalf("CreateWrit() error: %v", err)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "test-expand-kind",
		World:     "test-world",
		ParentID:  targetID,
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Verify analysis template creates analysis writ.
	analyzeItem, err := ws.GetWrit(result.ChildIDs["analyze"])
	if err != nil {
		t.Fatalf("GetWrit(analyze) error: %v", err)
	}
	if analyzeItem.Kind != "analysis" {
		t.Errorf("analyze kind: got %q, want %q", analyzeItem.Kind, "analysis")
	}

	// Verify implement template defaults to code (empty kind).
	implItem, err := ws.GetWrit(result.ChildIDs["implement"])
	if err != nil {
		t.Fatalf("GetWrit(implement) error: %v", err)
	}
	if implItem.Kind != "" && implItem.Kind != "code" {
		t.Errorf("implement kind: got %q, want empty or %q", implItem.Kind, "code")
	}
}

func TestValidateConvoyInstructionsFileNotFound(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Type: "convoy",
		Legs: []Leg{
			{ID: "review", Title: "Review", Description: "Review.", Instructions: "steps/missing.md"},
		},
		Synth: &Synthesis{
			Title:       "Synthesize",
			Description: "Consolidate.",
			DependsOn:   []string{"review"},
		},
	}

	// Without dir, validation should pass (instructions paths not checked).
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() without dir should pass: %v", err)
	}

	// With dir, validation should fail for missing instructions file.
	err := Validate(m, dir)
	if err == nil {
		t.Fatal("Validate() with dir should fail for missing instructions file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
	if !strings.Contains(err.Error(), "review") {
		t.Errorf("error should mention leg ID 'review', got: %v", err)
	}
}

func TestValidateConvoySynthesisInstructionsFileNotFound(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Type: "convoy",
		Legs: []Leg{
			{ID: "review", Title: "Review", Description: "Review."},
		},
		Synth: &Synthesis{
			Title:        "Synthesize",
			Description:  "Consolidate.",
			Instructions: "steps/missing-synth.md",
			DependsOn:    []string{"review"},
		},
	}

	err := Validate(m, dir)
	if err == nil {
		t.Fatal("Validate() should fail for missing synthesis instructions file")
	}
	if !strings.Contains(err.Error(), "synthesis") {
		t.Errorf("error should mention 'synthesis', got: %v", err)
	}
}

func TestValidateConvoyInstructionsFileExists(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "steps"), 0o755)
	os.WriteFile(filepath.Join(dir, "steps", "01-review.md"), []byte("# review"), 0o644)

	m := &Manifest{
		Type: "convoy",
		Legs: []Leg{
			{ID: "review", Title: "Review", Description: "Review.", Instructions: "steps/01-review.md"},
		},
		Synth: &Synthesis{
			Title:       "Synthesize",
			Description: "Consolidate.",
			DependsOn:   []string{"review"},
		},
	}

	if err := Validate(m, dir); err != nil {
		t.Fatalf("Validate() should pass when instructions file exists: %v", err)
	}
}
