package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallForgeHooks(t *testing.T) {
	dir := t.TempDir()

	if err := InstallForgeHooks(dir, "myworld"); err != nil {
		t.Fatalf("InstallForgeHooks failed: %v", err)
	}

	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings.local.json: %v", err)
	}

	var cfg HookConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
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

	hookCmd := groups[0].Hooks[0].Command

	// Must contain forge sync before prime.
	if !strings.Contains(hookCmd, "sol forge sync --world=myworld") {
		t.Errorf("hook command missing forge sync: %q", hookCmd)
	}
	if !strings.Contains(hookCmd, "sol prime --world=myworld --agent=forge") {
		t.Errorf("hook command missing prime: %q", hookCmd)
	}

	// Sync must come before prime (connected by &&).
	syncIdx := strings.Index(hookCmd, "sol forge sync")
	primeIdx := strings.Index(hookCmd, "sol prime")
	if syncIdx >= primeIdx {
		t.Errorf("forge sync should come before prime in hook command: %q", hookCmd)
	}
	if !strings.Contains(hookCmd, "&&") {
		t.Errorf("expected && between sync and prime: %q", hookCmd)
	}

	// Must have PreCompact hook with handoff command.
	pcGroups, ok := cfg.Hooks["PreCompact"]
	if !ok {
		t.Fatal("settings.local.json missing PreCompact hook")
	}
	if len(pcGroups) != 1 {
		t.Fatalf("expected 1 PreCompact matcher group, got %d", len(pcGroups))
	}
	pcCmd := pcGroups[0].Hooks[0].Command
	if pcCmd != "sol handoff --world=myworld --agent=forge" {
		t.Errorf("expected PreCompact command 'sol handoff --world=myworld --agent=forge', got %q", pcCmd)
	}

	// Must have PreToolUse hook blocking EnterPlanMode.
	ptuGroups, ok := cfg.Hooks["PreToolUse"]
	if !ok {
		t.Fatal("settings.local.json missing PreToolUse hook")
	}
	// Forge: 1 EnterPlanMode + 6 dangerous-command + 7 forge-specific = 14
	if len(ptuGroups) != 14 {
		t.Fatalf("expected 14 PreToolUse matcher groups (1 EnterPlanMode + 6 dangerous + 7 forge-specific), got %d", len(ptuGroups))
	}
	if ptuGroups[0].Matcher != "EnterPlanMode" {
		t.Errorf("PreToolUse matcher = %q, want \"EnterPlanMode\"", ptuGroups[0].Matcher)
	}
	if !strings.Contains(ptuGroups[0].Hooks[0].Command, "BLOCKED") {
		t.Error("EnterPlanMode hook should contain BLOCKED message")
	}
	if !strings.Contains(ptuGroups[0].Hooks[0].Command, "exit 2") {
		t.Error("EnterPlanMode hook should exit 2 to block the tool call")
	}
	// Groups 1-6: dangerous-command guards.
	for _, g := range ptuGroups[1:7] {
		if len(g.Hooks) > 0 {
			if !strings.Contains(g.Hooks[0].Command, "sol guard dangerous-command") {
				t.Errorf("forge guard hook should be dangerous-command, got %q", g.Hooks[0].Command)
			}
		}
	}
	// Groups 7-13: forge-specific manual-command blocks.
	forgeBlockedMatchers := map[string]bool{
		"Bash(git fetch*)":      false,
		"Bash(git pull*)":       false,
		"Bash(git merge*)":      false,
		"Bash(git rebase*)":     false,
		"Bash(git checkout*)":   false,
		"Bash(git push origin*)": false,
		"Bash(go test*)":        false,
	}
	for _, g := range ptuGroups[7:] {
		if _, ok := forgeBlockedMatchers[g.Matcher]; !ok {
			t.Errorf("unexpected forge-specific matcher: %q", g.Matcher)
		}
		forgeBlockedMatchers[g.Matcher] = true
		if len(g.Hooks) == 0 {
			t.Errorf("forge-specific matcher %q has no hooks", g.Matcher)
			continue
		}
		if !strings.Contains(g.Hooks[0].Command, "BLOCKED") {
			t.Errorf("forge-specific hook %q should contain BLOCKED message", g.Matcher)
		}
		if !strings.Contains(g.Hooks[0].Command, "exit 2") {
			t.Errorf("forge-specific hook %q should exit 2", g.Matcher)
		}
	}
	for matcher, found := range forgeBlockedMatchers {
		if !found {
			t.Errorf("missing forge-specific matcher: %s", matcher)
		}
	}
	// Forge should NOT have workflow-bypass guards.
	for _, g := range ptuGroups {
		if len(g.Hooks) > 0 && strings.Contains(g.Hooks[0].Command, "sol guard workflow-bypass") {
			t.Error("forge should not have workflow-bypass guards")
		}
	}
}

