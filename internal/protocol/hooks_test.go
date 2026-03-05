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
	// Forge: 1 EnterPlanMode + 6 dangerous-command (exempt from workflow-bypass) = 7
	if len(ptuGroups) != 7 {
		t.Fatalf("expected 7 PreToolUse matcher groups (1 EnterPlanMode + 6 dangerous guards), got %d", len(ptuGroups))
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
	// Forge should have dangerous-command guards but NOT workflow-bypass guards.
	for _, g := range ptuGroups[1:] {
		if len(g.Hooks) > 0 {
			if !strings.Contains(g.Hooks[0].Command, "sol guard dangerous-command") {
				t.Errorf("forge guard hook should only be dangerous-command, got %q", g.Hooks[0].Command)
			}
		}
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
