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
		World:       "myworld",
		WritID:      "sol-a1b2c3d4",
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
		World:       "myworld",
		WritID:      "sol-a1b2c3d4",
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

	// Verify skills installed.
	skills := RoleSkills("outpost")
	for _, name := range skills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("outpost skill %q should be installed: %v", name, err)
		}
	}
}

func TestGenerateClaudeMDWithModelTier(t *testing.T) {
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:       "myworld",
		WritID:      "sol-a1b2c3d4",
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
		WritID:      "sol-a1b2c3d4",
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
		"Never push directly or bypass forge",
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

func TestGenerateEnvoyClaudeMDNoPersonaSection(t *testing.T) {
	// Persona content is now delivered via system prompt, not CLAUDE.local.md.
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if strings.Contains(content, "## Persona") {
		t.Error("GenerateEnvoyClaudeMD should not contain Persona section (persona is now in system prompt)")
	}
}

func TestGenerateEnvoyClaudeMDIdentitySections(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, "## Identity") {
		t.Error("GenerateEnvoyClaudeMD missing Identity section")
	}
	if !strings.Contains(content, "scout") {
		t.Error("GenerateEnvoyClaudeMD missing agent name")
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

	// Verify skills installed.
	skills := RoleSkills("envoy")
	for _, name := range skills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("envoy skill %q should be installed: %v", name, err)
		}
	}
}

func TestInstallEnvoyClaudeMDNoPersonaInOutput(t *testing.T) {
	// Persona content is no longer embedded in CLAUDE.local.md — it goes to system prompt.
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
	if strings.Contains(content, "## Persona") {
		t.Error("CLAUDE.local.md should not contain Persona section (persona is now in system prompt)")
	}
}

func TestInstallHooks(t *testing.T) {
	dir := t.TempDir()

	cfg := BaseHooks(HookOptions{
		Role:             "outpost",
		SessionStartCmds: []string{"sol prime --world=myworld --agent=Toast"},
		PreCompactCmd:    "sol prime --world=myworld --agent=Toast --compact",
		NudgeDrainCmd:    "sol nudge drain --world=myworld --agent=Toast",
	})
	if err := WriteHookSettings(dir, cfg); err != nil {
		t.Fatalf("WriteHookSettings failed: %v", err)
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

	var hookCfg HookConfig
	if err := json.Unmarshal(settingsData, &hookCfg); err != nil {
		t.Fatalf("failed to parse settings.local.json: %v", err)
	}

	groups, ok := hookCfg.Hooks["SessionStart"]
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
		WritContext: WritContext{
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
		},
	}

	content := GenerateEnvoyClaudeMD(ctx)

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
	if !strings.Contains(content, "## Background Writs") {
		t.Error("missing Background Writs section")
	}
	if !strings.Contains(content, "Task A") {
		t.Error("missing background writ 'Task A'")
	}
	if !strings.Contains(content, "Task C") {
		t.Error("missing background writ 'Task C'")
	}
	if !strings.Contains(content, "Work only on your active writ") {
		t.Error("missing constraint text")
	}
	if !strings.Contains(content, "Envoy: Meridian") {
		t.Error("missing envoy identity header")
	}
}

func TestGenerateEnvoyClaudeMDMultiWritNoActive(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "Meridian",
		World:     "myworld",
		SolBinary: "sol",
		WritContext: WritContext{
			TetheredWrits: []WritSummary{
				{ID: "sol-aaa1", Title: "Task A", Kind: "code", Status: "tethered"},
				{ID: "sol-bbb2", Title: "Task B", Kind: "code", Status: "tethered"},
			},
			// No ActiveWritID set.
		},
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, "Wait for the operator to activate one") {
		t.Error("missing wait-for-activation message")
	}
	if !strings.Contains(content, "2 tethered writs") {
		t.Error("missing tethered writ count")
	}
	if !strings.Contains(content, "Task A") {
		t.Error("missing writ 'Task A'")
	}
	if !strings.Contains(content, "Task B") {
		t.Error("missing writ 'Task B'")
	}
	if strings.Contains(content, "## Active Writ") {
		t.Error("no-active persona should not have Active Writ section")
	}
}

func TestGenerateClaudeMDOutpostNoBackgroundSection(t *testing.T) {
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
		WritContext: WritContext{
			TetheredWrits: []WritSummary{
				{ID: "sol-aaa1", Title: "Plan feature X", Kind: "code", Status: "tethered"},
				{ID: "sol-bbb2", Title: "Research Y", Kind: "analysis", Status: "tethered"},
			},
			ActiveWritID: "sol-aaa1",
			ActiveTitle:  "Plan feature X",
			ActiveDesc:   "Create writs for feature X",
			ActiveKind:   "code",
		},
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
	if !strings.Contains(content, "Governor (world: myworld)") {
		t.Error("missing governor header")
	}
}
