package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const sessionStartScript = `#!/bin/bash
# SessionStart hook — inject execution context via sol prime
exec sol prime --world="$SOL_WORLD" --agent="$SOL_AGENT"
`

// HookConfig represents the Claude Code settings.local.json structure for hooks.
type HookConfig struct {
	Hooks map[string][]HookEntry `json:"hooks"`
}

// HookEntry represents a single hook entry in the Claude Code settings.
type HookEntry struct {
	Type    string `json:"type"`
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
}

// InstallHooks writes Claude Code hook scripts into the worktree.
// Creates .claude/hooks/ directory and registers hooks in .claude/settings.local.json.
//
// Hooks installed:
//
//	SessionStart: runs "sol prime --world={world} --agent={name}" and outputs
//	              the result as initial context
func InstallHooks(worktreeDir, world, agentName string) error {
	hooksDir := filepath.Join(worktreeDir, ".claude", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude/hooks directory: %w", err)
	}

	// Write the session-start hook script.
	scriptPath := filepath.Join(hooksDir, "session-start.sh")
	if err := os.WriteFile(scriptPath, []byte(sessionStartScript), 0o755); err != nil {
		return fmt.Errorf("failed to write session-start.sh: %w", err)
	}

	// Write settings.local.json with hook configuration.
	cfg := HookConfig{
		Hooks: map[string][]HookEntry{
			"SessionStart": {
				{
					Type:    "command",
					Command: ".claude/hooks/session-start.sh",
				},
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal hook settings: %w", err)
	}

	settingsPath := filepath.Join(worktreeDir, ".claude", "settings.local.json")
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write settings.local.json: %w", err)
	}

	return nil
}
