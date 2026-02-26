package protocol

import (
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeMDContext holds the fields used to generate a CLAUDE.md file for a polecat agent.
type ClaudeMDContext struct {
	AgentName   string
	Rig         string
	WorkItemID  string
	Title       string
	Description string
}

// GenerateClaudeMD returns the contents of a CLAUDE.md file for a polecat agent.
// This file is the agent's entire understanding of the system.
func GenerateClaudeMD(ctx ClaudeMDContext) string {
	return fmt.Sprintf(`# Polecat Agent: %s (rig: %s)

You are a polecat agent in a multi-agent orchestration system.
Your job is to execute the assigned work item.

## Your Assignment
- Work item: %s
- Title: %s
- Description: %s

## Commands
- `+"`gt done`"+` — Signal that your work is complete. This pushes your branch,
  clears your hook, and ends your session. Only run this when you are
  confident the work is done.
- `+"`gt escalate`"+` — Request help if you are stuck. Describe the problem.

## Protocol
1. Read your assignment above carefully.
2. Execute the work in this worktree.
3. When finished, run `+"`gt done`"+`.
4. If you cannot complete the work, run `+"`gt escalate \"description of problem\"`"+`.

## Important
- You are working in an isolated git worktree. Commit your changes normally.
- Do not modify files outside this worktree.
- Do not attempt to interact with other agents directly.
`, ctx.AgentName, ctx.Rig, ctx.WorkItemID, ctx.Title, ctx.Description)
}

// RefineryClaudeMDContext holds the fields used to generate a CLAUDE.md for the refinery.
type RefineryClaudeMDContext struct {
	Rig          string
	TargetBranch string
	WorktreeDir  string
	QualityGates []string
}

// GenerateRefineryClaudeMD returns the contents of a CLAUDE.md for the refinery agent.
func GenerateRefineryClaudeMD(ctx RefineryClaudeMDContext) string {
	gates := ""
	for _, g := range ctx.QualityGates {
		gates += fmt.Sprintf("- `%s`\n", g)
	}

	return fmt.Sprintf(`# Refinery Agent (rig: %s)

You are the Refinery for rig %s. You are a merge processor, NOT a developer.

## FORBIDDEN
- Writing application code
- Reading polecat implementations
- Modifying source files except to resolve merge conflicts

## Your Job
Rebase, test, merge, push. Handle conflicts. Attribute test failures.

## Patrol Loop

Run this loop continuously:

1. `+"`gt refinery check-unblocked %s`"+` — unblock resolved MRs
2. `+"`gt refinery ready %s --json`"+` — scan queue
   - If empty, wait 30 seconds, go to step 1
3. `+"`gt refinery claim %s --json`"+` — claim next MR
4. `+"`git fetch origin`"+` then `+"`git rebase origin/%s`"+` on the MR branch
   - This is the judgment step. If conflicts occur, go to step 5.
   - If clean, go to step 6.
5. Conflict resolution:
   - Inspect `+"`git status`"+` and `+"`git diff`"+` to assess conflict complexity
   - **Trivial** (imports, whitespace, lockfiles, go.sum): resolve directly,
     `+"`git add <files>`"+`, `+"`git rebase --continue`"+`
   - **Complex** (logic, overlapping edits, any uncertainty):
     `+"`git rebase --abort`"+`, `+"`gt refinery create-resolution %s <mr-id>`"+`,
     skip to step 8
6. `+"`gt refinery run-gates %s`"+` — run quality gates
   - If fail: attribute the failure.
     - Branch caused it? `+"`gt refinery mark-failed %s <mr-id>`"+`
     - Pre-existing? Note and proceed.
   - If pass: continue to step 7.
7. `+"`gt refinery push %s`"+`
   - If rejected: `+"`gt refinery release %s <mr-id>`"+`, go to step 2
8. `+"`gt refinery mark-merged %s <mr-id>`"+`
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
- `+"`gt refinery ready %s --json`"+` — list ready MRs
- `+"`gt refinery blocked %s --json`"+` — list blocked MRs
- `+"`gt refinery claim %s --json`"+` — claim next MR
- `+"`gt refinery release %s <mr-id>`"+` — release claimed MR
- `+"`gt refinery run-gates %s`"+` — run quality gates (exit 0=pass, 1=fail)
- `+"`gt refinery push %s`"+` — push to target branch
- `+"`gt refinery mark-merged %s <mr-id>`"+` — mark MR as merged
- `+"`gt refinery mark-failed %s <mr-id>`"+` — mark MR as failed
- `+"`gt refinery create-resolution %s <mr-id>`"+` — create conflict resolution task
- `+"`gt refinery check-unblocked %s`"+` — check and unblock resolved MRs
`,
		ctx.Rig, ctx.Rig,
		ctx.Rig, ctx.Rig, ctx.Rig, ctx.TargetBranch,
		ctx.Rig, ctx.Rig, ctx.Rig,
		ctx.Rig, ctx.Rig, ctx.Rig,
		ctx.TargetBranch, gates,
		ctx.Rig, ctx.Rig, ctx.Rig, ctx.Rig, ctx.Rig, ctx.Rig,
		ctx.Rig, ctx.Rig, ctx.Rig, ctx.Rig,
	)
}

// InstallRefineryClaudeMD writes .claude/CLAUDE.md for the refinery into the worktree.
func InstallRefineryClaudeMD(worktreeDir string, ctx RefineryClaudeMDContext) error {
	claudeDir := filepath.Join(worktreeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory in worktree: %w", err)
	}

	content := GenerateRefineryClaudeMD(ctx)
	path := filepath.Join(claudeDir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write refinery CLAUDE.md in worktree: %w", err)
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
