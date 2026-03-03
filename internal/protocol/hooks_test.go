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
	if !strings.Contains(hookCmd, "sol forge sync myworld") {
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
}
