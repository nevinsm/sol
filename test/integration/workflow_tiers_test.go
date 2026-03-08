package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/workflow"
)

// --- Three-Tier Workflow Resolution Integration Tests ---
//
// These tests verify the project > user > embedded resolution order
// defined in ADR-0021. Project formulas live in {repo}/.sol/workflows/,
// user formulas in $SOL_HOME/formulas/, and embedded formulas are
// compiled into the binary.

// makeFormula creates a minimal workflow formula directory with a single step.
func makeFormula(t *testing.T, dir, name, description string) {
	t.Helper()
	formulaDir := filepath.Join(dir, name)
	stepsDir := filepath.Join(formulaDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create formula dir %s: %v", name, err)
	}
	manifest := `name = "` + name + `"
type = "agent"
description = "` + description + `"

[variables]
[variables.issue]
required = true

[[steps]]
id = "only"
title = "Only Step"
instructions = "steps/01.md"
`
	if err := os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest for %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Instructions for "+name+" ("+description+"): {{issue}}\n"), 0o644); err != nil {
		t.Fatalf("write step for %s: %v", name, err)
	}
}

func TestProjectWorkflowOverridesUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Create same formula at project and user tiers.
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	userBase := filepath.Join(solHome, "formulas")

	makeFormula(t, projectBase, "shared-formula", "project tier")
	makeFormula(t, userBase, "shared-formula", "user tier")

	// Resolve — project tier should win.
	res, err := workflow.EnsureFormula("shared-formula", repoPath)
	if err != nil {
		t.Fatalf("EnsureFormula: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("tier: got %q, want %q", res.Tier, workflow.TierProject)
	}
	if !strings.Contains(res.Path, ".sol/workflows/shared-formula") {
		t.Errorf("path should be project-level: %s", res.Path)
	}
}

func TestUserWorkflowFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Create formula only at user tier. Project tier directory exists but
	// does not contain this formula.
	if err := os.MkdirAll(filepath.Join(repoPath, ".sol", "workflows"), 0o755); err != nil {
		t.Fatalf("create project workflows dir: %v", err)
	}
	userBase := filepath.Join(solHome, "formulas")
	makeFormula(t, userBase, "user-only-formula", "user tier")

	// Resolve — user tier should win.
	res, err := workflow.EnsureFormula("user-only-formula", repoPath)
	if err != nil {
		t.Fatalf("EnsureFormula: %v", err)
	}
	if res.Tier != workflow.TierUser {
		t.Errorf("tier: got %q, want %q", res.Tier, workflow.TierUser)
	}
}

func TestEmbeddedWorkflowFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Ensure the repo path exists but has no project-level default-work.
	if err := os.MkdirAll(filepath.Join(repoPath, ".sol", "workflows"), 0o755); err != nil {
		t.Fatalf("create project workflows dir: %v", err)
	}

	// No user formula either — embedded should be extracted and used.
	res, err := workflow.EnsureFormula("default-work", repoPath)
	if err != nil {
		t.Fatalf("EnsureFormula: %v", err)
	}
	if res.Tier != workflow.TierEmbedded {
		t.Errorf("tier: got %q, want %q", res.Tier, workflow.TierEmbedded)
	}

	// Verify the embedded formula was extracted to user-level path.
	extractedManifest := filepath.Join(solHome, "formulas", "default-work", "manifest.toml")
	if _, err := os.Stat(extractedManifest); os.IsNotExist(err) {
		t.Error("embedded default-work should be extracted to $SOL_HOME/formulas/")
	}
}

func TestProjectShadowsEmbeddedDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Create a custom default-work at project tier — should shadow the embedded one.
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	makeFormula(t, projectBase, "default-work", "custom project default-work")

	res, err := workflow.EnsureFormula("default-work", repoPath)
	if err != nil {
		t.Fatalf("EnsureFormula: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("tier: got %q, want %q — project should shadow embedded", res.Tier, workflow.TierProject)
	}

	// Verify embedded was NOT extracted (project took priority).
	extractedManifest := filepath.Join(solHome, "formulas", "default-work", "manifest.toml")
	if _, err := os.Stat(extractedManifest); !os.IsNotExist(err) {
		t.Error("embedded default-work should NOT be extracted when project tier matches")
	}

	// Verify the resolved formula has the project description.
	m, err := workflow.LoadManifest(res.Path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Description != "custom project default-work" {
		t.Errorf("description: got %q, want %q", m.Description, "custom project default-work")
	}
}

func TestWorkflowListShowsCorrectTiers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Set up three tiers:
	// - project-only: exists only at project tier
	// - shared-formula: exists at project AND user tiers (user is shadowed)
	// - default-work: exists at project tier (shadows embedded)
	// - user-only: exists only at user tier
	// - (embedded defaults exist automatically)
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	userBase := filepath.Join(solHome, "formulas")

	makeFormula(t, projectBase, "project-only", "project-only formula")
	makeFormula(t, projectBase, "shared-formula", "project tier shared")
	makeFormula(t, userBase, "shared-formula", "user tier shared")
	makeFormula(t, projectBase, "default-work", "custom project default")
	makeFormula(t, userBase, "user-only", "user-only formula")

	entries, err := workflow.ListFormulas(repoPath)
	if err != nil {
		t.Fatalf("ListFormulas: %v", err)
	}

	// Build a lookup map: name → list of entries (multiple tiers possible).
	type entryKey struct {
		name string
		tier workflow.FormulaTier
	}
	lookup := map[entryKey]workflow.FormulaEntry{}
	for _, e := range entries {
		lookup[entryKey{e.Name, e.Tier}] = e
	}

	// project-only: tier=project, not shadowed.
	if e, ok := lookup[entryKey{"project-only", workflow.TierProject}]; !ok {
		t.Error("project-only should appear at project tier")
	} else if e.Shadowed {
		t.Error("project-only should NOT be shadowed")
	}

	// shared-formula at project tier: not shadowed.
	if e, ok := lookup[entryKey{"shared-formula", workflow.TierProject}]; !ok {
		t.Error("shared-formula should appear at project tier")
	} else if e.Shadowed {
		t.Error("shared-formula at project tier should NOT be shadowed")
	}

	// shared-formula at user tier: IS shadowed by project.
	if e, ok := lookup[entryKey{"shared-formula", workflow.TierUser}]; !ok {
		t.Error("shared-formula should appear at user tier")
	} else if !e.Shadowed {
		t.Error("shared-formula at user tier should be shadowed by project tier")
	}

	// default-work at project tier: not shadowed.
	if e, ok := lookup[entryKey{"default-work", workflow.TierProject}]; !ok {
		t.Error("default-work should appear at project tier")
	} else if e.Shadowed {
		t.Error("default-work at project tier should NOT be shadowed")
	}

	// default-work at embedded tier: IS shadowed by project.
	if e, ok := lookup[entryKey{"default-work", workflow.TierEmbedded}]; !ok {
		t.Error("default-work should appear at embedded tier")
	} else if !e.Shadowed {
		t.Error("default-work at embedded tier should be shadowed by project tier")
	}

	// user-only at user tier: not shadowed.
	if e, ok := lookup[entryKey{"user-only", workflow.TierUser}]; !ok {
		t.Error("user-only should appear at user tier")
	} else if e.Shadowed {
		t.Error("user-only should NOT be shadowed")
	}
}

func TestCastWithProjectWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()
	logger := events.NewLogger(solHome)

	world := "ember"

	// Create project-level formula in the managed repo path.
	repoPath := config.RepoPath(world)
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	formulaDir := filepath.Join(projectBase, "project-cast-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create project formula dir: %v", err)
	}

	manifest := `name = "project-cast-formula"
type = "agent"
description = "Project-level formula for cast test"

[variables]
[variables.issue]
required = true

[[steps]]
id = "project-step"
title = "Project Step"
instructions = "steps/01.md"
`
	if err := os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Project workflow step for {{issue}}.\n"), 0o644); err != nil {
		t.Fatalf("write step: %v", err)
	}

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("ProjectBot", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Project WF task", "Test project workflow", "operator", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast with the project-level formula.
	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:      world,
		AgentName:  "ProjectBot",
		SourceRepo: sourceRepo,
		Formula:    "project-cast-formula",
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("cast with project formula: %v", err)
	}

	if result.Formula != "project-cast-formula" {
		t.Errorf("result formula: got %q, want project-cast-formula", result.Formula)
	}

	// Verify workflow state was created.
	state, err := workflow.ReadState(world, "ProjectBot", "agent")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state == nil {
		t.Fatal("state should not be nil after cast with formula")
	}
	if state.CurrentStep != "project-step" {
		t.Errorf("current step: got %q, want project-step", state.CurrentStep)
	}

	// Verify step instructions contain the rendered variable.
	step, err := workflow.ReadCurrentStep(world, "ProjectBot", "agent")
	if err != nil {
		t.Fatalf("ReadCurrentStep: %v", err)
	}
	if !strings.Contains(step.Instructions, itemID) {
		t.Errorf("step instructions should contain writ ID %s, got: %s", itemID, step.Instructions)
	}
	if !strings.Contains(step.Instructions, "Project workflow step") {
		t.Error("step instructions should contain project workflow text")
	}

	// Full cycle: advance → done → resolve.
	_, done, err := workflow.Advance(world, "ProjectBot", "agent")
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if !done {
		t.Error("should be done after single step")
	}

	if err := os.WriteFile(filepath.Join(result.WorktreeDir, "work.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}

	_, err = dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:     world,
		AgentName: "ProjectBot",
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Verify workflow cleaned up.
	wfDir := filepath.Join(solHome, world, "outposts", "ProjectBot", ".workflow")
	if _, err := os.Stat(wfDir); !os.IsNotExist(err) {
		t.Error(".workflow/ should be removed after resolve")
	}

	assertEventEmitted(t, solHome, events.EventWorkflowInstantiate)
	assertEventEmitted(t, solHome, events.EventResolve)
}

func TestInstantiateResolvesProjectTier(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	agent := "TierBot"

	// Create formula at both project and user tiers with different content.
	repoPath := config.RepoPath(world)
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	userBase := filepath.Join(solHome, "formulas")

	// Project tier formula.
	projectDir := filepath.Join(projectBase, "tier-test")
	projectSteps := filepath.Join(projectDir, "steps")
	if err := os.MkdirAll(projectSteps, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "manifest.toml"), []byte(`name = "tier-test"
type = "agent"
description = "Project version"

[variables]
[variables.issue]
required = true

[[steps]]
id = "p1"
title = "Project Step"
instructions = "steps/01.md"
`), 0o644); err != nil {
		t.Fatalf("write project manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectSteps, "01.md"), []byte("PROJECT INSTRUCTIONS for {{issue}}\n"), 0o644); err != nil {
		t.Fatalf("write project step: %v", err)
	}

	// User tier formula (same name, different content).
	userDir := filepath.Join(userBase, "tier-test")
	userSteps := filepath.Join(userDir, "steps")
	if err := os.MkdirAll(userSteps, 0o755); err != nil {
		t.Fatalf("create user dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "manifest.toml"), []byte(`name = "tier-test"
type = "agent"
description = "User version"

[variables]
[variables.issue]
required = true

[[steps]]
id = "u1"
title = "User Step"
instructions = "steps/01.md"
`), 0o644); err != nil {
		t.Fatalf("write user manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSteps, "01.md"), []byte("USER INSTRUCTIONS for {{issue}}\n"), 0o644); err != nil {
		t.Fatalf("write user step: %v", err)
	}

	// Create outpost dir for the agent.
	agentDir := filepath.Join(solHome, world, "outposts", agent)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("create agent dir: %v", err)
	}

	// Instantiate — should use project tier.
	inst, state, err := workflow.Instantiate(world, agent, "agent", "tier-test", map[string]string{"issue": "sol-abcd1234"})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}

	if inst.Formula != "tier-test" {
		t.Errorf("formula: got %q, want tier-test", inst.Formula)
	}
	if state.CurrentStep != "p1" {
		t.Errorf("current step: got %q, want p1 (project tier)", state.CurrentStep)
	}

	// Read the step — instructions should be from project tier.
	step, err := workflow.ReadCurrentStep(world, agent, "agent")
	if err != nil {
		t.Fatalf("ReadCurrentStep: %v", err)
	}
	if !strings.Contains(step.Instructions, "PROJECT INSTRUCTIONS") {
		t.Errorf("expected project-tier instructions, got: %s", step.Instructions)
	}
	if strings.Contains(step.Instructions, "USER INSTRUCTIONS") {
		t.Error("should NOT contain user-tier instructions")
	}
}

func TestBranchChangesProjectWorkflows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"

	// Create managed repo as a real git repo.
	repoPath := config.RepoPath(world)
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	runGit(t, repoPath, "init")

	// On main branch: create a project workflow "branch-formula".
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	makeFormula(t, projectBase, "branch-formula", "main branch version")
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "add branch-formula on main")

	// Verify it resolves from project tier.
	res, err := workflow.EnsureFormula("branch-formula", repoPath)
	if err != nil {
		t.Fatalf("EnsureFormula on main: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("main branch tier: got %q, want project", res.Tier)
	}

	// Create a feature branch that adds a different formula and removes branch-formula.
	runGit(t, repoPath, "checkout", "-b", "feature-branch")
	os.RemoveAll(filepath.Join(projectBase, "branch-formula"))
	makeFormula(t, projectBase, "feature-formula", "feature branch only")
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "replace branch-formula with feature-formula")

	// On feature branch: branch-formula should NOT be found at project tier.
	_, err = workflow.EnsureFormula("branch-formula", repoPath)
	if err == nil {
		t.Error("branch-formula should NOT resolve on feature branch (removed)")
	}

	// feature-formula should resolve on feature branch.
	res, err = workflow.EnsureFormula("feature-formula", repoPath)
	if err != nil {
		t.Fatalf("EnsureFormula for feature-formula: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("feature branch tier: got %q, want project", res.Tier)
	}

	// ListFormulas should show feature-formula but not branch-formula on this branch.
	entries, err := workflow.ListFormulas(repoPath)
	if err != nil {
		t.Fatalf("ListFormulas on feature: %v", err)
	}
	foundFeature := false
	foundBranch := false
	for _, e := range entries {
		if e.Name == "feature-formula" && e.Tier == workflow.TierProject {
			foundFeature = true
		}
		if e.Name == "branch-formula" && e.Tier == workflow.TierProject {
			foundBranch = true
		}
	}
	if !foundFeature {
		t.Error("feature-formula should appear in list on feature branch")
	}
	if foundBranch {
		t.Error("branch-formula should NOT appear in list on feature branch")
	}

	// Switch back to main — branch-formula should be back.
	runGit(t, repoPath, "checkout", "main")
	res, err = workflow.EnsureFormula("branch-formula", repoPath)
	if err != nil {
		t.Fatalf("EnsureFormula after checkout main: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("back on main tier: got %q, want project", res.Tier)
	}

	// feature-formula should NOT be available on main.
	_, err = workflow.EnsureFormula("feature-formula", repoPath)
	if err == nil {
		t.Error("feature-formula should NOT resolve on main branch")
	}
}

func TestWorkflowListCLIShowsTiers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnvWithRepo(t)

	world := "ember"
	initWorld(t, solHome, world)

	// Create a project-level formula in the managed repo path.
	repoPath := config.RepoPath(world)
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	makeFormula(t, projectBase, "cli-project-wf", "CLI project workflow")

	// Create a user-level formula.
	userBase := filepath.Join(solHome, "formulas")
	makeFormula(t, userBase, "cli-user-wf", "CLI user workflow")

	// Run sol workflow list --world=ember --json.
	out, err := runGT(t, solHome, "workflow", "list", "--world="+world, "--json")
	if err != nil {
		t.Fatalf("sol workflow list: %v: %s", err, out)
	}

	var entries []workflow.FormulaEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal list output: %v\nraw: %s", err, out)
	}

	// Verify our formulas appear with correct tiers.
	tierMap := map[string]workflow.FormulaTier{}
	for _, e := range entries {
		if !e.Shadowed {
			tierMap[e.Name] = e.Tier
		}
	}

	if tier, ok := tierMap["cli-project-wf"]; !ok {
		t.Error("cli-project-wf should appear in list")
	} else if tier != workflow.TierProject {
		t.Errorf("cli-project-wf tier: got %q, want project", tier)
	}

	if tier, ok := tierMap["cli-user-wf"]; !ok {
		t.Error("cli-user-wf should appear in list")
	} else if tier != workflow.TierUser {
		t.Errorf("cli-user-wf tier: got %q, want user", tier)
	}

	// Embedded defaults should be present too.
	if tier, ok := tierMap["default-work"]; !ok {
		t.Error("default-work should appear in list")
	} else if tier != workflow.TierEmbedded {
		t.Errorf("default-work tier: got %q, want embedded", tier)
	}
}

func TestWorkflowListCLIShowsShadowed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnvWithRepo(t)

	world := "ember"
	initWorld(t, solHome, world)

	// Create default-work at project tier to shadow the embedded one.
	repoPath := config.RepoPath(world)
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	makeFormula(t, projectBase, "default-work", "project override of default-work")

	// Run with --all to see shadowed entries.
	out, err := runGT(t, solHome, "workflow", "list", "--world="+world, "--json", "--all")
	if err != nil {
		t.Fatalf("sol workflow list --all: %v: %s", err, out)
	}

	var entries []workflow.FormulaEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}

	// Find both default-work entries.
	var projectEntry, embeddedEntry *workflow.FormulaEntry
	for i := range entries {
		if entries[i].Name == "default-work" {
			switch entries[i].Tier {
			case workflow.TierProject:
				projectEntry = &entries[i]
			case workflow.TierEmbedded:
				embeddedEntry = &entries[i]
			}
		}
	}

	if projectEntry == nil {
		t.Fatal("default-work at project tier should appear with --all")
	}
	if projectEntry.Shadowed {
		t.Error("project-tier default-work should NOT be shadowed")
	}

	if embeddedEntry == nil {
		t.Fatal("default-work at embedded tier should appear with --all")
	}
	if !embeddedEntry.Shadowed {
		t.Error("embedded default-work should be shadowed by project tier")
	}
}
