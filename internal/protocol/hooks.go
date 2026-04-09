package protocol

import (
	"fmt"

	"github.com/nevinsm/sol/internal/adapter"
)

// PlanModeBlockCommand is the standard PreToolUse command to block EnterPlanMode
// for roles that use Claude Code auto-memory (envoy).
const PlanModeBlockCommand = `echo "BLOCKED: Plan mode overrides your persona and context. Outline your approach in conversation instead. Your persistent memory is in Claude Code's auto-memory (use the /memory command to browse) — consult it for your role constraints and accumulated knowledge." >&2; exit 2`

// OutpostPlanModeBlockCommand generates the PreToolUse command to block EnterPlanMode
// for outpost agents. Outpost agents have no persistent memory — their context
// comes from CLAUDE.local.md and sol prime.
func OutpostPlanModeBlockCommand(world, agent string) string {
	return fmt.Sprintf(`echo "BLOCKED: Plan mode overrides your persona and context. Outline your approach in conversation instead. Your context is in CLAUDE.local.md — run 'sol prime --world=%s --agent=%s' to re-inject it." >&2; exit 2`, world, agent)
}

// ForgePlanModeBlockCommand is the forge-specific EnterPlanMode blocker.
const ForgePlanModeBlockCommand = `echo "BLOCKED: Plan mode is not permitted in forge merge sessions." >&2; exit 2`

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
		{Pattern: "Bash(rm -rf*)", Command: "sol guard dangerous-command"},
		{Pattern: "Bash(rm -fr*)", Command: "sol guard dangerous-command"},
		{Pattern: "Bash(rm -r -f*)", Command: "sol guard dangerous-command"},
		{Pattern: "Bash(rm -f -r*)", Command: "sol guard dangerous-command"},
		{Pattern: "Bash(git worktree remove*)", Command: "sol guard dangerous-command"},
	}
	if role != "forge" {
		guards = append(guards,
			adapter.Guard{Pattern: "Bash(git reset --hard*)", Command: "sol guard dangerous-command"},
			adapter.Guard{Pattern: "Bash(git clean -f*)", Command: "sol guard dangerous-command"},
			adapter.Guard{Pattern: "Bash(git checkout -- *)", Command: "sol guard dangerous-command"},
			adapter.Guard{Pattern: "Bash(git restore *)", Command: "sol guard dangerous-command"},
			adapter.Guard{Pattern: "Bash(git checkout -b*)|Bash(git switch -c*)", Command: "sol guard workflow-bypass"},
			adapter.Guard{Pattern: "Bash(git push origin main*)|Bash(git push origin master*)", Command: "sol guard workflow-bypass"},
			adapter.Guard{Pattern: "Bash(gh pr create*)", Command: "sol guard workflow-bypass"},
		)
	}
	return guards
}

