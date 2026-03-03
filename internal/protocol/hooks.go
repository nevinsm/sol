package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// HookConfig represents the Claude Code settings.local.json structure for hooks.
type HookConfig struct {
	Hooks map[string][]HookMatcherGroup `json:"hooks"`
}

// HookMatcherGroup is a matcher + its hook handlers. The Matcher field filters
// when the hooks fire (regex matched against event-specific values). Omit or
// use "" to match all occurrences.
type HookMatcherGroup struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []HookHandler `json:"hooks"`
}

// HookHandler is a single hook handler within a matcher group.
type HookHandler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// InstallHooks writes Claude Code hooks into .claude/settings.local.json.
// Values are inlined into the command string so hooks don't depend on
// environment variables (tmux set-environment runs after the session
// process starts, so env vars aren't available to SessionStart hooks).
//
// Hooks installed:
//
//	SessionStart: runs "sol prime --world={world} --agent={name}" and outputs
//	              the result as initial context
func InstallHooks(worktreeDir, world, agentName string) error {
	claudeDir := filepath.Join(worktreeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	cfg := HookConfig{
		Hooks: map[string][]HookMatcherGroup{
			"SessionStart": {
				{
					Hooks: []HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol prime --world=%s --agent=%s", world, agentName),
						},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal hook settings: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write settings.local.json: %w", err)
	}

	return nil
}
