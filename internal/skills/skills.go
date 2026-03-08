// Package skills provides generators for Claude Code Agent Skills.
//
// Each generator produces a SKILL.md file with YAML frontmatter (name, description)
// and templated content. Skills are installed to .claude/agents/{skill_name}/SKILL.md
// in agent worktrees so Claude Code discovers them as slash commands.
//
// Three skill generators exist:
//   - sol-resolve: Complete work — push branch, create MR, clear tether
//   - sol-workflow: Execute workflow steps — read, execute, advance
//   - sol-forge-ops: Merge pipeline operations — claim, merge, gate, mark
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveContext holds the templated values for the sol-resolve skill.
type ResolveContext struct {
	World        string
	Agent        string
	QualityGates []string // commands to run before resolving
	OutputDir    string   // persistent output directory
}

// WorkflowContext holds the templated values for the sol-workflow skill.
type WorkflowContext struct {
	World string
	Agent string
}

// ForgeOpsContext holds the templated values for the sol-forge-ops skill.
type ForgeOpsContext struct {
	World        string
	TargetBranch string
}

// GenerateResolve returns the SKILL.md content for the sol-resolve skill.
func GenerateResolve(ctx ResolveContext) string {
	// Build quality gates section.
	gatesSection := ""
	if len(ctx.QualityGates) > 0 {
		gatesSection = "\n### Quality Gates\nRun all quality gates before resolving:\n"
		for _, g := range ctx.QualityGates {
			gatesSection += fmt.Sprintf("- `%s`\n", g)
		}
	}

	// Build output dir section.
	outputSection := ""
	if ctx.OutputDir != "" {
		outputSection = fmt.Sprintf("\nPersistent output directory: `%s`\n", ctx.OutputDir)
	}

	// Template uses § as a backtick placeholder.
	tmpl := `---
name: sol-resolve
description: Complete your work — pushes branch, creates merge request, clears tether. Use when work is done and ready to submit.
---

# sol-resolve

Complete your work and submit it for review.

## Resolve Protocol

When your work is done, run:

§§§
sol resolve
§§§

This is the **mandatory final step** for every writ. If you do not resolve, your tether
is orphaned and the writ stays stuck.

## Code Writs

For writs with kind §code§ (the default):

1. **Stage** your changes: §git add <files>§
2. **Commit** with a clear message: §git commit -m "feat: ..."§
3. **Run quality gates** — all must pass before resolving
4. **Resolve**: §sol resolve§
   - Pushes your branch to the remote
   - Creates a merge request for the forge pipeline
   - Clears your tether
   - Ends your outpost session
{QUALITY_GATES}
## Non-Code Writs

For writs with kind §analysis§ or other non-code kinds:

1. **Write output** to the output directory — this is your primary delivery surface
2. **Review** your output for completeness
3. **Resolve**: §sol resolve§
   - Closes the writ directly — no branch push, no MR
   - Clears your tether
   - Ends your outpost session
{OUTPUT_DIR}
## Post-Resolve Behavior

After resolve completes:
- **Outpost agents**: session ends, worktree is cleaned up
- **Envoy agents**: session stays alive — reset your worktree (§git checkout main && git pull§) and update your brief

## Warning: Orphaned Tethers

If you do not run §sol resolve§:
- Your tether remains active indefinitely
- The forge never sees your merge request
- Your worktree leaks until sentinel reaps it
- The writ stays stuck in §tethered§ state

**Always resolve.** If you are stuck, run §sol escalate "description"§ instead.
`

	tmpl = strings.ReplaceAll(tmpl, "§", "`")
	tmpl = strings.ReplaceAll(tmpl, "{WORLD}", ctx.World)
	tmpl = strings.ReplaceAll(tmpl, "{AGENT}", ctx.Agent)
	tmpl = strings.ReplaceAll(tmpl, "{QUALITY_GATES}", gatesSection)
	tmpl = strings.ReplaceAll(tmpl, "{OUTPUT_DIR}", outputSection)

	return tmpl
}

// GenerateWorkflow returns the SKILL.md content for the sol-workflow skill.
func GenerateWorkflow(ctx WorkflowContext) string {
	// Template uses § as a backtick placeholder.
	tmpl := `---
name: sol-workflow
description: Execute workflow steps — read current step, execute instructions, advance to next.
---

# sol-workflow

Execute workflow steps in a structured loop.

## Step Loop

Follow this loop until all steps are complete:

1. **Read** current step: §sol workflow current --world={WORLD} --agent={AGENT}§
2. **Execute** the step instructions exactly as written
3. **Advance** to next step: §sol workflow advance --world={WORLD} --agent={AGENT}§
4. **Repeat** from step 1

## Commands

| Command | Description |
|---------|-------------|
| §sol workflow current --world={WORLD} --agent={AGENT}§ | Read current step instructions |
| §sol workflow advance --world={WORLD} --agent={AGENT}§ | Mark step complete, advance to next |
| §sol workflow status --world={WORLD} --agent={AGENT}§ | Check overall progress |

## Workflow Completion

When the last step is advanced, the workflow status becomes §done§.
At that point, run §sol resolve§ to submit your work.

## Looping Workflows

Some workflows (like forge patrol) loop continuously — when the last step completes,
the formula is re-instantiated and the workflow restarts from step 1. In this case,
do not resolve after the workflow completes; instead, continue the loop.

## Important

- Execute each step's instructions **exactly** as written
- Do not skip steps or execute them out of order
- Check §sol workflow status§ if you are unsure where you are
- If a step fails, escalate: §sol escalate "step N failed: description"§
`

	tmpl = strings.ReplaceAll(tmpl, "§", "`")
	tmpl = strings.ReplaceAll(tmpl, "{WORLD}", ctx.World)
	tmpl = strings.ReplaceAll(tmpl, "{AGENT}", ctx.Agent)

	return tmpl
}

