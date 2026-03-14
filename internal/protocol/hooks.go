package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/adapter"
)

// PlanModeBlockCommand is the standard PreToolUse command to block EnterPlanMode
// for roles that use brief memory (envoy, governor, chancellor).
const PlanModeBlockCommand = `echo "BLOCKED: Plan mode overrides your persona and context. Outline your approach in conversation instead. Your persistent memory is at .brief/memory.md — consult it for your role constraints and accumulated knowledge." >&2; exit 2`

// ForgePlanModeBlockCommand is the forge-specific EnterPlanMode blocker.
const ForgePlanModeBlockCommand = `echo "BLOCKED: Plan mode is not permitted in forge merge sessions." >&2; exit 2`

// AutoMemoryBlockCommand is the standard PreToolUse command to block Claude Code
// auto-memory writes (roles that use .brief/memory.md instead).
const AutoMemoryBlockCommand = `FILE=$(jq -r '.tool_input.file_path // empty'); if echo "$FILE" | grep -q '.claude/projects/.*/memory/'; then echo "BLOCKED: Use .brief/memory.md, not Claude Code auto-memory." >&2; exit 2; fi`

// RoleGuards returns the standard adapter.Guard entries for the given role.
// These represent PreToolUse blockers that the adapter translates to
// runtime-specific hook format.
//
// Roles:
//   - "forge": dangerous-command guards only (no workflow-bypass guards,
//     no git reset/restore guards — forge needs these operations)
//   - all others: full set of dangerous-command and workflow-bypass guards
func RoleGuards(role string) []adapter.Guard {
	guards := []adapter.Guard{
		{Pattern: "Bash(git push --force*)|Bash(git push -f *)", Command: "sol guard dangerous-command"},
		{Pattern: "Bash(git checkout -b*)|Bash(git switch -c*)", Command: "sol guard dangerous-command"},
		{Pattern: "Bash(rm -rf /*)", Command: "sol guard dangerous-command"},
	}
	if role != "forge" {
		guards = append(guards,
			adapter.Guard{Pattern: "Bash(git reset --hard*)", Command: "sol guard dangerous-command"},
			adapter.Guard{Pattern: "Bash(git clean -f*)", Command: "sol guard dangerous-command"},
			adapter.Guard{Pattern: "Bash(git checkout -- *)", Command: "sol guard dangerous-command"},
			adapter.Guard{Pattern: "Bash(git restore .*)", Command: "sol guard dangerous-command"},
			adapter.Guard{Pattern: "Bash(git push origin main*)|Bash(git push origin master*)", Command: "sol guard workflow-bypass"},
			adapter.Guard{Pattern: "Bash(gh pr create*)", Command: "sol guard workflow-bypass"},
		)
	}
	return guards
}

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

// GuardHooks returns PreToolUse matcher groups for sol guard commands.
// These block dangerous commands (force push, rm -rf, etc.) and
// workflow-bypass commands (push to main, gh pr create, manual branching).
// The role parameter controls which guards apply:
//   - "forge": force push, feature branching, rm -rf (forge uses git reset --hard in sync step)
//   - "outpost": all dangerous-command guards + workflow-bypass guards
func GuardHooks(role string) []HookMatcherGroup {
	// Common dangerous-command guards for all roles.
	groups := []HookMatcherGroup{
		{
			Matcher: "Bash(git push --force*)|Bash(git push -f *)",
			Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
		},
		{
			Matcher: "Bash(git checkout -b*)|Bash(git switch -c*)",
			Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
		},
		{
			Matcher: "Bash(rm -rf /*)",
			Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
		},
	}

	// Outpost agents get additional guards. Forge is exempt because it uses
	// git reset --hard (sync step), pushes to main by design, etc.
	if role != "forge" {
		groups = append(groups,
			HookMatcherGroup{
				Matcher: "Bash(git reset --hard*)",
				Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git clean -f*)",
				Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git checkout -- *)",
				Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git restore .*)",
				Hooks:   []HookHandler{{Type: "command", Command: "sol guard dangerous-command"}},
			},
			HookMatcherGroup{
				Matcher: "Bash(git push origin main*)|Bash(git push origin master*)",
				Hooks:   []HookHandler{{Type: "command", Command: "sol guard workflow-bypass"}},
			},
			HookMatcherGroup{
				Matcher: "Bash(gh pr create*)",
				Hooks:   []HookHandler{{Type: "command", Command: "sol guard workflow-bypass"}},
			},
		)
	}

	return groups
}

