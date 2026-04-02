package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	// Optional variable with empty default resolves to empty string.
	m2 := &Manifest{
		Variables: map[string]VariableDecl{
			"optional_var": {Required: false, Default: ""},
		},
	}
	resolved, err = ResolveVariables(m2, map[string]string{})
	if err != nil {
		t.Fatalf("ResolveVariables() error for optional with empty default: %v", err)
	}
	if v, ok := resolved["optional_var"]; !ok {
		t.Error("optional_var not present in resolved map")
	} else if v != "" {
		t.Errorf("optional_var: got %q, want %q", v, "")
	}
}

func TestRenderStepInstructions(t *testing.T) {
	dir := t.TempDir()
	stepsDir := filepath.Join(dir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	t.Run("all variables resolved", func(t *testing.T) {
		content := "Work on {{issue}} from {{base_branch}}.\n"
		os.WriteFile(filepath.Join(stepsDir, "step.md"), []byte(content), 0o644)

		step := StepDef{ID: "test", Instructions: "steps/step.md"}
		vars := map[string]string{"issue": "sol-abc12345", "base_branch": "main"}

		rendered, err := RenderStepInstructions(dir, step, vars)
		if err != nil {
			t.Fatalf("RenderStepInstructions() unexpected error: %v", err)
		}
		if rendered != "Work on sol-abc12345 from main.\n" {
			t.Errorf("rendered: got %q", rendered)
		}
	})

	t.Run("unresolved variable returns error", func(t *testing.T) {
		// Manifest declares {{issue}} but step file also uses {{issue_url}} — undefined.
		content := "Work on {{issue}} from {{base_branch}}. Also {{issue_url}}.\n"
		os.WriteFile(filepath.Join(stepsDir, "step_unresolved.md"), []byte(content), 0o644)

		step := StepDef{ID: "test-unresolved", Instructions: "steps/step_unresolved.md"}
		vars := map[string]string{"issue": "sol-abc12345", "base_branch": "main"}

		_, err := RenderStepInstructions(dir, step, vars)
		if err == nil {
			t.Fatal("RenderStepInstructions() expected error for unresolved variable, got nil")
		}
		if !strings.Contains(err.Error(), "{{issue_url}}") {
			t.Errorf("error should mention unresolved token, got: %v", err)
		}
	})
}

func TestResolve(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Known workflow not on disk → extracted (embedded tier).
	res, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve(code-review) error: %v", err)
	}
	if res.Tier != TierEmbedded {
		t.Errorf("tier: got %q, want %q", res.Tier, TierEmbedded)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "manifest.toml")); err != nil {
		t.Errorf("manifest.toml not found after extraction: %v", err)
	}

	// Already exists with version marker → still embedded tier.
	res2, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve(code-review) second call error: %v", err)
	}
	if res.Path != res2.Path {
		t.Errorf("paths differ: %q vs %q", res.Path, res2.Path)
	}
	if res2.Tier != TierEmbedded {
		t.Errorf("second call tier: got %q, want %q", res2.Tier, TierEmbedded)
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

func TestValidateUnknownType(t *testing.T) {
	// A typo in the type field should be caught immediately.
	m := &Manifest{
		Type: "convyo",
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for unknown workflow type")
	}
	if got := err.Error(); got != `unknown workflow type "convyo": must be workflow` {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateConvoyTypeRejected(t *testing.T) {
	m := &Manifest{Type: "convoy"}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for convoy type")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Errorf("error should mention 'no longer supported', got: %v", err)
	}
}

func TestValidateExpansionTypeRejected(t *testing.T) {
	m := &Manifest{Type: "expansion"}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for expansion type")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Errorf("error should mention 'no longer supported', got: %v", err)
	}
}

func TestValidateUnknownMode(t *testing.T) {
	// A typo in the mode field should be caught immediately.
	m := &Manifest{
		Mode: "manifset",
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("Validate() expected error for unknown workflow mode")
	}
	if got := err.Error(); got != `unknown workflow mode "manifset": must be manifest` {
		t.Errorf("error: got %q", got)
	}
}

func TestValidateValidModes(t *testing.T) {
	for _, mode := range []string{"", "manifest"} {
		m := &Manifest{
			Mode: mode,
			Steps: []StepDef{{ID: "s1", Title: "step"}},
		}
		if err := Validate(m); err != nil {
			t.Errorf("Validate() unexpected error for mode %q: %v", mode, err)
		}
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
	if m.Type != "workflow" {
		t.Errorf("type: got %q, want %q", m.Type, "workflow")
	}
	if m.Mode != "manifest" {
		t.Errorf("mode: got %q, want %q", m.Mode, "manifest")
	}
	if len(m.Steps) != 5 {
		t.Fatalf("steps: got %d, want 5", len(m.Steps))
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error on rule-of-five: %v", err)
	}

	// Verify DAG: draft → refine-correctness → refine-clarity → refine-edge-cases → refine-polish
	expectedIDs := []string{"draft", "refine-correctness", "refine-clarity", "refine-edge-cases", "refine-polish"}
	for i, s := range m.Steps {
		if s.ID != expectedIDs[i] {
			t.Errorf("step %d: got ID %q, want %q", i, s.ID, expectedIDs[i])
		}
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
func setupStores(t *testing.T) (worldStore *store.WorldStore, sphereStore *store.SphereStore) {
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
	// Workflow without mode field (inline by default).
	m := &Manifest{Type: "workflow"}
	if ShouldManifest(m) {
		t.Error("ShouldManifest() = true for workflow without mode")
	}

	// Workflow with mode = "manifest".
	m = &Manifest{Type: "workflow", Mode: "manifest"}
	if !ShouldManifest(m) {
		t.Error("ShouldManifest() = false for workflow with mode = manifest")
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
	if result.ParentID != "" {
		t.Errorf("ParentID should be empty when no target provided, got %q", result.ParentID)
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
		if item.ParentID != "" {
			t.Errorf("step %q parent_id: got %q, want empty (no target provided)", stepDef.ID, item.ParentID)
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

	// Verify caravan was created and is open.
	caravan, err := ss.GetCaravan(result.CaravanID)
	if err != nil {
		t.Fatalf("GetCaravan() error: %v", err)
	}
	if caravan.Status != "drydock" {
		t.Errorf("caravan status: got %q, want drydock", caravan.Status)
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

func TestLoadManifestWithModeManifest(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "flagged-wf"
type = "workflow"
mode = "manifest"
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
	if m.Mode != "manifest" {
		t.Errorf("Mode: got %q, want %q", m.Mode, "manifest")
	}
	if !ShouldManifest(m) {
		t.Error("ShouldManifest() should return true")
	}
}

// writeTOMLManifestWithFlag writes a manifest.toml with the mode field.
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
		f.WriteString("mode = \"manifest\"\n")
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
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if m.Type != "workflow" {
		t.Errorf("type: got %q, want %q", m.Type, "workflow")
	}
	if m.Mode != "manifest" {
		t.Errorf("mode: got %q, want %q", m.Mode, "manifest")
	}
	// 5 parallel analysis steps + 1 synthesis step = 6 total
	if len(m.Steps) != 6 {
		t.Fatalf("steps: got %d, want 6", len(m.Steps))
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error on plan-review: %v", err)
	}

	// Verify step IDs include the 5 parallel steps and synthesis.
	wantIDs := map[string]bool{
		"completeness": true,
		"sequencing":   true,
		"risk":         true,
		"scope-creep":  true,
		"testability":  true,
		"synthesis":    true,
	}
	for _, step := range m.Steps {
		if !wantIDs[step.ID] {
			t.Errorf("unexpected step ID %q", step.ID)
		}
		delete(wantIDs, step.ID)
	}
	for id := range wantIDs {
		t.Errorf("missing step ID %q", id)
	}

	// Synthesis step should depend on all 5 analysis steps.
	synthStep := m.Steps[len(m.Steps)-1]
	if synthStep.ID != "synthesis" {
		t.Errorf("last step should be synthesis, got %q", synthStep.ID)
	}
	if len(synthStep.Needs) != 5 {
		t.Errorf("synthesis needs: got %d, want 5", len(synthStep.Needs))
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
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if m.Type != "workflow" {
		t.Errorf("type: got %q, want %q", m.Type, "workflow")
	}
	if m.Mode != "manifest" {
		t.Errorf("mode: got %q, want %q", m.Mode, "manifest")
	}
	// 10 parallel analysis steps + 1 synthesis step = 11 total
	if len(m.Steps) != 11 {
		t.Fatalf("steps: got %d, want 11", len(m.Steps))
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
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if m.Type != "workflow" {
		t.Errorf("type: got %q, want %q", m.Type, "workflow")
	}
	if m.Mode != "manifest" {
		t.Errorf("mode: got %q, want %q", m.Mode, "manifest")
	}
	// 6 parallel exploration steps + 1 synthesis step = 7 total
	if len(m.Steps) != 7 {
		t.Fatalf("steps: got %d, want 7", len(m.Steps))
	}

	// Verify all expected step IDs.
	expectedSteps := map[string]bool{
		"api-design":     true,
		"data-model":     true,
		"ux-interaction": true,
		"scalability":    true,
		"security":       true,
		"integration":    true,
		"synthesis":      true,
	}
	for _, step := range m.Steps {
		if !expectedSteps[step.ID] {
			t.Errorf("unexpected step ID: %q", step.ID)
		}
		delete(expectedSteps, step.ID)
	}
	for id := range expectedSteps {
		t.Errorf("missing step ID: %q", id)
	}

	// Synthesis step should depend on all 6 exploration steps.
	synthStep := m.Steps[len(m.Steps)-1]
	if synthStep.ID != "synthesis" {
		t.Errorf("last step should be synthesis, got %q", synthStep.ID)
	}
	if len(synthStep.Needs) != 6 {
		t.Errorf("synthesis needs: got %d, want 6", len(synthStep.Needs))
	}
	if err := Validate(m); err != nil {
		t.Fatalf("Validate() error on guided-design: %v", err)
	}
}

func TestListEmbeddedOnly(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	entries, err := List("")
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	// Should find all known defaults.
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
	projectDir := filepath.Join(repoDir, ".sol", "workflows", "code-review")
	os.MkdirAll(projectDir, 0o755)
	os.WriteFile(filepath.Join(projectDir, "manifest.toml"), []byte(
		"name = \"code-review\"\ntype = \"workflow\"\ndescription = \"Project override\"\n",
	), 0o644)

	entries, err := List(repoDir)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	// Count code-review entries.
	var projectEntry, embeddedEntry *Entry
	for i := range entries {
		if entries[i].Name == "code-review" {
			if entries[i].Tier == TierProject {
				projectEntry = &entries[i]
			} else if entries[i].Tier == TierEmbedded {
				embeddedEntry = &entries[i]
			}
		}
	}

	if projectEntry == nil {
		t.Fatal("project tier code-review not found")
	}
	if projectEntry.Shadowed {
		t.Error("project tier entry should not be shadowed")
	}

	if embeddedEntry == nil {
		t.Fatal("embedded tier code-review not found")
	}
	if !embeddedEntry.Shadowed {
		t.Error("embedded tier entry should be shadowed")
	}
}

func TestListUserShadowsEmbedded(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Extract a default to user tier (simulates Resolve extraction).
	_, err := Resolve("code-review", "")
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}

	entries, err := List("")
	if err != nil {
		t.Fatalf("List error: %v", err)
	}

	// code-review should appear twice: user (winning) and embedded (shadowed).
	var userEntry, embeddedEntry *Entry
	for i := range entries {
		if entries[i].Name == "code-review" {
			if entries[i].Tier == TierUser {
				userEntry = &entries[i]
			} else if entries[i].Tier == TierEmbedded {
				embeddedEntry = &entries[i]
			}
		}
	}

	if userEntry == nil {
		t.Fatal("user tier code-review not found")
	}
	if userEntry.Shadowed {
		t.Error("user tier entry should not be shadowed (it wins)")
	}

	if embeddedEntry == nil {
		t.Fatal("embedded tier code-review not found")
	}
	if !embeddedEntry.Shadowed {
		t.Error("embedded tier entry should be shadowed by user tier")
	}
}

// TestMixedKindConvoyEndToEnd validates the full lifecycle of a caravan with
// mixed code and analysis legs:
//   - Code legs (kind=code) → forge path (branch, MR, merge)
//   - Analysis legs (kind=analysis) → close directly (no MR)
//   - Synthesis waits for ALL legs regardless of kind
//   - Phase gating works correctly across mixed kinds

// --- Code-Review Convoy Workflow Integration Test ---
//
// This test exercises the rebuilt code-review embedded workflow end-to-end:
// 1. Creates a target writ (the code being reviewed).
// 2. Manifests the code-review workflow against the target.
// 3. Verifies legs are created with kind=analysis and target-substituted titles.
// 4. Simulates resolve of analysis legs (close directly, write output files).
// 5. Verifies synthesis unblocks after leg writs close.
// 6. Verifies synthesis agent can read leg output directories.

// --- Schema parity tests ---

func TestValidateWorkflowStepInstructionsFileNotFound(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Steps: []StepDef{
			{ID: "s1", Title: "Step 1", Instructions: "steps/missing.md"},
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
	if !strings.Contains(err.Error(), "s1") {
		t.Errorf("error should mention step ID 's1', got: %v", err)
	}
}

func TestValidateWorkflowStepInstructionsFileExists(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "steps"), 0o755)
	os.WriteFile(filepath.Join(dir, "steps", "01-init.md"), []byte("# init"), 0o644)

	m := &Manifest{
		Steps: []StepDef{
			{ID: "s1", Title: "Step 1", Instructions: "steps/01-init.md"},
		},
	}

	if err := Validate(m, dir); err != nil {
		t.Fatalf("Validate() should pass when instructions file exists: %v", err)
	}
}

// --- Mode field, Description, DAG enrichment, target variable tests ---

func TestLoadManifestDefaultsTypeToWorkflow(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "no-type-wf"
description = "Workflow without explicit type"

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
	if m.Type != "workflow" {
		t.Errorf("type: got %q, want %q", m.Type, "workflow")
	}
}

func TestModeManifestCausesStepsToBeChildWrits(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "mode-manifest-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	steps := []StepDef{
		{ID: "analyze", Title: "Analyze", Instructions: "steps/01-analyze.md"},
		{ID: "implement", Title: "Implement", Instructions: "steps/02-implement.md", Needs: []string{"analyze"}},
	}
	writeTOMLManifestWithFlag(t, workflowDir, "mode-manifest-wf", steps, map[string]VariableDecl{
		"issue": {Required: true},
	}, true)
	for _, s := range steps {
		content := "# " + s.Title + "\n\nWork on {{issue}}.\n"
		os.WriteFile(filepath.Join(workflowDir, s.Instructions), []byte(content), 0o644)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "mode-manifest-wf",
		World:     "test-world",
		Variables: map[string]string{"issue": "sol-test456"},
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	if len(result.ChildIDs) != 2 {
		t.Fatalf("ChildIDs: got %d, want 2", len(result.ChildIDs))
	}

	// Verify child writs were created.
	analyzeItem, _ := ws.GetWrit(result.ChildIDs["analyze"])
	if analyzeItem.Title != "Analyze" {
		t.Errorf("analyze title: got %q, want %q", analyzeItem.Title, "Analyze")
	}
	if !strings.Contains(analyzeItem.Description, "sol-test456") {
		t.Errorf("analyze description missing variable: %q", analyzeItem.Description)
	}
}

func TestStepDefDescriptionField(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "desc-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	// Write a manifest with description on steps (no instructions file for first step).
	toml := `name = "desc-wf"
type = "workflow"
mode = "manifest"
description = "Test description field"

[variables]
issue = { required = true }

[[steps]]
id = "plan"
title = "Plan"
description = "Plan the work for {{issue}}."

[[steps]]
id = "implement"
title = "Implement"
instructions = "steps/02-impl.md"
description = "This should be overridden by instructions."
needs = ["plan"]
`
	os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(toml), 0o644)
	os.WriteFile(filepath.Join(workflowDir, "steps", "02-impl.md"),
		[]byte("# Implement\n\nImplement {{issue}}.\n"), 0o644)

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "desc-wf",
		World:     "test-world",
		Variables: map[string]string{"issue": "sol-desc-test"},
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// Step with description only (no instructions).
	planItem, _ := ws.GetWrit(result.ChildIDs["plan"])
	if planItem.Description != "Plan the work for sol-desc-test." {
		t.Errorf("plan description: got %q, want %q", planItem.Description, "Plan the work for sol-desc-test.")
	}

	// Step with both description and instructions — instructions wins.
	implItem, _ := ws.GetWrit(result.ChildIDs["implement"])
	if !strings.Contains(implItem.Description, "Implement sol-desc-test") {
		t.Errorf("implement description should come from instructions file, got: %q", implItem.Description)
	}
	if strings.Contains(implItem.Description, "overridden") {
		t.Error("implement description should NOT contain the description field text")
	}
}

func TestDAGEnrichmentForManifestedWorkflowSteps(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "dag-enrich-wf")
	os.MkdirAll(filepath.Join(workflowDir, "steps"), 0o755)

	// Write a manifest with steps that have dependencies and kinds.
	toml := `name = "dag-enrich-wf"
type = "workflow"
mode = "manifest"
description = "Test DAG enrichment"

[variables]
issue = { required = true }

[[steps]]
id = "analyze"
title = "Analyze Requirements"
description = "Analyze the requirements for {{issue}}."
kind = "analysis"

[[steps]]
id = "implement"
title = "Implement Changes"
instructions = "steps/02-impl.md"
needs = ["analyze"]

[[steps]]
id = "verify"
title = "Verify"
instructions = "steps/03-verify.md"
needs = ["implement"]
`
	os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(toml), 0o644)
	os.WriteFile(filepath.Join(workflowDir, "steps", "02-impl.md"),
		[]byte("# Implement\n\nImplement {{issue}}.\n"), 0o644)
	os.WriteFile(filepath.Join(workflowDir, "steps", "03-verify.md"),
		[]byte("# Verify\n\nVerify {{issue}}.\n"), 0o644)

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "dag-enrich-wf",
		World:     "test-world",
		Variables: map[string]string{"issue": "sol-dag-test"},
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	// First step (no deps) — no enrichment.
	analyzeItem, _ := ws.GetWrit(result.ChildIDs["analyze"])
	if strings.Contains(analyzeItem.Description, "Dependency Writs") {
		t.Error("analyze should NOT have DAG enrichment (no deps)")
	}

	// Second step (depends on analyze) — should have enrichment.
	implItem, _ := ws.GetWrit(result.ChildIDs["implement"])
	if !strings.Contains(implItem.Description, "Dependency Writs") {
		t.Error("implement should have DAG enrichment section")
	}
	if !strings.Contains(implItem.Description, result.ChildIDs["analyze"]) {
		t.Error("implement enrichment should contain analyze writ ID")
	}
	if !strings.Contains(implItem.Description, "Analyze Requirements") {
		t.Error("implement enrichment should contain analyze title")
	}
	// Analysis dependency should reference output directory.
	if !strings.Contains(implItem.Description, "output at") {
		t.Error("implement enrichment should reference output path for analysis dependency")
	}

	// Third step (depends on implement/code kind) — should mention branch.
	verifyItem, _ := ws.GetWrit(result.ChildIDs["verify"])
	if !strings.Contains(verifyItem.Description, "Dependency Writs") {
		t.Error("verify should have DAG enrichment section")
	}
	if !strings.Contains(verifyItem.Description, result.ChildIDs["implement"]) {
		t.Error("verify enrichment should contain implement writ ID")
	}
	if !strings.Contains(verifyItem.Description, "branch merged to target") {
		t.Error("verify enrichment should mention branch for code dependency")
	}
}

func TestTargetVariableAutoPopulation(t *testing.T) {
	ws, ss := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	workflowDir := filepath.Join(solHome, "workflows", "target-vars-wf")
	os.MkdirAll(workflowDir, 0o755)

	// Write a manifest that uses {{target.*}} variables.
	toml := `name = "target-vars-wf"
type = "workflow"
mode = "manifest"
description = "Test target variable auto-population"

[[steps]]
id = "review"
title = "Review: {{target.title}}"
description = "Review {{target.title}} ({{target.id}}). Description: {{target.description}}"
`
	os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(toml), 0o644)

	// Create a target writ.
	targetID, err := ws.CreateWrit("Fix login bug", "The login form breaks on Safari.", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit() error: %v", err)
	}

	result, err := Materialize(ws, ss, ManifestOpts{
		Name:      "target-vars-wf",
		World:     "test-world",
		ParentID:  targetID,
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatalf("Materialize() error: %v", err)
	}

	reviewItem, _ := ws.GetWrit(result.ChildIDs["review"])
	if reviewItem.Title != "Review: Fix login bug" {
		t.Errorf("title: got %q, want %q", reviewItem.Title, "Review: Fix login bug")
	}
	if !strings.Contains(reviewItem.Description, targetID) {
		t.Errorf("description should contain target ID %q, got: %q", targetID, reviewItem.Description)
	}
	if !strings.Contains(reviewItem.Description, "Fix login bug") {
		t.Errorf("description should contain target title, got: %q", reviewItem.Description)
	}
	if !strings.Contains(reviewItem.Description, "The login form breaks on Safari.") {
		t.Errorf("description should contain target description, got: %q", reviewItem.Description)
	}
}

func TestModeInlineIsDefault(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "inline-wf"
type = "workflow"
description = "Default mode is inline"

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
	if m.Mode != "" {
		t.Errorf("mode: got %q, want empty (manifest default)", m.Mode)
	}
	if ShouldManifest(m) {
		t.Error("ShouldManifest() should return false when mode is empty")
	}
}

func TestStepDefDescriptionFieldInLoadManifest(t *testing.T) {
	dir := t.TempDir()
	toml := `name = "desc-load-wf"
type = "workflow"

[[steps]]
id = "s1"
title = "Step 1"
description = "This is the step description."
instructions = "steps/s1.md"
`
	os.WriteFile(filepath.Join(dir, "manifest.toml"), []byte(toml), 0o644)

	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if m.Steps[0].Description != "This is the step description." {
		t.Errorf("step description: got %q, want %q", m.Steps[0].Description, "This is the step description.")
	}
}
