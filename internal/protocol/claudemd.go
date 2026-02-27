package protocol

import (
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeMDContext holds the fields used to generate a CLAUDE.md file for an outpost agent.
type ClaudeMDContext struct {
	AgentName   string
	World       string
	WorkItemID  string
	Title       string
	Description string
	HasWorkflow bool // if true, include workflow commands
}

// GenerateClaudeMD returns the contents of a CLAUDE.md file for an outpost agent.
// This file is the agent's entire understanding of the system.
func GenerateClaudeMD(ctx ClaudeMDContext) string {
	workflowSection := ""
	protocolSection := ""

	if ctx.HasWorkflow {
		workflowSection = fmt.Sprintf(`
## Workflow Commands
- `+"`sol workflow current --world=%s --agent=%s`"+` — Read current step instructions
- `+"`sol workflow advance --world=%s --agent=%s`"+` — Mark step complete, advance to next
- `+"`sol workflow status --world=%s --agent=%s`"+` — Check progress
`, ctx.World, ctx.AgentName, ctx.World, ctx.AgentName, ctx.World, ctx.AgentName)

		protocolSection = fmt.Sprintf(`## Protocol
1. Read your current step: `+"`sol workflow current --world=%s --agent=%s`"+`
2. Execute the step instructions.
3. When the step is complete: `+"`sol workflow advance --world=%s --agent=%s`"+`
4. Repeat from step 1 until all steps are done.
5. When the workflow is complete, run `+"`sol resolve`"+`.
`, ctx.World, ctx.AgentName, ctx.World, ctx.AgentName)
	} else {
		protocolSection = `## Protocol
1. Read your assignment above carefully.
2. Execute the work in this worktree.
3. When finished, run ` + "`sol resolve`" + `.
4. If you cannot complete the work, run ` + "`sol escalate \"description of problem\"`" + `.
`
	}

	return fmt.Sprintf(`# Outpost Agent: %s (world: %s)

You are an outpost agent in a multi-agent orchestration system.
Your job is to execute the assigned work item.

## Your Assignment
- Work item: %s
- Title: %s
- Description: %s

## Commands
- `+"`sol resolve`"+` — Signal that your work is complete. This pushes your branch,
  clears your tether, and ends your session. Only run this when you are
  confident the work is done.
- `+"`sol escalate`"+` — Request help if you are stuck. Describe the problem.
%s
%s
## Session Management
- `+"`sol handoff`"+` — Hand off to a fresh session (preserves context)
- `+"`sol handoff --summary=\"what I've done so far\"`"+` — Hand off with a summary

## Important
- You are working in an isolated git worktree. Commit your changes normally.
- Do not modify files outside this worktree.
- Do not attempt to interact with other agents directly.
`, ctx.AgentName, ctx.World, ctx.WorkItemID, ctx.Title, ctx.Description,
		workflowSection, protocolSection)
}

// ForgeClaudeMDContext holds the fields used to generate a CLAUDE.md for the forge.
type ForgeClaudeMDContext struct {
	World        string
	TargetBranch string
	WorktreeDir  string
	QualityGates []string
}

// GenerateForgeClaudeMD returns the contents of a CLAUDE.md for the forge agent.
func GenerateForgeClaudeMD(ctx ForgeClaudeMDContext) string {
	gates := ""
	for _, g := range ctx.QualityGates {
		gates += fmt.Sprintf("- `%s`\n", g)
	}

	return fmt.Sprintf(`# Forge Agent (world: %s)

You are the Forge for world %s. You are a merge processor, NOT a developer.

## FORBIDDEN
- Writing application code
- Reading outpost implementations
- Modifying source files except to resolve merge conflicts

## Your Job
Rebase, test, merge, push. Handle conflicts. Attribute test failures.

## Patrol Loop

Run this loop continuously:

1. `+"`sol forge check-unblocked %s`"+` — unblock resolved MRs
2. `+"`sol forge ready %s --json`"+` — scan queue
   - If empty, wait 30 seconds, go to step 1
3. `+"`sol forge claim %s --json`"+` — claim next MR
4. `+"`git fetch origin`"+` then `+"`git rebase origin/%s`"+` on the MR branch
   - This is the judgment step. If conflicts occur, go to step 5.
   - If clean, go to step 6.
5. Conflict resolution:
   - Inspect `+"`git status`"+` and `+"`git diff`"+` to assess conflict complexity
   - **Trivial** (imports, whitespace, lockfiles, go.sum): resolve directly,
     `+"`git add <files>`"+`, `+"`git rebase --continue`"+`
   - **Complex** (logic, overlapping edits, any uncertainty):
     `+"`git rebase --abort`"+`, `+"`sol forge create-resolution %s <mr-id>`"+`,
     skip to step 8
6. `+"`sol forge run-gates %s`"+` — run quality gates
   - If fail: attribute the failure.
     - Branch caused it? `+"`sol forge mark-failed %s <mr-id>`"+`
     - Pre-existing? Note and proceed.
   - If pass: continue to step 7.
7. `+"`sol forge push %s`"+`
   - If rejected: `+"`sol forge release %s <mr-id>`"+`, go to step 2
8. `+"`sol forge mark-merged %s <mr-id>`"+`
9. More MRs? Go to step 2. Otherwise wait 30 seconds, go to step 1.

## Conflict Judgment Framework

| Conflicted files | Nature | Action |
|---|---|---|
| go.sum, package-lock.json | Auto-generated | Resolve: regenerate |
| Import blocks only | Trivial | Resolve: merge imports |
| Same function, different edits | Complex | Delegate: create-resolution |
| Any uncertainty | Complex | Delegate: always safe to delegate |

## Sequential Rebase Rule
After every merge, the target branch moves. The next branch MUST rebase on
that new baseline. Always `+"`git fetch origin`"+` before rebasing.

## Target Branch
%s

## Quality Gates
%s
## Commands Reference
- `+"`sol forge ready %s --json`"+` — list ready MRs
- `+"`sol forge blocked %s --json`"+` — list blocked MRs
- `+"`sol forge claim %s --json`"+` — claim next MR
- `+"`sol forge release %s <mr-id>`"+` — release claimed MR
- `+"`sol forge run-gates %s`"+` — run quality gates (exit 0=pass, 1=fail)
- `+"`sol forge push %s`"+` — push to target branch
- `+"`sol forge mark-merged %s <mr-id>`"+` — mark MR as merged
- `+"`sol forge mark-failed %s <mr-id>`"+` — mark MR as failed
- `+"`sol forge create-resolution %s <mr-id>`"+` — create conflict resolution task
- `+"`sol forge check-unblocked %s`"+` — check and unblock resolved MRs
`,
		ctx.World, ctx.World,
		ctx.World, ctx.World, ctx.World, ctx.TargetBranch,
		ctx.World, ctx.World, ctx.World,
		ctx.World, ctx.World, ctx.World,
		ctx.TargetBranch, gates,
		ctx.World, ctx.World, ctx.World, ctx.World, ctx.World, ctx.World,
		ctx.World, ctx.World, ctx.World, ctx.World,
	)
}

// InstallForgeClaudeMD writes .claude/CLAUDE.md for the forge into the worktree.
func InstallForgeClaudeMD(worktreeDir string, ctx ForgeClaudeMDContext) error {
	claudeDir := filepath.Join(worktreeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory in worktree: %w", err)
	}

	content := GenerateForgeClaudeMD(ctx)
	path := filepath.Join(claudeDir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write forge CLAUDE.md in worktree: %w", err)
	}
	return nil
}

// InstallClaudeMD writes .claude/CLAUDE.md into the given worktree directory.
// Creates .claude/ if it doesn't exist.
func InstallClaudeMD(worktreeDir string, ctx ClaudeMDContext) error {
	claudeDir := filepath.Join(worktreeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory in worktree: %w", err)
	}

	content := GenerateClaudeMD(ctx)
	path := filepath.Join(claudeDir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write CLAUDE.md in worktree: %w", err)
	}
	return nil
}