// HookOptions controls what BaseHooks generates.
// BriefPath drives both brief injection in SessionStart and the Write|Edit
// auto-memory blocker in PreToolUse — set it for roles that maintain a brief.
// Leave it empty for roles that use sol prime instead (e.g. outpost).
type HookOptions struct {
	Role             string             // role name passed to GuardHooks
	BriefPath        string             // if set: adds brief inject to SessionStart (with "startup|resume" matcher) and Write|Edit auto-memory blocker to PreToolUse
	SessionStartCmds []string           // additional SessionStart commands appended (joined with " && ") after brief inject
	PreCompactCmd    string             // if set, adds a PreCompact hook with this command
	NudgeDrainCmd    string             // if set, adds a UserPromptSubmit hook with this command
	ExtraPreToolUse  []HookMatcherGroup // role-specific PreToolUse matchers prepended before the standard blockers
}

// BaseHooks builds a HookConfig from common role options.
//
// Always included in PreToolUse:
//   - EnterPlanMode blocker (exit 2)
//   - GuardHooks(role)
//
// Conditionally included when options are set:
//   - Write|Edit auto-memory blocker    — when BriefPath is non-empty
//   - Brief inject in SessionStart       — when BriefPath is non-empty (matcher: "startup|resume")
//   - Additional SessionStart commands  — when SessionStartCmds is non-empty
//   - PreCompact hook                    — when PreCompactCmd is non-empty
//   - UserPromptSubmit nudge drain       — when NudgeDrainCmd is non-empty
func BaseHooks(opts HookOptions) HookConfig {
	hooks := map[string][]HookMatcherGroup{}

	// --- SessionStart ---
	var sessionStartCmd string
	var sessionStartMatcher string
	if opts.BriefPath != "" {
		sessionStartCmd = fmt.Sprintf("sol brief inject --path=%s --max-lines=200", opts.BriefPath)
		sessionStartMatcher = "startup|resume"
	}
	for _, cmd := range opts.SessionStartCmds {
		if sessionStartCmd != "" {
			sessionStartCmd += " && " + cmd
		} else {
			sessionStartCmd = cmd
		}
	}
	if sessionStartCmd != "" {
		hooks["SessionStart"] = []HookMatcherGroup{{
			Matcher: sessionStartMatcher,
			Hooks:   []HookHandler{{Type: "command", Command: sessionStartCmd}},
		}}
	}

	// --- PreToolUse ---
	var preToolUse []HookMatcherGroup
	preToolUse = append(preToolUse, opts.ExtraPreToolUse...)
	if opts.BriefPath != "" {
		preToolUse = append(preToolUse, HookMatcherGroup{
			Matcher: "Write|Edit",
			Hooks: []HookHandler{{
				Type:    "command",
				Command: `FILE=$(jq -r '.tool_input.file_path // empty'); if echo "$FILE" | grep -q '.claude/projects/.*/memory/'; then echo "BLOCKED: Use .brief/memory.md, not Claude Code auto-memory." >&2; exit 2; fi`,
			}},
		})
	}
	preToolUse = append(preToolUse, HookMatcherGroup{
		Matcher: "EnterPlanMode",
		Hooks: []HookHandler{{
			Type:    "command",
			Command: `echo "BLOCKED: Plan mode overrides your persona and context. Outline your approach in conversation instead. Your persistent memory is at .brief/memory.md — consult it for your role constraints and accumulated knowledge." >&2; exit 2`,
		}},
	})
	preToolUse = append(preToolUse, GuardHooks(opts.Role)...)
	hooks["PreToolUse"] = preToolUse

	// --- PreCompact (optional) ---
	if opts.PreCompactCmd != "" {
		hooks["PreCompact"] = []HookMatcherGroup{{
			Hooks: []HookHandler{{Type: "command", Command: opts.PreCompactCmd}},
		}}
	}

	// --- UserPromptSubmit nudge drain (optional) ---
	if opts.NudgeDrainCmd != "" {
		hooks["UserPromptSubmit"] = []HookMatcherGroup{{
			Hooks: []HookHandler{{Type: "command", Command: opts.NudgeDrainCmd}},
		}}
	}

	return HookConfig{Hooks: hooks}
}

// WriteHookSettings writes a HookConfig to .claude/settings.local.json.
func WriteHookSettings(worktreeDir string, cfg HookConfig) error {
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
