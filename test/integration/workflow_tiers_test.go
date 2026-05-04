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
	"github.com/nevinsm/sol/internal/workflow"
)

// --- Three-Tier Workflow Resolution Integration Tests ---
//
// These tests verify the project > user > embedded resolution order
// defined in ADR-0021. Project workflows live in {repo}/.sol/workflows/,
// user workflows in $SOL_HOME/workflows/, and embedded workflows are
// compiled into the binary.

// makeWorkflow creates a minimal workflow directory with a single step.
func makeWorkflow(t *testing.T, dir, name, description string) {
	t.Helper()
	workflowDir := filepath.Join(dir, name)
	stepsDir := filepath.Join(workflowDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("create workflow dir %s: %v", name, err)
	}
	manifest := `name = "` + name + `"
type = "workflow"
description = "` + description + `"

[variables]
[variables.issue]
required = true

[[steps]]
id = "only"
title = "Only Step"
instructions = "steps/01.md"
`
	if err := os.WriteFile(filepath.Join(workflowDir, "manifest.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest for %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Instructions for "+name+" ("+description+"): {{issue}}\n"), 0o644); err != nil {
		t.Fatalf("write step for %s: %v", name, err)
	}
}

func TestProjectWorkflowOverridesUser(t *testing.T) {
	skipUnlessIntegration(t)

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Create same workflow at project and user tiers.
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	userBase := filepath.Join(solHome, "workflows")

	makeWorkflow(t, projectBase, "shared-workflow", "project tier")
	makeWorkflow(t, userBase, "shared-workflow", "user tier")

	// Resolve — project tier should win.
	res, err := workflow.Resolve("shared-workflow", repoPath)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("tier: got %q, want %q", res.Tier, workflow.TierProject)
	}
	if !strings.Contains(res.Path, ".sol/workflows/shared-workflow") {
		t.Errorf("path should be project-level: %s", res.Path)
	}
}

func TestUserWorkflowFallback(t *testing.T) {
	skipUnlessIntegration(t)

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Create workflow only at user tier. Project tier directory exists but
	// does not contain this workflow.
	if err := os.MkdirAll(filepath.Join(repoPath, ".sol", "workflows"), 0o755); err != nil {
		t.Fatalf("create project workflows dir: %v", err)
	}
	userBase := filepath.Join(solHome, "workflows")
	makeWorkflow(t, userBase, "user-only-workflow", "user tier")

	// Resolve — user tier should win.
	res, err := workflow.Resolve("user-only-workflow", repoPath)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Tier != workflow.TierUser {
		t.Errorf("tier: got %q, want %q", res.Tier, workflow.TierUser)
	}
}

func TestEmbeddedWorkflowFallback(t *testing.T) {
	skipUnlessIntegration(t)

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Ensure the repo path exists but has no project-level code-review.
	if err := os.MkdirAll(filepath.Join(repoPath, ".sol", "workflows"), 0o755); err != nil {
		t.Fatalf("create project workflows dir: %v", err)
	}

	// No user workflow either — embedded should be extracted and used.
	res, err := workflow.Resolve("code-review", repoPath)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Tier != workflow.TierEmbedded {
		t.Errorf("tier: got %q, want %q", res.Tier, workflow.TierEmbedded)
	}

	// Verify the embedded workflow was extracted to user-level path.
	extractedManifest := filepath.Join(solHome, "workflows", "code-review", "manifest.toml")
	if _, err := os.Stat(extractedManifest); os.IsNotExist(err) {
		t.Error("embedded code-review should be extracted to $SOL_HOME/workflows/")
	}
}

func TestProjectShadowsEmbeddedDefault(t *testing.T) {
	skipUnlessIntegration(t)

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Create a custom code-review at project tier — should shadow the embedded one.
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	makeWorkflow(t, projectBase, "code-review", "custom project code-review")

	res, err := workflow.Resolve("code-review", repoPath)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("tier: got %q, want %q — project should shadow embedded", res.Tier, workflow.TierProject)
	}

	// Verify embedded was NOT extracted (project took priority).
	extractedManifest := filepath.Join(solHome, "workflows", "code-review", "manifest.toml")
	if _, err := os.Stat(extractedManifest); !os.IsNotExist(err) {
		t.Error("embedded code-review should NOT be extracted when project tier matches")
	}

	// Verify the resolved workflow has the project description.
	m, err := workflow.LoadManifest(res.Path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Description != "custom project code-review" {
		t.Errorf("description: got %q, want %q", m.Description, "custom project code-review")
	}
}

