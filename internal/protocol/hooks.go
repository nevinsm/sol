package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const sessionStartScript = `#!/bin/bash
# SessionStart hook — inject execution context via gt prime
exec gt prime --rig="$GT_RIG" --agent="$GT_AGENT"
`

// hookConfig represents the Claude Code settings.local.json structure for hooks.
type hookConfig struct {
	Hooks map[string][]hookEntry `json:"hooks"`
}

type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// InstallHooks writes Claude Code hook scripts into the worktree.
// Creates .claude/hooks/ directory and registers hooks in .claude/settings.local.json.
//
// Hooks installed:
//
//	SessionStart: runs "gt prime --rig={rig} --agent={name}" and outputs
//	              the result as initial context
func InstallHooks(worktreeDir, rig, agentName string) error {
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
	cfg := hookConfig{
		Hooks: map[string][]hookEntry{
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
