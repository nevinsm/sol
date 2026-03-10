package forge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/protocol"
)

func TestInstallForgePersona(t *testing.T) {
	t.Run("writes persona and hooks", func(t *testing.T) {
		dir := t.TempDir()
		world := "test-world"

		// Set SOL_HOME so WorktreePath resolves (used by forgeHookConfig).
		t.Setenv("SOL_HOME", dir)

		if err := InstallForgePersona(dir, world); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Check CLAUDE.local.md exists and has persona content.
		personaPath := filepath.Join(dir, "CLAUDE.local.md")
		data, err := os.ReadFile(personaPath)
		if err != nil {
			t.Fatalf("failed to read persona: %v", err)
		}
		persona := string(data)

		// Verify key persona sections.
		mustContain(t, persona, "Forge Merge Engineer — test-world")
		mustContain(t, persona, "What You Do")
		mustContain(t, persona, "What You Do NOT Do")
		mustContain(t, persona, "Conflict Resolution")
		mustContain(t, persona, "Gate Failures")
		mustContain(t, persona, ".forge-result.json")

		// Check settings.local.json exists and has hook config.
		settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
		settingsData, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("failed to read settings: %v", err)
		}

		var hookCfg protocol.HookConfig
		if err := json.Unmarshal(settingsData, &hookCfg); err != nil {
			t.Fatalf("failed to parse settings: %v", err)
		}

		// Verify PreToolUse hooks exist.
		preTool, ok := hookCfg.Hooks["PreToolUse"]
		if !ok || len(preTool) == 0 {
			t.Fatal("PreToolUse hooks should be configured")
		}

		// First hook should block EnterPlanMode.
		if preTool[0].Matcher != "EnterPlanMode" {
			t.Errorf("first PreToolUse matcher = %q, want EnterPlanMode", preTool[0].Matcher)
		}

		// Verify PreCompact hook exists.
		preCompact, ok := hookCfg.Hooks["PreCompact"]
		if !ok || len(preCompact) == 0 {
			t.Fatal("PreCompact hooks should be configured")
		}
		if !strings.Contains(preCompact[0].Hooks[0].Command, ".forge-injection.md") {
			t.Errorf("PreCompact hook should cat injection file, got: %s", preCompact[0].Hooks[0].Command)
		}
	})

	t.Run("idempotent — second call overwrites cleanly", func(t *testing.T) {
		dir := t.TempDir()
		world := "idempotent-world"

		t.Setenv("SOL_HOME", dir)

		// Install twice.
		if err := InstallForgePersona(dir, world); err != nil {
			t.Fatalf("first install: %v", err)
		}
		if err := InstallForgePersona(dir, world); err != nil {
			t.Fatalf("second install: %v", err)
		}

		// Verify content is correct (not doubled or corrupted).
		data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md"))
		if err != nil {
			t.Fatalf("failed to read persona: %v", err)
		}
		persona := string(data)

		// Should appear exactly once.
		count := strings.Count(persona, "Forge Merge Engineer")
		if count != 1 {
			t.Errorf("persona header appears %d times, want 1", count)
		}
	})
}

func TestForgePersonaContent(t *testing.T) {
	persona := ForgePersonaContent("prod-world")

	markers := []string{
		"Forge Merge Engineer — prod-world",
		"Squash merge the source branch",
		"Write new features or add functionality",
		"Conflict Resolution",
		"Gate Failures",
		".forge-result.json",
		"session will exit automatically",
		// New hardening markers.
		"Never Lose Work",
		"Never delete a branch",
		"Empty Branch and Reversion Detection",
		"processing exactly one merge request",
		"Commit Message Format",
	}

	missing := ForgePersonaContains(persona, markers)
	if len(missing) > 0 {
		t.Errorf("persona missing markers: %v", missing)
	}

	// Verify persona does NOT instruct agent to run /exit.
	if strings.Contains(persona, "/exit") {
		t.Error("persona should not mention /exit — session exit is handled by Stop hook")
	}
}

