package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateClaudeMD(t *testing.T) {
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:         "myworld",
		WorkItemID:  "sol-a1b2c3d4",
		Title:       "Add a README",
		Description: "Create a README.md file with project info",
	}

	content := GenerateClaudeMD(ctx)

	checks := []string{
		"Outpost Agent: Toast (world: myworld)",
		"sol-a1b2c3d4",
		"Add a README",
		"Create a README.md file with project info",
		"sol resolve",
		"sol escalate",
		"isolated git worktree",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("GenerateClaudeMD missing %q", check)
		}
	}
}

func TestInstallClaudeMD(t *testing.T) {
	dir := t.TempDir()
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:         "myworld",
		WorkItemID:  "sol-a1b2c3d4",
		Title:       "Add a README",
		Description: "Create a README.md file",
	}

	if err := InstallClaudeMD(dir, ctx); err != nil {
		t.Fatalf("InstallClaudeMD failed: %v", err)
	}

	path := filepath.Join(dir, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Toast") {
		t.Error("CLAUDE.md missing agent name")
	}
	if !strings.Contains(content, "sol-a1b2c3d4") {
		t.Error("CLAUDE.md missing work item ID")
	}
}

func TestGenerateForgeClaudeMD(t *testing.T) {
	ctx := ForgeClaudeMDContext{
		World:          "myworld",
		TargetBranch: "main",
		WorktreeDir:  "/home/user/sol/myworld/forge/worktree",
		QualityGates: []string{"go test ./...", "go vet ./..."},
	}

	content := GenerateForgeClaudeMD(ctx)

	checks := []string{
		"Forge Agent (world: myworld)",
		"merge processor, NOT a developer",
		"FORBIDDEN",
		"Patrol Loop",
		"sol forge check-unblocked myworld",
		"sol forge ready myworld --json",
		"sol forge claim myworld --json",
		"sol forge run-gates myworld",
		"sol forge push myworld",
		"sol forge mark-merged myworld",
		"sol forge mark-failed myworld",
		"sol forge create-resolution myworld",
		"git rebase origin/main",
		"Conflict Judgment Framework",
		"Sequential Rebase Rule",
		"go test ./...",
		"go vet ./...",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("GenerateForgeClaudeMD missing %q", check)
		}
	}
}

func TestInstallForgeClaudeMD(t *testing.T) {
	dir := t.TempDir()
	ctx := ForgeClaudeMDContext{
		World:          "myworld",
		TargetBranch: "main",
		WorktreeDir:  dir,
		QualityGates: []string{"go test ./..."},
	}

	if err := InstallForgeClaudeMD(dir, ctx); err != nil {
		t.Fatalf("InstallForgeClaudeMD failed: %v", err)
	}

	path := filepath.Join(dir, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Forge Agent") {
		t.Error("CLAUDE.md missing 'Forge Agent'")
	}
	if !strings.Contains(content, "myworld") {
		t.Error("CLAUDE.md missing world name")
	}
}

func TestGenerateClaudeMDWithModelTier(t *testing.T) {
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:       "myworld",
		WorkItemID:  "sol-a1b2c3d4",
		Title:       "Test task",
		Description: "Testing model tier",
		ModelTier:   "opus",
	}

	content := GenerateClaudeMD(ctx)

	if !strings.Contains(content, "## Model") {
		t.Error("GenerateClaudeMD missing Model section header")
	}
	if !strings.Contains(content, "model tier: opus") {
		t.Error("GenerateClaudeMD missing model tier value")
	}
}

func TestGenerateClaudeMDWithoutModelTier(t *testing.T) {
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:       "myworld",
		WorkItemID:  "sol-a1b2c3d4",
		Title:       "Test task",
		Description: "Testing no model tier",
	}

	content := GenerateClaudeMD(ctx)

	if strings.Contains(content, "## Model") {
		t.Error("GenerateClaudeMD should not contain Model section when ModelTier is empty")
	}
}

func TestInstallHooks(t *testing.T) {
	dir := t.TempDir()

	if err := InstallHooks(dir, "myworld", "Toast"); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// Verify hook script exists and has correct content.
	scriptPath := filepath.Join(dir, ".claude", "hooks", "session-start.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read session-start.sh: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "sol prime") {
		t.Error("session-start.sh missing 'sol prime' command")
	}
	if !strings.Contains(content, "$SOL_WORLD") {
		t.Error("session-start.sh missing $SOL_WORLD reference")
	}
	if !strings.Contains(content, "$SOL_AGENT") {
		t.Error("session-start.sh missing $SOL_AGENT reference")
	}

	// Verify script is executable.
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("failed to stat session-start.sh: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("session-start.sh is not executable")
	}

	// Verify settings.local.json exists and has correct structure.
	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings.local.json: %v", err)
	}

	var cfg hookConfig
	if err := json.Unmarshal(settingsData, &cfg); err != nil {
		t.Fatalf("failed to parse settings.local.json: %v", err)
	}

	hooks, ok := cfg.Hooks["SessionStart"]
	if !ok {
		t.Fatal("settings.local.json missing SessionStart hook")
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 SessionStart hook, got %d", len(hooks))
	}
	if hooks[0].Type != "command" {
		t.Errorf("expected hook type 'command', got %q", hooks[0].Type)
	}
	if hooks[0].Command != ".claude/hooks/session-start.sh" {
		t.Errorf("expected hook command '.claude/hooks/session-start.sh', got %q", hooks[0].Command)
	}
}
