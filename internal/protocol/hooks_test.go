package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardHooksOutpostNoForgeBlocks(t *testing.T) {
	groups := GuardHooks("outpost")
	// Outpost: 3 common + 4 dangerous-command + 2 workflow-bypass = 9
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
	// Forge: 3 dangerous-command only (force push, checkout -b, rm -rf)
	if len(groups) != 3 {
		t.Fatalf("expected 3 guard hook groups for forge, got %d", len(groups))
	}
	dangerousCount := 0
	for _, g := range groups {
		if len(g.Hooks) > 0 && strings.Contains(g.Hooks[0].Command, "sol guard dangerous-command") {
			dangerousCount++
		}
	}
	if dangerousCount != 3 {
		t.Errorf("expected 3 dangerous-command guards, got %d", dangerousCount)
	}
}

func TestInstallHooksPreCompact(t *testing.T) {
	dir := t.TempDir()

	cfg := BaseHooks(HookOptions{
		Role:             "outpost",
		SessionStartCmds: []string{"sol prime --world=ember --agent=Toast"},
		PreCompactCmd:    "sol prime --world=ember --agent=Toast --compact",
		NudgeDrainCmd:    "sol nudge drain --world=ember --agent=Toast",
	})
	if err := WriteHookSettings(dir, cfg); err != nil {
		t.Fatalf("WriteHookSettings failed: %v", err)
	}

	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings.local.json: %v", err)
	}

	var parsed HookConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse settings.local.json: %v", err)
	}

	// Verify SessionStart hook.
	groups, ok := parsed.Hooks["SessionStart"]
	if !ok {
		t.Fatal("settings.local.json missing SessionStart hook")
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 SessionStart matcher group, got %d", len(groups))
	}
	if groups[0].Hooks[0].Command != "sol prime --world=ember --agent=Toast" {
		t.Errorf("unexpected SessionStart command: %q", groups[0].Hooks[0].Command)
	}

	// Verify PreCompact hook uses sol prime --compact (not handoff).
	pcGroups, ok := parsed.Hooks["PreCompact"]
	if !ok {
		t.Fatal("settings.local.json missing PreCompact hook")
	}
	if len(pcGroups) != 1 {
		t.Fatalf("expected 1 PreCompact matcher group, got %d", len(pcGroups))
	}
	pcCmd := pcGroups[0].Hooks[0].Command
	wantPC := "sol prime --world=ember --agent=Toast --compact"
	if pcCmd != wantPC {
		t.Errorf("expected PreCompact command %q, got %q", wantPC, pcCmd)
	}

	// Verify PreToolUse hooks: 1 EnterPlanMode + 9 guard hooks = 10
	ptuGroups, ok := parsed.Hooks["PreToolUse"]
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
