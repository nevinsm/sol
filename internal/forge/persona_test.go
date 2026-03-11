package forge

import (
	"strings"
	"testing"
)

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
		t.Error("persona should not mention /exit — session exit is handled by the Go monitor")
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

	// Verify Stop hook is NOT configured — exit 0 is the default behavior
	// and the Go monitor detects the result file directly via os.Stat.
	if _, ok := cfg.Hooks["Stop"]; ok {
		t.Error("Stop hook should not be configured — it is a no-op (exit 0 is default)")
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

	// Verify persona content via RoleConfig.Persona.
	personaBytes, err := cfg.Persona("test-world", "")
	if err != nil {
		t.Fatalf("Persona() error: %v", err)
	}
	persona := string(personaBytes)
	if !strings.Contains(persona, "Forge Merge Engineer — test-world") {
		t.Error("persona from RoleConfig should contain world name")
	}
	if !strings.Contains(persona, ".forge-result.json") {
		t.Error("persona from RoleConfig should contain result file reference")
	}
}
