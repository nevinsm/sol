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
//	SessionStart:      runs "sol prime --world={world} --agent={name}" and outputs
//	                   the result as initial context
//	PreCompact:        runs "sol handoff --world={world} --agent={name}" to hand off
//	                   to a fresh session instead of lossy context compaction
//	UserPromptSubmit:  runs "sol nudge drain --world={world} --agent={name}" to drain
//	                   queued nudge messages at turn boundaries
func InstallHooks(worktreeDir, world, agentName string) error {
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
			"PreCompact": {
				{
					Hooks: []HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol handoff --world=%s --agent=%s", world, agentName),
						},
					},
				},
			},
			"PreToolUse": append([]HookMatcherGroup{
				{
					Matcher: "EnterPlanMode",
					Hooks: []HookHandler{
						{
							Type:    "command",
							Command: `echo "BLOCKED: Plan mode requires human approval — no one is watching. Outline your approach in conversation, then implement directly." >&2; exit 2`,
						},
					},
				},
			}, GuardHooks("outpost")...),
			"UserPromptSubmit": {
				{
					Hooks: []HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol nudge drain --world=%s --agent=%s", world, agentName),
						},
					},
				},
			},
		},
	}
	return writeHookSettings(worktreeDir, cfg)
}

// InstallForgeHooks writes forge-specific Claude Code hooks that sync before priming.
// The SessionStart hook runs "sol forge sync {world}" to reset the forge worktree
// to the latest target branch, then "sol prime" to inject execution context.
// The PreCompact hook hands off to a fresh session instead of lossy compaction.
// The UserPromptSubmit hook drains nudge messages at turn boundaries.
func InstallForgeHooks(worktreeDir, world string) error {
	cfg := HookConfig{
		Hooks: map[string][]HookMatcherGroup{
			"SessionStart": {
				{
					Hooks: []HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol forge sync --world=%s && sol prime --world=%s --agent=forge", world, world),
						},
					},
				},
			},
			"PreCompact": {
				{
					Hooks: []HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol handoff --world=%s --agent=forge", world),
						},
					},
				},
			},
			"PreToolUse": append([]HookMatcherGroup{
				{
					Matcher: "EnterPlanMode",
					Hooks: []HookHandler{
						{
							Type:    "command",
							Command: `echo "BLOCKED: Plan mode requires human approval — no one is watching. Outline your approach in conversation, then implement directly." >&2; exit 2`,
						},
					},
				},
			}, GuardHooks("forge")...),
			"UserPromptSubmit": {
				{
					Hooks: []HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol nudge drain --world=%s --agent=forge", world),
						},
					},
				},
			},
		},
	}
	return writeHookSettings(worktreeDir, cfg)
}

// GuardHooks returns PreToolUse matcher groups for sol guard commands.
// These block dangerous commands (force push, hard reset, rm -rf, etc.) and
// workflow-bypass commands (push to main, gh pr create, manual branching).
// The role parameter controls exemptions: "forge" is exempt from workflow-bypass.
func GuardHooks(role string) []HookMatcherGroup {
	groups := []HookMatcherGroup{
		// --- dangerous-command guards ---
		{
			Matcher: "Bash(git push --force*)|Bash(git push -f *)",
			Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
		},
		{
			Matcher: "Bash(git reset --hard*)",
			Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
		},
		{
			Matcher: "Bash(git clean -f*)",
			Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
		},
		{
			Matcher: "Bash(git checkout -- *)",
			Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
		},
		{
			Matcher: "Bash(git restore .*)",
			Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
		},
		{
			Matcher: "Bash(rm -rf /*)",
			Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
		},
	}

	// Forge is exempt from workflow-bypass guards but gets its own
	// manual-command guards that force it through sol forge merge.
	if role == "forge" {
		groups = append(groups,
			HookMatcherGroup{
				Matcher: "Bash(git fetch*)",
				Hooks:   []HookHandler{{Type: "command", Command: `echo "BLOCKED: Use sol forge merge — it handles fetch internally." >&2; exit 2`}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git pull*)",
				Hooks:   []HookHandler{{Type: "command", Command: `echo "BLOCKED: Use sol forge merge — it handles pull internally." >&2; exit 2`}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git merge*)",
				Hooks:   []HookHandler{{Type: "command", Command: `echo "BLOCKED: Use sol forge merge — it handles merge internally." >&2; exit 2`}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git rebase*)",
				Hooks:   []HookHandler{{Type: "command", Command: `echo "BLOCKED: Use sol forge merge — it handles rebase internally." >&2; exit 2`}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git checkout*)",
				Hooks:   []HookHandler{{Type: "command", Command: `echo "BLOCKED: Use sol forge merge — it handles checkout internally." >&2; exit 2`}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git push origin*)",
				Hooks:   []HookHandler{{Type: "command", Command: `echo "BLOCKED: Use sol forge merge — it handles push internally." >&2; exit 2`}},
			},
			HookMatcherGroup{
				Matcher: "Bash(go test*)",
				Hooks:   []HookHandler{{Type: "command", Command: `echo "BLOCKED: Quality gates run inside sol forge merge. Do not run tests directly." >&2; exit 2`}},
			},
		)
	} else {
		groups = append(groups,
			HookMatcherGroup{
				Matcher: "Bash(git push origin main*)|Bash(git push origin master*)",
				Hooks:   []HookHandler{{Type: "command", Command: "sol guard workflow-bypass"}},
			},
			HookMatcherGroup{
				Matcher: "Bash(gh pr create*)",
				Hooks:   []HookHandler{{Type: "command", Command: "sol guard workflow-bypass"}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git checkout -b*)|Bash(git switch -c*)",
				Hooks:   []HookHandler{{Type: "command", Command: "sol guard workflow-bypass"}},
			},
		)
	}

	return groups
}

// writeHookSettings writes a HookConfig to .claude/settings.local.json.
func writeHookSettings(worktreeDir string, cfg HookConfig) error {
	claudeDir := filepath.Join(worktreeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
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
