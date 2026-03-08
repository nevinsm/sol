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
		WritID:  "sol-a1b2c3d4",
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
		WritID:  "sol-a1b2c3d4",
		Title:       "Add a README",
		Description: "Create a README.md file",
	}

	if err := InstallClaudeMD(dir, ctx); err != nil {
		t.Fatalf("InstallClaudeMD failed: %v", err)
	}

	path := filepath.Join(dir, "CLAUDE.local.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Toast") {
		t.Error("CLAUDE.local.md missing agent name")
	}
	if !strings.Contains(content, "sol-a1b2c3d4") {
		t.Error("CLAUDE.local.md missing writ ID")
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
		"merge processor for world myworld",
		"FORBIDDEN",
		"Patrol Protocol",
		"sol workflow current --world=myworld --agent=forge",
		"sol workflow advance --world=myworld --agent=forge",
		"sol forge check-unblocked --world=myworld",
		"sol forge ready --world=myworld --json",
		"sol forge claim --world=myworld --json",
		"sol forge sync --world=myworld",
		"git merge --squash",
		"git push origin HEAD:main",
		"sol forge mark-merged --world=myworld",
		"sol forge mark-failed --world=myworld",
		"sol forge create-resolution --world=myworld",
		"sol forge release --world=myworld",
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

	path := filepath.Join(dir, "CLAUDE.local.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Forge Agent") {
		t.Error("CLAUDE.local.md missing 'Forge Agent'")
	}
	if !strings.Contains(content, "myworld") {
		t.Error("CLAUDE.local.md missing world name")
	}
}

func TestGenerateClaudeMDWithModelTier(t *testing.T) {
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:       "myworld",
		WritID:  "sol-a1b2c3d4",
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
		WritID:  "sol-a1b2c3d4",
		Title:       "Test task",
		Description: "Testing no model tier",
	}

	content := GenerateClaudeMD(ctx)

	if strings.Contains(content, "## Model") {
		t.Error("GenerateClaudeMD should not contain Model section when ModelTier is empty")
	}
}

func TestGenerateEnvoyClaudeMD(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := GenerateEnvoyClaudeMD(ctx)

	checks := []string{
		"Envoy: scout (world: myworld)",
		"scout",
		"myworld",
		"sol resolve --world=myworld --agent=scout",
		"sol store create --world=myworld",
		"sol escalate --world=myworld --agent=scout",
		"sol status --world=myworld",
		".brief/memory.md",
		"200 lines",
		"Brief Maintenance",
		"human-supervised",
		"Three Modes",
		"Tethered work",
		"Self-service",
		"Freeform",
		"Submitting Work",
		"All code changes MUST go through",
		"Never use `git push` alone",
		"session stays alive",
		"Never push directly or bypass forge",
		".claude/sol-cli-reference.md",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("GenerateEnvoyClaudeMD missing %q", check)
		}
	}

	// Verify no wrong command names.
	for _, bad := range []string{
		"store create-item",
		"store list-items",
		"caravan add-items",
	} {
		if strings.Contains(content, bad) {
			t.Errorf("GenerateEnvoyClaudeMD should not contain %q", bad)
		}
	}

	// Verify tether check uses status command, not outpost path.
	if strings.Contains(content, "outposts") {
		t.Error("GenerateEnvoyClaudeMD should not reference outposts directory")
	}
}

func TestGenerateEnvoyClaudeMDDefaultBinary(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		// SolBinary intentionally empty
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, "sol resolve") {
		t.Error("GenerateEnvoyClaudeMD should default to 'sol' binary")
	}
}

func TestGenerateEnvoyClaudeMDWithPersona(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName:      "scout",
		World:          "myworld",
		SolBinary:      "sol",
		PersonaContent: "You are thoughtful and concise.\nAlways explain your reasoning.",
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, "## Persona") {
		t.Error("GenerateEnvoyClaudeMD missing Persona section")
	}
	if !strings.Contains(content, "You are thoughtful and concise.") {
		t.Error("GenerateEnvoyClaudeMD missing persona content")
	}
	if !strings.Contains(content, "Always explain your reasoning.") {
		t.Error("GenerateEnvoyClaudeMD missing second line of persona content")
	}
}

