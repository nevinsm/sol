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

// writeTOMLConvoyManifest writes a convoy manifest.toml file for testing.
func writeTOMLConvoyManifest(t *testing.T, dir, name string, legs []Leg, synth *Synthesis) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, "manifest.toml"))
	if err != nil {
		t.Fatalf("create manifest.toml: %v", err)
	}
	defer f.Close()

	f.WriteString("name = \"" + name + "\"\n")
	f.WriteString("type = \"convoy\"\n")
	f.WriteString("description = \"Test convoy formula\"\n\n")

	for _, leg := range legs {
		f.WriteString("[[legs]]\n")
		f.WriteString("id = \"" + leg.ID + "\"\n")
		f.WriteString("title = \"" + leg.Title + "\"\n")
		f.WriteString("description = \"" + leg.Description + "\"\n")
		if leg.Focus != "" {
			f.WriteString("focus = \"" + leg.Focus + "\"\n")
		}
		f.WriteString("\n")
	}

	if synth != nil {
		f.WriteString("[synthesis]\n")
		f.WriteString("title = \"" + synth.Title + "\"\n")
		f.WriteString("description = \"" + synth.Description + "\"\n")
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
		t.Fatalf("LoadManifest() error: %v", err)
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
	if got := err.Error(); got != "convoy formula requires at least one [[legs]] entry" {
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
	if got := err.Error(); got != "convoy formula requires a [synthesis] section" {
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
	if got := err.Error(); got != "convoy formula must not contain [[steps]] entries" {
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
	if got := err.Error(); got != "convoy formula must not contain [[template]] entries" {
		t.Errorf("error: got %q", got)
	}
}

func TestShouldManifestConvoy(t *testing.T) {
	m := &Manifest{Type: "convoy"}
	if !ShouldManifest(m) {
		t.Error("ShouldManifest() = false for convoy type")
	}
}

func TestManifestFormulaConvoy(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create convoy formula.
	formulaDir := filepath.Join(solHome, "formulas", "test-convoy")
	os.MkdirAll(formulaDir, 0o755)

	writeTOMLConvoyManifest(t, formulaDir, "test-convoy", testLegs(), testSynthesis())

	result, err := ManifestFormula(ws, ss, ManifestOpts{
		FormulaName: "test-convoy",
		World:       "test-world",
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

	// 2 legs + 1 synthesis = 3 children.
	if len(result.ChildIDs) != 3 {
		t.Fatalf("ChildIDs: got %d, want 3", len(result.ChildIDs))
	}

	// Verify leg work items.
	for _, leg := range testLegs() {
		childID, ok := result.ChildIDs[leg.ID]
		if !ok {
			t.Fatalf("missing child for leg %q", leg.ID)
		}
		item, err := ws.GetWorkItem(childID)
		if err != nil {
			t.Fatalf("GetWorkItem(%q) error: %v", childID, err)
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

	// Verify synthesis work item.
	synthID, ok := result.ChildIDs["synthesis"]
	if !ok {
		t.Fatal("missing child for synthesis")
	}
	synthItem, err := ws.GetWorkItem(synthID)
	if err != nil {
		t.Fatalf("GetWorkItem(synthesis) error: %v", err)
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
		itemPhases[ci.WorkItemID] = ci.Phase
	}
	for formulaID, workItemID := range result.ChildIDs {
		expectedPhase := result.Phases[formulaID]
		gotPhase := itemPhases[workItemID]
		if gotPhase != expectedPhase {
			t.Errorf("caravan item phase for %q: got %d, want %d", formulaID, gotPhase, expectedPhase)
		}
	}
}

// TestConvoyLifecycle tests the full convoy lifecycle:
// manifest → verify structure → simulate leg merges → verify synthesis becomes ready.
func TestConvoyLifecycle(t *testing.T) {
	ws, ss := setupStores(t)

	solHome := os.Getenv("SOL_HOME")

	// Create convoy formula.
	formulaDir := filepath.Join(solHome, "formulas", "lifecycle-convoy")
	os.MkdirAll(formulaDir, 0o755)

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
	writeTOMLConvoyManifest(t, formulaDir, "lifecycle-convoy", legs, synth)

	// --- Phase 1: Manifest the convoy ---
	result, err := ManifestFormula(ws, ss, ManifestOpts{
		FormulaName: "lifecycle-convoy",
		World:       "test-world",
		CreatedBy:   "operator",
	})
	if err != nil {
		t.Fatalf("ManifestFormula() error: %v", err)
	}

	// Verify 3 legs + 1 synthesis = 4 children.
	if len(result.ChildIDs) != 4 {
		t.Fatalf("ChildIDs: got %d, want 4", len(result.ChildIDs))
	}

	// Verify convoy-leg labels on leg items.
	for _, leg := range legs {
		childID := result.ChildIDs[leg.ID]
		item, err := ws.GetWorkItem(childID)
		if err != nil {
			t.Fatalf("GetWorkItem(%q) error: %v", childID, err)
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
	synthItem, err := ws.GetWorkItem(synthID)
	if err != nil {
		t.Fatalf("GetWorkItem(synthesis) error: %v", err)
	}
	if !synthItem.HasLabel("convoy-synthesis") {
		t.Error("synthesis missing convoy-synthesis label")
	}
	// Synthesis description should reference all leg work items.
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
		if s.WorkItemID == synthID {
			if s.Ready {
				t.Error("synthesis should not be ready before legs are merged")
			}
		} else {
			if !s.Ready {
				t.Errorf("leg %s should be ready", s.WorkItemID)
			}
		}
	}

	// --- Phase 3: Simulate leg merges (close leg work items) ---
	// Close legs one at a time and verify synthesis stays blocked until all are done.
	for i, leg := range legs {
		legItemID := result.ChildIDs[leg.ID]
		ws2, err := store.OpenWorld("test-world")
		if err != nil {
			t.Fatalf("OpenWorld() error: %v", err)
		}
		if err := ws2.CloseWorkItem(legItemID); err != nil {
			t.Fatalf("CloseWorkItem(%q) error: %v", legItemID, err)
		}
		ws2.Close()

		statuses, err = ss.CheckCaravanReadiness(result.CaravanID, store.OpenWorld)
		if err != nil {
			t.Fatalf("CheckCaravanReadiness() after closing leg %d error: %v", i, err)
		}

		for _, s := range statuses {
			if s.WorkItemID == synthID {
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
	if err := ws3.CloseWorkItem(synthID); err != nil {
		t.Fatalf("CloseWorkItem(synthesis) error: %v", err)
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

func TestEnsureFormulaPlanReview(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	dir, err := EnsureFormula("plan-review")
	if err != nil {
		t.Fatalf("EnsureFormula(plan-review) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}

	// Load and validate the extracted formula.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
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

func TestEnsureFormulaCodeReview(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	dir, err := EnsureFormula("code-review")
	if err != nil {
		t.Fatalf("EnsureFormula(code-review) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}

	// Load and validate the extracted formula.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
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

func TestEnsureFormulaGuidedDesign(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	dir, err := EnsureFormula("guided-design")
	if err != nil {
		t.Fatalf("EnsureFormula(guided-design) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}

	// Load and validate the extracted formula.
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
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

func TestEnsureFormulaThoroughWork(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	dir, err := EnsureFormula("thorough-work")
	if err != nil {
		t.Fatalf("EnsureFormula(thorough-work) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.toml")); err != nil {
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
		if _, err := os.Stat(filepath.Join(dir, "steps", f)); err != nil {
			t.Errorf("step file %q not found: %v", f, err)
		}
	}

	// Load and validate the manifest.
	m, err := LoadManifest(dir)
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