func TestWorkflowListShowsCorrectTiers(t *testing.T) {
	skipUnlessIntegration(t)

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store: %v", err)
	}

	world := "ember"
	repoPath := config.RepoPath(world)

	// Set up three tiers:
	// - project-only: exists only at project tier
	// - shared-workflow: exists at project AND user tiers (user is shadowed)
	// - code-review: exists at project tier (shadows embedded)
	// - user-only: exists only at user tier
	// - (embedded defaults exist automatically)
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	userBase := filepath.Join(solHome, "workflows")

	makeWorkflow(t, projectBase, "project-only", "project-only workflow")
	makeWorkflow(t, projectBase, "shared-workflow", "project tier shared")
	makeWorkflow(t, userBase, "shared-workflow", "user tier shared")
	makeWorkflow(t, projectBase, "code-review", "custom project default")
	makeWorkflow(t, userBase, "user-only", "user-only workflow")

	entries, err := workflow.List(repoPath)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Build a lookup map: name → list of entries (multiple tiers possible).
	type entryKey struct {
		name string
		tier workflow.Tier
	}
	lookup := map[entryKey]workflow.Entry{}
	for _, e := range entries {
		lookup[entryKey{e.Name, e.Tier}] = e
	}

	// project-only: tier=project, not shadowed.
	if e, ok := lookup[entryKey{"project-only", workflow.TierProject}]; !ok {
		t.Error("project-only should appear at project tier")
	} else if e.Shadowed {
		t.Error("project-only should NOT be shadowed")
	}

	// shared-workflow at project tier: not shadowed.
	if e, ok := lookup[entryKey{"shared-workflow", workflow.TierProject}]; !ok {
		t.Error("shared-workflow should appear at project tier")
	} else if e.Shadowed {
		t.Error("shared-workflow at project tier should NOT be shadowed")
	}

	// shared-workflow at user tier: IS shadowed by project.
	if e, ok := lookup[entryKey{"shared-workflow", workflow.TierUser}]; !ok {
		t.Error("shared-workflow should appear at user tier")
	} else if !e.Shadowed {
		t.Error("shared-workflow at user tier should be shadowed by project tier")
	}

	// code-review at project tier: not shadowed.
	if e, ok := lookup[entryKey{"code-review", workflow.TierProject}]; !ok {
		t.Error("code-review should appear at project tier")
	} else if e.Shadowed {
		t.Error("code-review at project tier should NOT be shadowed")
	}

	// code-review at embedded tier: IS shadowed by project.
	if e, ok := lookup[entryKey{"code-review", workflow.TierEmbedded}]; !ok {
		t.Error("code-review should appear at embedded tier")
	} else if !e.Shadowed {
		t.Error("code-review at embedded tier should be shadowed by project tier")
	}

	// user-only at user tier: not shadowed.
	if e, ok := lookup[entryKey{"user-only", workflow.TierUser}]; !ok {
		t.Error("user-only should appear at user tier")
	} else if e.Shadowed {
		t.Error("user-only should NOT be shadowed")
	}
}

