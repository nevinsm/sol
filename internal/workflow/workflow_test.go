package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

func setupTestFormula(t *testing.T, steps []StepDef, vars map[string]VariableDecl) string {
	t.Helper()
	dir := t.TempDir()

	if vars == nil {
		vars = map[string]VariableDecl{
			"issue":       {Required: true},
			"base_branch": {Default: "main"},
		}
	}

	os.MkdirAll(filepath.Join(dir, "steps"), 0o755)
	writeTOMLManifest(t, dir, "test-formula", steps, vars)

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
	dir := setupTestFormula(t, linearSteps(), nil)

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}

	if m.Name != "test-formula" {
		t.Errorf("name: got %q, want %q", m.Name, "test-formula")
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
		t.Fatal("LoadManifest() expected error for missing directory")
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

	// Create formula dir.
	formulaDir := filepath.Join(solHome, "formulas", "test-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)

	steps := linearSteps()
	writeTOMLManifest(t, formulaDir, "test-wf", steps, map[string]VariableDecl{
		"issue":       {Required: true},
		"base_branch": {Default: "main"},
	})
	for _, s := range steps {
		content := "# " + s.Title + "\n\nWork on {{issue}} from {{base_branch}}.\n"
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte(content), 0o644)
	}

	// Create outpost dir.
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	// Instantiate.
	inst, state, err := Instantiate("haven", "Toast", "agent", "test-wf",
		map[string]string{"issue": "sol-abc12345"})
	if err != nil {
		t.Fatalf("Instantiate() error: %v", err)
	}

	if inst.Formula != "test-wf" {
		t.Errorf("formula: got %q, want %q", inst.Formula, "test-wf")
	}
	if state.CurrentStep != "load-context" {
		t.Errorf("current_step: got %q, want %q", state.CurrentStep, "load-context")
	}
	if state.Status != "running" {
		t.Errorf("status: got %q, want %q", state.Status, "running")
	}

	// Verify files created.
	wfDir := WorkflowDir("haven", "Toast", "agent")
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

	formulaDir := filepath.Join(solHome, "formulas", "test-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)

	steps := linearSteps()
	writeTOMLManifest(t, formulaDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte("test"), 0o644)
	}

	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	_, _, err := Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{})
	if err == nil {
		t.Fatal("Instantiate() expected error for missing required variable")
	}

	// Verify no directory created.
	wfDir := WorkflowDir("haven", "Toast", "agent")
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
	formulaDir := filepath.Join(solHome, "formulas", "test-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, formulaDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte("test"), 0o644)
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

	formulaDir := filepath.Join(solHome, "formulas", "test-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, formulaDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte("Do {{issue}}"), 0o644)
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

	formulaDir := filepath.Join(solHome, "formulas", "test-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, formulaDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte("test"), 0o644)
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

	formulaDir := filepath.Join(solHome, "formulas", "dag-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)
	writeTOMLManifest(t, formulaDir, "dag-wf", dagSteps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range dagSteps {
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte("test"), 0o644)
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

func TestRemove(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	formulaDir := filepath.Join(solHome, "formulas", "test-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, formulaDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte("test"), 0o644)
	}
	os.MkdirAll(filepath.Join(solHome, "haven", "outposts", "Toast"), 0o755)

	Instantiate("haven", "Toast", "agent", "test-wf", map[string]string{"issue": "sol-test"})

	wfDir := WorkflowDir("haven", "Toast", "agent")
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

func TestEnsureFormula(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Known formula not on disk → extracted.
	dir, err := EnsureFormula("default-work")
	if err != nil {
		t.Fatalf("EnsureFormula(default-work) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "steps", "01-load-context.md")); err != nil {
		t.Errorf("step file not found after extraction: %v", err)
	}

	// Already exists → no-op.
	dir2, err := EnsureFormula("default-work")
	if err != nil {
		t.Fatalf("EnsureFormula(default-work) second call error: %v", err)
	}
	if dir != dir2 {
		t.Errorf("paths differ: %q vs %q", dir, dir2)
	}

	// Unknown formula → error.
	_, err = EnsureFormula("nonexistent-formula")
	if err == nil {
		t.Fatal("EnsureFormula(nonexistent) expected error")
	}
}

func TestAdvanceIdempotentOnCompletedStep(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	formulaDir := filepath.Join(solHome, "formulas", "test-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)
	steps := linearSteps()
	writeTOMLManifest(t, formulaDir, "test-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	for _, s := range steps {
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte("test"), 0o644)
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
	statePath := filepath.Join(WorkflowDir("haven", "Toast", "agent"), "state.json")
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
description = "Test expansion formula"

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
		t.Fatalf("LoadManifest() error: %v", err)
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
	if got := err.Error(); got != "expansion formula requires at least one [[template]] entry" {
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
	if got := err.Error(); got != "expansion formula must not contain [[steps]] entries" {
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
	if got := err.Error(); got != "workflow formula must not contain [[template]] entries" {
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
	if got := err.Error(); got != "agent formula must not contain [[template]] entries" {
		t.Errorf("error: got %q", got)
	}
}

func TestEnsureFormulaRuleOfFive(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	dir, err := EnsureFormula("rule-of-five")
	if err != nil {
		t.Fatalf("EnsureFormula(rule-of-five) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}

	// Load and validate the extracted formula.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
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
	dir2, err := EnsureFormula("rule-of-five")
	if err != nil {
		t.Fatalf("EnsureFormula(rule-of-five) second call error: %v", err)
	}
	if dir != dir2 {
		t.Errorf("paths differ: %q vs %q", dir, dir2)
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

func TestManifestFormulaWorkflow(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create a manifested workflow formula.
	formulaDir := filepath.Join(solHome, "formulas", "manifest-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)

	steps := linearSteps()
	writeTOMLManifestWithFlag(t, formulaDir, "manifest-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	}, true)
	for _, s := range steps {
		content := "# " + s.Title + "\n\nWork on {{issue}}.\n"
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte(content), 0o644)
	}

	result, err := ManifestFormula(ws, ss, ManifestOpts{
		FormulaName: "manifest-wf",
		World:       "test-world",
		Variables:   map[string]string{"issue": "sol-test123"},
		CreatedBy:   "operator",
	})
	if err != nil {
		t.Fatalf("ManifestFormula() error: %v", err)
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

	// Verify child work items were created with correct titles and parent.
	for _, stepDef := range steps {
		childID, ok := result.ChildIDs[stepDef.ID]
		if !ok {
			t.Fatalf("missing child for step %q", stepDef.ID)
		}
		item, err := ws.GetWorkItem(childID)
		if err != nil {
			t.Fatalf("GetWorkItem(%q) error: %v", childID, err)
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
	loadItem, _ := ws.GetWorkItem(result.ChildIDs["load-context"])
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
		itemPhases[ci.WorkItemID] = ci.Phase
	}
	for stepID, workItemID := range result.ChildIDs {
		expectedPhase := result.Phases[stepID]
		gotPhase := itemPhases[workItemID]
		if gotPhase != expectedPhase {
			t.Errorf("caravan item phase for %q: got %d, want %d", stepID, gotPhase, expectedPhase)
		}
	}
}

func TestManifestFormulaExpansion(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create expansion formula.
	formulaDir := filepath.Join(solHome, "formulas", "test-expand")
	os.MkdirAll(formulaDir, 0o755)

	toml := `name = "test-expand"
type = "expansion"
description = "Test expansion formula"

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
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(toml), 0o644)

	// Create a target work item.
	targetID, err := ws.CreateWorkItem("Build auth system", "Implement OAuth2", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem() error: %v", err)
	}

	result, err := ManifestFormula(ws, ss, ManifestOpts{
		FormulaName: "test-expand",
		World:       "test-world",
		ParentID:    targetID,
		CreatedBy:   "operator",
	})
	if err != nil {
		t.Fatalf("ManifestFormula() error: %v", err)
	}

	// Verify children.
	if len(result.ChildIDs) != 2 {
		t.Fatalf("ChildIDs: got %d, want 2", len(result.ChildIDs))
	}

	// Verify template variable substitution in titles.
	draftItem, err := ws.GetWorkItem(result.ChildIDs["draft"])
	if err != nil {
		t.Fatalf("GetWorkItem(draft) error: %v", err)
	}
	if draftItem.Title != "Draft: Build auth system" {
		t.Errorf("draft title: got %q, want %q", draftItem.Title, "Draft: Build auth system")
	}
	if draftItem.Description != "Initial attempt at Build auth system." {
		t.Errorf("draft description: got %q", draftItem.Description)
	}

	refineItem, err := ws.GetWorkItem(result.ChildIDs["refine"])
	if err != nil {
		t.Fatalf("GetWorkItem(refine) error: %v", err)
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

func TestManifestFormulaExpansionRequiresParent(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	formulaDir := filepath.Join(solHome, "formulas", "test-expand")
	os.MkdirAll(formulaDir, 0o755)

	toml := `name = "test-expand"
type = "expansion"
description = "Test expansion formula"

[[template]]
id = "draft"
title = "Draft"
description = "First pass."
`
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(toml), 0o644)

	_, err := ManifestFormula(ws, ss, ManifestOpts{
		FormulaName: "test-expand",
		World:       "test-world",
		CreatedBy:   "operator",
	})
	if err == nil {
		t.Fatal("ManifestFormula() expected error for expansion without parent")
	}
	if !strings.Contains(err.Error(), "requires a parent work item") {
		t.Errorf("error: got %q", err.Error())
	}
}

func TestManifestFormulaRejectsNonManifest(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	formulaDir := filepath.Join(solHome, "formulas", "plain-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)

	steps := []StepDef{{ID: "s1", Title: "Step 1", Instructions: "steps/s1.md"}}
	writeTOMLManifest(t, formulaDir, "plain-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	})
	os.WriteFile(filepath.Join(formulaDir, "steps", "s1.md"), []byte("test"), 0o644)

	_, err := ManifestFormula(ws, ss, ManifestOpts{
		FormulaName: "plain-wf",
		World:       "test-world",
		Variables:   map[string]string{"issue": "sol-test"},
		CreatedBy:   "operator",
	})
	if err == nil {
		t.Fatal("ManifestFormula() expected error for non-manifest formula")
	}
	if !strings.Contains(err.Error(), "not configured for manifestation") {
		t.Errorf("error: got %q", err.Error())
	}
}

func TestManifestFormulaDAGPhases(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// DAG: A (no deps), B needs A, C needs A, D needs B and C.
	dagSteps := []StepDef{
		{ID: "a", Title: "Step A", Instructions: "steps/a.md"},
		{ID: "b", Title: "Step B", Instructions: "steps/b.md", Needs: []string{"a"}},
		{ID: "c", Title: "Step C", Instructions: "steps/c.md", Needs: []string{"a"}},
		{ID: "d", Title: "Step D", Instructions: "steps/d.md", Needs: []string{"b", "c"}},
	}

	formulaDir := filepath.Join(solHome, "formulas", "dag-manifest")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)
	writeTOMLManifestWithFlag(t, formulaDir, "dag-manifest", dagSteps, map[string]VariableDecl{
		"issue": {Required: true},
	}, true)
	for _, s := range dagSteps {
		os.WriteFile(filepath.Join(formulaDir, s.Instructions), []byte("test"), 0o644)
	}

	result, err := ManifestFormula(ws, ss, ManifestOpts{
		FormulaName: "dag-manifest",
		World:       "test-world",
		Variables:   map[string]string{"issue": "sol-dag-test"},
		CreatedBy:   "operator",
	})
	if err != nil {
		t.Fatalf("ManifestFormula() error: %v", err)
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

func TestManifestFormulaWithExistingParent(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	formulaDir := filepath.Join(solHome, "formulas", "parent-wf")
	os.MkdirAll(filepath.Join(formulaDir, "steps"), 0o755)

	steps := []StepDef{
		{ID: "only-step", Title: "The only step", Instructions: "steps/only.md"},
	}
	writeTOMLManifestWithFlag(t, formulaDir, "parent-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	}, true)
	os.WriteFile(filepath.Join(formulaDir, "steps", "only.md"), []byte("Do the thing."), 0o644)

	// Create parent first.
	parentID, err := ws.CreateWorkItem("Parent item", "Top-level work", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem() error: %v", err)
	}

	result, err := ManifestFormula(ws, ss, ManifestOpts{
		FormulaName: "parent-wf",
		World:       "test-world",
		ParentID:    parentID,
		Variables:   map[string]string{"issue": "sol-test"},
		CreatedBy:   "operator",
	})
	if err != nil {
		t.Fatalf("ManifestFormula() error: %v", err)
	}

	// Verify parent is the provided one.
	if result.ParentID != parentID {
		t.Errorf("ParentID: got %q, want %q", result.ParentID, parentID)
	}

	// Verify child's parent.
	child, err := ws.GetWorkItem(result.ChildIDs["only-step"])
	if err != nil {
		t.Fatalf("GetWorkItem() error: %v", err)
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
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if !m.Manifest {
		t.Error("Manifest field should be true")
	}
	if !ShouldManifest(m) {
		t.Error("ShouldManifest() should return true")
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
	f.WriteString("description = \"Test formula\"\n\n")

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
	f.WriteString("description = \"Test formula\"\n\n")

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