func TestGenerateEnvoyClaudeMDWithoutPersona(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if strings.Contains(content, "## Persona") {
		t.Error("GenerateEnvoyClaudeMD should not contain Persona section when PersonaContent is empty")
	}
}

func TestInstallEnvoyClaudeMD(t *testing.T) {
	dir := t.TempDir()
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		SolBinary: "sol",
	}

	if err := InstallEnvoyClaudeMD(dir, ctx); err != nil {
		t.Fatalf("InstallEnvoyClaudeMD failed: %v", err)
	}

	path := filepath.Join(dir, "CLAUDE.local.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "scout") {
		t.Error("CLAUDE.local.md missing agent name")
	}
	if !strings.Contains(content, "myworld") {
		t.Error("CLAUDE.local.md missing world name")
	}
}

func TestInstallEnvoyClaudeMDWithPersona(t *testing.T) {
	dir := t.TempDir()
	ctx := EnvoyClaudeMDContext{
		AgentName:      "scout",
		World:          "myworld",
		SolBinary:      "sol",
		PersonaContent: "Be direct and action-oriented.",
	}

	if err := InstallEnvoyClaudeMD(dir, ctx); err != nil {
		t.Fatalf("InstallEnvoyClaudeMD failed: %v", err)
	}

	path := filepath.Join(dir, "CLAUDE.local.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "## Persona") {
		t.Error("CLAUDE.local.md missing Persona section")
	}
	if !strings.Contains(content, "Be direct and action-oriented.") {
		t.Error("CLAUDE.local.md missing persona content")
	}
}

func TestInstallHooks(t *testing.T) {
	dir := t.TempDir()

	if err := InstallHooks(dir, "myworld", "Toast"); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// Verify no script file — values are inlined in the hook command.
	scriptPath := filepath.Join(dir, ".claude", "hooks", "session-start.sh")
	if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
		t.Error("session-start.sh should not exist — values are inlined in hook command")
	}

	// Verify settings.local.json exists and has correct structure.
	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings.local.json: %v", err)
	}

	var cfg HookConfig
	if err := json.Unmarshal(settingsData, &cfg); err != nil {
		t.Fatalf("failed to parse settings.local.json: %v", err)
	}

	groups, ok := cfg.Hooks["SessionStart"]
	if !ok {
		t.Fatal("settings.local.json missing SessionStart hook")
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 SessionStart matcher group, got %d", len(groups))
	}
	if len(groups[0].Hooks) != 1 {
		t.Fatalf("expected 1 hook handler, got %d", len(groups[0].Hooks))
	}
	if groups[0].Hooks[0].Type != "command" {
		t.Errorf("expected hook type 'command', got %q", groups[0].Hooks[0].Type)
	}

	wantCmd := "sol prime --world=myworld --agent=Toast"
	if groups[0].Hooks[0].Command != wantCmd {
		t.Errorf("hook command = %q, want %q", groups[0].Hooks[0].Command, wantCmd)
	}
}

func TestGenerateEnvoyClaudeMDMultiWritActive(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "Meridian",
		World:     "myworld",
		SolBinary: "sol",
		TetheredWrits: []WritSummary{
			{ID: "sol-aaa1", Title: "Task A", Kind: "code", Status: "tethered"},
			{ID: "sol-bbb2", Title: "Task B", Kind: "analysis", Status: "tethered"},
			{ID: "sol-ccc3", Title: "Task C", Kind: "code", Status: "tethered"},
		},
		ActiveWritID: "sol-bbb2",
		ActiveTitle:  "Task B",
		ActiveDesc:   "Analyze the system",
		ActiveKind:   "analysis",
		ActiveOutput: "/tmp/output/sol-bbb2",
	}

	content := GenerateEnvoyClaudeMD(ctx)

	// Active writ section.
	if !strings.Contains(content, "## Active Writ") {
		t.Error("missing Active Writ section")
	}
	if !strings.Contains(content, "sol-bbb2") {
		t.Error("missing active writ ID")
	}
	if !strings.Contains(content, "Task B") {
		t.Error("missing active writ title")
	}
	if !strings.Contains(content, "Analyze the system") {
		t.Error("missing active writ description")
	}

	// Background writs.
	if !strings.Contains(content, "## Background Writs") {
		t.Error("missing Background Writs section")
	}
	if !strings.Contains(content, "Task A") {
		t.Error("missing background writ 'Task A'")
	}
	if !strings.Contains(content, "Task C") {
		t.Error("missing background writ 'Task C'")
	}

	// Constraint text.
	if !strings.Contains(content, "Work only on your active writ") {
		t.Error("missing constraint text")
	}

	// Should still contain base envoy content.
	if !strings.Contains(content, "Envoy: Meridian") {
		t.Error("missing envoy identity header")
	}
}

