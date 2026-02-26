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
		Rig:         "myrig",
		WorkItemID:  "gt-a1b2c3d4",
		Title:       "Add a README",
		Description: "Create a README.md file with project info",
	}

	content := GenerateClaudeMD(ctx)

	checks := []string{
		"Polecat Agent: Toast (rig: myrig)",
		"gt-a1b2c3d4",
		"Add a README",
		"Create a README.md file with project info",
		"gt done",
		"gt escalate",
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
		Rig:         "myrig",
		WorkItemID:  "gt-a1b2c3d4",
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
	if !strings.Contains(content, "gt-a1b2c3d4") {
		t.Error("CLAUDE.md missing work item ID")
	}
}

func TestGenerateRefineryClaudeMD(t *testing.T) {
	ctx := RefineryClaudeMDContext{
		Rig:          "myrig",
		TargetBranch: "main",
		WorktreeDir:  "/home/user/gt/myrig/refinery/rig",
		QualityGates: []string{"go test ./...", "go vet ./..."},
	}

	content := GenerateRefineryClaudeMD(ctx)

	checks := []string{
		"Refinery Agent (rig: myrig)",
		"merge processor, NOT a developer",
		"FORBIDDEN",
		"Patrol Loop",
		"gt refinery check-unblocked myrig",
		"gt refinery ready myrig --json",
		"gt refinery claim myrig --json",
		"gt refinery run-gates myrig",
		"gt refinery push myrig",
		"gt refinery mark-merged myrig",
		"gt refinery mark-failed myrig",
		"gt refinery create-resolution myrig",
		"git rebase origin/main",
		"Conflict Judgment Framework",
		"Sequential Rebase Rule",
		"go test ./...",
		"go vet ./...",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("GenerateRefineryClaudeMD missing %q", check)
		}
	}
}

func TestInstallRefineryClaudeMD(t *testing.T) {
	dir := t.TempDir()
	ctx := RefineryClaudeMDContext{
		Rig:          "myrig",
		TargetBranch: "main",
		WorktreeDir:  dir,
		QualityGates: []string{"go test ./..."},
	}

	if err := InstallRefineryClaudeMD(dir, ctx); err != nil {
		t.Fatalf("InstallRefineryClaudeMD failed: %v", err)
	}

	path := filepath.Join(dir, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Refinery Agent") {
		t.Error("CLAUDE.md missing 'Refinery Agent'")
	}
	if !strings.Contains(content, "myrig") {
		t.Error("CLAUDE.md missing rig name")
	}
}

func TestInstallHooks(t *testing.T) {
	dir := t.TempDir()

	if err := InstallHooks(dir, "myrig", "Toast"); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// Verify hook script exists and has correct content.
	scriptPath := filepath.Join(dir, ".claude", "hooks", "session-start.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("failed to read session-start.sh: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "gt prime") {
		t.Error("session-start.sh missing 'gt prime' command")
	}
	if !strings.Contains(content, "$GT_RIG") {
		t.Error("session-start.sh missing $GT_RIG reference")
	}
	if !strings.Contains(content, "$GT_AGENT") {
		t.Error("session-start.sh missing $GT_AGENT reference")
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
