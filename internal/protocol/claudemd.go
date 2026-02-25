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
