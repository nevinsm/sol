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
	})

	t.Run("idempotent — second call overwrites cleanly", func(t *testing.T) {
		dir := t.TempDir()
		world := "idempotent-world"

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
		"session will be recycled",
	}

	missing := ForgePersonaContains(persona, markers)
	if len(missing) > 0 {
		t.Errorf("persona missing markers: %v", missing)
	}
}

func TestForgeHookConfig(t *testing.T) {
	cfg := forgeHookConfig()

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
}