func TestGuardHooksOutpostNoForgeBlocks(t *testing.T) {
	groups := GuardHooks("outpost")
	// Outpost: 6 dangerous-command + 3 workflow-bypass = 9
	if len(groups) != 9 {
		t.Fatalf("expected 9 guard hook groups for outpost, got %d", len(groups))
	}
	for _, g := range groups {
		if len(g.Hooks) > 0 && strings.Contains(g.Hooks[0].Command, "BLOCKED") {
			t.Errorf("outpost should not have forge-specific BLOCKED hooks, got matcher %q", g.Matcher)
		}
	}
}

func TestGuardHooksForgeHasDangerousCommands(t *testing.T) {
	groups := GuardHooks("forge")
	// Forge: 6 dangerous-command + 7 forge-specific = 13
	if len(groups) != 13 {
		t.Fatalf("expected 13 guard hook groups for forge, got %d", len(groups))
	}
	// First 6 should be dangerous-command guards.
	dangerousCount := 0
	for _, g := range groups[:6] {
		if len(g.Hooks) > 0 && strings.Contains(g.Hooks[0].Command, "sol guard dangerous-command") {
			dangerousCount++
		}
	}
	if dangerousCount != 6 {
		t.Errorf("expected 6 dangerous-command guards, got %d", dangerousCount)
	}
}

func TestInstallHooksPreCompact(t *testing.T) {
	dir := t.TempDir()

	if err := InstallHooks(dir, "ember", "Toast"); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings.local.json: %v", err)
	}

	var cfg HookConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse settings.local.json: %v", err)
	}

	// Verify SessionStart hook.
	groups, ok := cfg.Hooks["SessionStart"]
	if !ok {
		t.Fatal("settings.local.json missing SessionStart hook")
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 SessionStart matcher group, got %d", len(groups))
	}
	if groups[0].Hooks[0].Command != "sol prime --world=ember --agent=Toast" {
		t.Errorf("unexpected SessionStart command: %q", groups[0].Hooks[0].Command)
	}

	// Verify PreCompact hook.
	pcGroups, ok := cfg.Hooks["PreCompact"]
	if !ok {
		t.Fatal("settings.local.json missing PreCompact hook")
	}
	if len(pcGroups) != 1 {
		t.Fatalf("expected 1 PreCompact matcher group, got %d", len(pcGroups))
	}
	pcCmd := pcGroups[0].Hooks[0].Command
	if pcCmd != "sol handoff --world=ember --agent=Toast" {
		t.Errorf("expected PreCompact command 'sol handoff --world=ember --agent=Toast', got %q", pcCmd)
	}

	// Verify PreToolUse hooks: 1 EnterPlanMode + 9 guard hooks = 10
	ptuGroups, ok := cfg.Hooks["PreToolUse"]
	if !ok {
		t.Fatal("settings.local.json missing PreToolUse hook")
	}
	if len(ptuGroups) != 10 {
		t.Fatalf("expected 10 PreToolUse matcher groups, got %d", len(ptuGroups))
	}
	if ptuGroups[0].Matcher != "EnterPlanMode" {
		t.Errorf("PreToolUse matcher = %q, want \"EnterPlanMode\"", ptuGroups[0].Matcher)
	}
	if !strings.Contains(ptuGroups[0].Hooks[0].Command, "BLOCKED") {
		t.Error("EnterPlanMode hook should contain BLOCKED message")
	}
	if !strings.Contains(ptuGroups[0].Hooks[0].Command, "exit 2") {
		t.Error("EnterPlanMode hook should exit 2 to block the tool call")
	}
}
