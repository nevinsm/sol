package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
	if got := err.Error(); got != "dependency cycle detected in workflow steps" {
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
	inst, state, err := Instantiate("haven", "Toast", "test-wf",
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
	wfDir := WorkflowDir("haven", "Toast")
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

	_, _, err := Instantiate("haven", "Toast", "test-wf", map[string]string{})
	if err == nil {
		t.Fatal("Instantiate() expected error for missing required variable")
	}

	// Verify no directory created.
	wfDir := WorkflowDir("haven", "Toast")
	if _, err := os.Stat(wfDir); !os.IsNotExist(err) {
		t.Errorf("workflow directory should not exist after error, but stat returned: %v", err)
	}
}

func TestReadState(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Non-existent workflow.
	state, err := ReadState("haven", "Ghost")
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

	Instantiate("haven", "Toast", "test-wf", map[string]string{"issue": "sol-test"})

	state, err = ReadState("haven", "Toast")
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

	Instantiate("haven", "Toast", "test-wf", map[string]string{"issue": "sol-abc"})

	step, err := ReadCurrentStep("haven", "Toast")
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

	Instantiate("haven", "Toast", "test-wf", map[string]string{"issue": "sol-test"})

	// Advance 1: load-context → implement.
	next, done, err := Advance("haven", "Toast")
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
	state, _ := ReadState("haven", "Toast")
	if len(state.Completed) != 1 || state.Completed[0] != "load-context" {
		t.Errorf("completed: got %v, want [load-context]", state.Completed)
	}

	// Advance 2: implement → verify.
	next, done, err = Advance("haven", "Toast")
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
	next, done, err = Advance("haven", "Toast")
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
	state, _ = ReadState("haven", "Toast")
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

	_, state, err := Instantiate("haven", "Toast", "dag-wf", map[string]string{"issue": "sol-test"})
	if err != nil {
		t.Fatalf("Instantiate() error: %v", err)
	}
	if state.CurrentStep != "a" {
		t.Fatalf("initial step: got %q, want %q", state.CurrentStep, "a")
	}

	// After A: both B and C ready, pick B (first in manifest order).
	next, done, err := Advance("haven", "Toast")
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
	next, done, err = Advance("haven", "Toast")
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
	next, done, err = Advance("haven", "Toast")
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
	next, done, err = Advance("haven", "Toast")
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

	Instantiate("haven", "Toast", "test-wf", map[string]string{"issue": "sol-test"})

	wfDir := WorkflowDir("haven", "Toast")
	if _, err := os.Stat(wfDir); err != nil {
		t.Fatalf("workflow dir should exist: %v", err)
	}

	if err := Remove("haven", "Toast"); err != nil {
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