func TestGenerateEnvoyClaudeMDMultiWritNoActive(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "Meridian",
		World:     "myworld",
		SolBinary: "sol",
		TetheredWrits: []WritSummary{
			{ID: "sol-aaa1", Title: "Task A", Kind: "code", Status: "tethered"},
			{ID: "sol-bbb2", Title: "Task B", Kind: "code", Status: "tethered"},
		},
		// No ActiveWritID set.
	}

	content := GenerateEnvoyClaudeMD(ctx)

	// Wait message.
	if !strings.Contains(content, "Wait for the operator to activate one") {
		t.Error("missing wait-for-activation message")
	}
	if !strings.Contains(content, "2 tethered writs") {
		t.Error("missing tethered writ count")
	}

	// All writs listed.
	if !strings.Contains(content, "Task A") {
		t.Error("missing writ 'Task A'")
	}
	if !strings.Contains(content, "Task B") {
		t.Error("missing writ 'Task B'")
	}

	// Should NOT have Active Writ section.
	if strings.Contains(content, "## Active Writ") {
		t.Error("no-active persona should not have Active Writ section")
	}
}

func TestGenerateClaudeMDOutpostNoBackgroundSection(t *testing.T) {
	// Outpost agents get TetheredWrits empty — no background section.
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:       "myworld",
		WritID:      "sol-a1b2c3d4",
		Title:       "Add a README",
		Description: "Create a README.md file",
		Kind:        "code",
	}

	content := GenerateClaudeMD(ctx)

	if !strings.Contains(content, "Outpost Agent: Toast") {
		t.Error("missing outpost header")
	}
	if strings.Contains(content, "Background Writs") {
		t.Error("outpost GenerateClaudeMD should NOT contain Background Writs section")
	}
	if strings.Contains(content, "Tethered Writs") {
		t.Error("outpost GenerateClaudeMD should NOT contain Tethered Writs section")
	}
}

func TestGenerateGovernorClaudeMDMultiWrit(t *testing.T) {
	ctx := GovernorClaudeMDContext{
		World:     "myworld",
		SolBinary: "sol",
		MirrorDir: "../repo",
		TetheredWrits: []WritSummary{
			{ID: "sol-aaa1", Title: "Plan feature X", Kind: "code", Status: "tethered"},
			{ID: "sol-bbb2", Title: "Research Y", Kind: "analysis", Status: "tethered"},
		},
		ActiveWritID: "sol-aaa1",
		ActiveTitle:  "Plan feature X",
		ActiveDesc:   "Create writs for feature X",
		ActiveKind:   "code",
	}

	content := GenerateGovernorClaudeMD(ctx)

	if !strings.Contains(content, "## Active Writ") {
		t.Error("missing Active Writ section")
	}
	if !strings.Contains(content, "Plan feature X") {
		t.Error("missing active writ title")
	}
	if !strings.Contains(content, "## Background Writs") {
		t.Error("missing Background Writs section")
	}
	if !strings.Contains(content, "Research Y") {
		t.Error("missing background writ")
	}
	// Should still contain governor identity.
	if !strings.Contains(content, "Governor (world: myworld)") {
		t.Error("missing governor header")
	}
}