func TestCastWithProjectGuidelines(t *testing.T) {
	skipUnlessIntegration(t)

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	world := "ember"

	// Create project-level guidelines in the source repo's .sol/guidelines/.
	guidelinesDir := filepath.Join(sourceRepo, ".sol", "guidelines")
	if err := os.MkdirAll(guidelinesDir, 0o755); err != nil {
		t.Fatalf("create project guidelines dir: %v", err)
	}
	projectContent := "# Project Guidelines\nCustom project guidelines for {{issue}}.\n"
	if err := os.WriteFile(filepath.Join(guidelinesDir, "default.md"), []byte(projectContent), 0o644); err != nil {
		t.Fatalf("write project guidelines: %v", err)
	}

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("ProjectBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Project GL task", "Test project guidelines", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast — project-level guidelines should be picked up.
	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      world,
		AgentName:  "ProjectBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("cast with project guidelines: %v", err)
	}

	if result.Guidelines != "default" {
		t.Errorf("guidelines: got %q, want default", result.Guidelines)
	}

	// Verify .guidelines.md contains project-level content with variable substitution.
	data, err := os.ReadFile(filepath.Join(result.WorktreeDir, ".guidelines.md"))
	if err != nil {
		t.Fatalf("read .guidelines.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Project Guidelines") {
		t.Error(".guidelines.md should contain project guidelines content")
	}
	if !strings.Contains(content, itemID) {
		t.Errorf(".guidelines.md should contain writ ID %s after variable substitution", itemID)
	}
}

func TestBranchChangesProjectWorkflows(t *testing.T) {
	skipUnlessIntegration(t)

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

	// On main branch: create a project workflow "branch-workflow".
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	makeWorkflow(t, projectBase, "branch-workflow", "main branch version")
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "add branch-workflow on main")

	// Verify it resolves from project tier.
	res, err := workflow.Resolve("branch-workflow", repoPath)
	if err != nil {
		t.Fatalf("Resolve on main: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("main branch tier: got %q, want project", res.Tier)
	}

	// Create a feature branch that adds a different workflow and removes branch-workflow.
	runGit(t, repoPath, "checkout", "-b", "feature-branch")
	os.RemoveAll(filepath.Join(projectBase, "branch-workflow"))
	makeWorkflow(t, projectBase, "feature-workflow", "feature branch only")
	runGit(t, repoPath, "add", ".")
	runGit(t, repoPath, "commit", "-m", "replace branch-workflow with feature-workflow")

	// On feature branch: branch-workflow should NOT be found at project tier.
	_, err = workflow.Resolve("branch-workflow", repoPath)
	if err == nil {
		t.Error("branch-workflow should NOT resolve on feature branch (removed)")
	}

	// feature-workflow should resolve on feature branch.
	res, err = workflow.Resolve("feature-workflow", repoPath)
	if err != nil {
		t.Fatalf("Resolve for feature-workflow: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("feature branch tier: got %q, want project", res.Tier)
	}

	// List should show feature-workflow but not branch-workflow on this branch.
	entries, err := workflow.List(repoPath)
	if err != nil {
		t.Fatalf("List on feature: %v", err)
	}
	foundFeature := false
	foundBranch := false
	for _, e := range entries {
		if e.Name == "feature-workflow" && e.Tier == workflow.TierProject {
			foundFeature = true
		}
		if e.Name == "branch-workflow" && e.Tier == workflow.TierProject {
			foundBranch = true
		}
	}
	if !foundFeature {
		t.Error("feature-workflow should appear in list on feature branch")
	}
	if foundBranch {
		t.Error("branch-workflow should NOT appear in list on feature branch")
	}

	// Switch back to main — branch-workflow should be back.
	runGit(t, repoPath, "checkout", "main")
	res, err = workflow.Resolve("branch-workflow", repoPath)
	if err != nil {
		t.Fatalf("Resolve after checkout main: %v", err)
	}
	if res.Tier != workflow.TierProject {
		t.Errorf("back on main tier: got %q, want project", res.Tier)
	}

	// feature-workflow should NOT be available on main.
	_, err = workflow.Resolve("feature-workflow", repoPath)
	if err == nil {
		t.Error("feature-workflow should NOT resolve on main branch")
	}
}

func TestWorkflowListCLIShowsTiers(t *testing.T) {
	skipUnlessIntegration(t)

	solHome, _ := setupTestEnvWithRepo(t)

	world := "ember"
	initWorld(t, solHome, world)

	// Create a project-level workflow in the managed repo path.
	repoPath := config.RepoPath(world)
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	makeWorkflow(t, projectBase, "cli-project-wf", "CLI project workflow")

	// Create a user-level workflow.
	userBase := filepath.Join(solHome, "workflows")
	makeWorkflow(t, userBase, "cli-user-wf", "CLI user workflow")

	// Run sol workflow list --world=ember --json.
	out, err := runGT(t, solHome, "workflow", "list", "--world="+world, "--json")
	if err != nil {
		t.Fatalf("sol workflow list: %v: %s", err, out)
	}

	var entries []workflow.Entry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal list output: %v\nraw: %s", err, out)
	}

	// Verify our workflows appear with correct tiers.
	tierMap := map[string]workflow.Tier{}
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
	if tier, ok := tierMap["code-review"]; !ok {
		t.Error("code-review should appear in list")
	} else if tier != workflow.TierEmbedded {
		t.Errorf("code-review tier: got %q, want embedded", tier)
	}
}

func TestWorkflowListCLIShowsShadowed(t *testing.T) {
	skipUnlessIntegration(t)

	solHome, _ := setupTestEnvWithRepo(t)

	world := "ember"
	initWorld(t, solHome, world)

	// Create code-review at project tier to shadow the embedded one.
	repoPath := config.RepoPath(world)
	projectBase := filepath.Join(repoPath, ".sol", "workflows")
	makeWorkflow(t, projectBase, "code-review", "project override of code-review")

	// Run with --all to see shadowed entries.
	out, err := runGT(t, solHome, "workflow", "list", "--world="+world, "--json", "--all")
	if err != nil {
		t.Fatalf("sol workflow list --all: %v: %s", err, out)
	}

	var entries []workflow.Entry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out)
	}

	// Find both code-review entries.
	var projectEntry, embeddedEntry *workflow.Entry
	for i := range entries {
		if entries[i].Name == "code-review" {
			switch entries[i].Tier {
			case workflow.TierProject:
				projectEntry = &entries[i]
			case workflow.TierEmbedded:
				embeddedEntry = &entries[i]
			}
		}
	}

	if projectEntry == nil {
		t.Fatal("code-review at project tier should appear with --all")
	}
	if projectEntry.Shadowed {
		t.Error("project-tier code-review should NOT be shadowed")
	}

	if embeddedEntry == nil {
		t.Fatal("code-review at embedded tier should appear with --all")
	}
	if !embeddedEntry.Shadowed {
		t.Error("embedded code-review should be shadowed by project tier")
	}
}