func TestForgeHookConfig(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	cfg := forgeHookConfig("test-world")

	preTool, ok := cfg.Hooks["PreToolUse"]
	if !ok {
		t.Fatal("PreToolUse hooks not configured")
	}

	// Check EnterPlanMode is blocked.
	found := false
	for _, group := range preTool {
		if group.Matcher == "EnterPlanMode" {
			found = true
			if len(group.Hooks) == 0 {
				t.Error("EnterPlanMode matcher has no hooks")
			}
			if group.Hooks[0].Type != "command" {
				t.Errorf("hook type = %q, want command", group.Hooks[0].Type)
			}
			if !strings.Contains(group.Hooks[0].Command, "exit 2") {
				t.Error("EnterPlanMode hook should exit 2")
			}
		}
	}
	if !found {
		t.Error("EnterPlanMode hook not found in PreToolUse")
	}

	// Forge role should have guard hooks (force push, branching, rm -rf) but
	// not outpost-specific guards (git reset --hard, push to main).
	hasForceGuard := false
	hasResetGuard := false
	hasPushMainGuard := false
	for _, group := range preTool {
		if strings.Contains(group.Matcher, "git push --force") {
			hasForceGuard = true
		}
		if strings.Contains(group.Matcher, "git reset --hard") {
			hasResetGuard = true
		}
		if strings.Contains(group.Matcher, "git push origin main") {
			hasPushMainGuard = true
		}
	}
	if !hasForceGuard {
		t.Error("forge should have force push guard")
	}
	if hasResetGuard {
		t.Error("forge should NOT have git reset --hard guard (needs it for sync)")
	}
	if hasPushMainGuard {
		t.Error("forge should NOT have push-to-main guard (pushes to main by design)")
	}

	// Check PreCompact hook exists.
	preCompact, ok := cfg.Hooks["PreCompact"]
	if !ok {
		t.Fatal("PreCompact hooks not configured")
	}
	if len(preCompact) == 0 || len(preCompact[0].Hooks) == 0 {
		t.Fatal("PreCompact should have at least one hook")
	}
	if !strings.Contains(preCompact[0].Hooks[0].Command, ".forge-injection.md") {
		t.Errorf("PreCompact hook should cat injection file, got: %s", preCompact[0].Hooks[0].Command)
	}

	// Check Stop hook exists and checks for result file.
	stopHook, ok := cfg.Hooks["Stop"]
	if !ok {
		t.Fatal("Stop hooks not configured")
	}
	if len(stopHook) == 0 || len(stopHook[0].Hooks) == 0 {
		t.Fatal("Stop should have at least one hook")
	}
	stopCmd := stopHook[0].Hooks[0].Command
	if !strings.Contains(stopCmd, ".forge-result.json") {
		t.Errorf("Stop hook should check for result file, got: %s", stopCmd)
	}
	if !strings.Contains(stopCmd, "test -f") {
		t.Errorf("Stop hook should use test -f, got: %s", stopCmd)
	}
}

func TestForgeMergeRoleConfig(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())

	cfg := ForgeMergeRoleConfig()

	if cfg.Role != "forge-merge" {
		t.Errorf("Role = %q, want forge-merge", cfg.Role)
	}
	if cfg.ReplacePrompt != true {
		t.Error("ReplacePrompt should be true")
	}
	if cfg.SystemPromptContent == "" {
		t.Error("SystemPromptContent should not be empty")
	}
	if cfg.SkillInstaller != nil {
		t.Error("SkillInstaller should be nil — forge has no skills")
	}
	if cfg.NeedsItem != false {
		t.Error("NeedsItem should be false")
	}
	if cfg.WorktreeDir == nil {
		t.Error("WorktreeDir should be set")
	}
	if cfg.Persona == nil {
		t.Error("Persona should be set")
	}
	if cfg.Hooks == nil {
		t.Error("Hooks should be set")
	}
	if cfg.PrimeBuilder == nil {
		t.Error("PrimeBuilder should be set")
	}

	// Verify WorktreeDir ignores agent parameter.
	dir := cfg.WorktreeDir("myworld", "anything")
	if !strings.Contains(dir, "myworld") {
		t.Errorf("WorktreeDir should contain world name, got: %s", dir)
	}
}