// GenerateForgeOps returns the SKILL.md content for the sol-forge-ops skill.
func GenerateForgeOps(ctx ForgeOpsContext) string {
	// Template uses § as a backtick placeholder.
	tmpl := `---
name: sol-forge-ops
description: Merge pipeline operations — claim, merge, run gates, mark results.
---

# sol-forge-ops

Merge pipeline operations for the forge agent.

## Queue Scanning

Scan for merge requests ready to process:

§§§
sol forge ready --world={WORLD} --json
§§§

Check for previously blocked MRs that are now unblocked:

§§§
sol forge check-unblocked --world={WORLD}
§§§

## Claiming

Claim the next ready merge request:

§§§
sol forge claim --world={WORLD} --json
§§§

This atomically claims an MR so no other process can work on it.

## Sync

Sync your worktree to the latest target branch before merging:

§§§
sol forge sync --world={WORLD}
§§§

## Squash Merge

Perform the squash merge using git directly:

§§§
git merge --squash origin/<branch>
git commit -m "<merge commit message>"
§§§

## Quality Gates

Run all quality gate commands after merging. If any gate fails, the merge
must be rolled back and the MR marked as failed.

## Push

Push the merged result to the target branch:

§§§
git push origin HEAD:{TARGET_BRANCH}
§§§

## Mark Results

After processing, mark the MR with the outcome:

| Outcome | Command |
|---------|---------|
| Merge succeeded | §sol forge mark-merged --world={WORLD} <id>§ |
| Merge or gates failed | §sol forge mark-failed --world={WORLD} <id>§ |

## Conflict Handling

When a merge has conflicts:

1. Abort the merge: §git merge --abort§
2. Create a resolution task: §sol forge create-resolution --world={WORLD} <id>§

This blocks the MR and creates a writ for an outpost agent to resolve the conflict.

## Release

Release a claimed MR back to the ready queue for retry:

§§§
sol forge release --world={WORLD} <id>
§§§

## Check Unblocked

Check for MRs whose blockers have been resolved:

§§§
sol forge check-unblocked --world={WORLD}
§§§

## Pause and Resume

Check pause state before claiming:

§§§
sol forge status {WORLD} --json
§§§

If paused, do not claim new MRs. Wait for a §FORGE_RESUMED§ nudge:

§§§
sol forge await --world={WORLD} --timeout=60
§§§

## Error Handling

| Situation | Action | Do NOT |
|-----------|--------|--------|
| Merge succeeds, gates pass, push succeeds | §sol forge mark-merged --world={WORLD} <id>§ | — |
| Merge has conflicts | §git merge --abort§, §sol forge create-resolution --world={WORLD} <id>§ | Resolve conflicts yourself |
| Quality gates fail | §git reset --hard origin/{TARGET_BRANCH}§, §sol forge mark-failed --world={WORLD} <id>§ | Read test output or investigate |
| Push rejected | §git reset --hard origin/{TARGET_BRANCH}§, §sol forge release --world={WORLD} <id>§, retry | Debug the rejection |
| Unexpected error | §sol forge mark-failed --world={WORLD} <id>§ | Attempt recovery |
| sol command fails | Retry once, then §sol forge mark-failed§ | Loop retrying forever |

## Command Quick-Reference

| Want to... | Command |
|------------|---------|
| Check for unblocked MRs | §sol forge check-unblocked --world={WORLD}§ |
| Scan queue | §sol forge ready --world={WORLD} --json§ |
| Claim next MR | §sol forge claim --world={WORLD} --json§ |
| Sync worktree | §sol forge sync --world={WORLD}§ |
| Squash merge | §git merge --squash origin/<branch>§ |
| Run quality gates | Run each gate command directly |
| Push to target | §git push origin HEAD:{TARGET_BRANCH}§ |
| Mark as merged | §sol forge mark-merged --world={WORLD} <id>§ |
| Mark as failed | §sol forge mark-failed --world={WORLD} <id>§ |
| Request resolution | §sol forge create-resolution --world={WORLD} <id>§ |
| Release for retry | §sol forge release --world={WORLD} <id>§ |
| Check pause state | §sol forge status {WORLD} --json§ |
`

	tmpl = strings.ReplaceAll(tmpl, "§", "`")
	tmpl = strings.ReplaceAll(tmpl, "{WORLD}", ctx.World)
	tmpl = strings.ReplaceAll(tmpl, "{TARGET_BRANCH}", ctx.TargetBranch)

	return tmpl
}

// InstallSkill writes a skill's SKILL.md to .claude/agents/{name}/SKILL.md
// in the given directory.
func InstallSkill(dir, name, content string) error {
	skillDir := filepath.Join(dir, ".claude", "agents", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("failed to create skill directory %q: %w", name, err)
	}

	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write SKILL.md for %q: %w", name, err)
	}
	return nil
}
