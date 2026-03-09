// Package skills provides generators for Claude Code Agent Skills.
//
// Each generator produces a SKILL.md file with YAML frontmatter (name, description)
// and templated content. Skills are installed to .claude/agents/{skill_name}/SKILL.md
// in agent worktrees so Claude Code discovers them as slash commands.
//
// Skill generators:
//   - sol-resolve: Complete work — push branch, create MR, clear tether
//   - sol-workflow: Execute workflow steps — read, execute, advance
//   - sol-forge-ops: Merge pipeline operations — claim, merge, gate, mark
//   - sol-dispatch: Create writs and dispatch work to agents
//   - sol-caravan: Organize writs into phased batches
//   - sol-tether-mgmt: Manage writ tethers for persistent agents
//   - sol-notify: Handle system notifications
//   - sol-status: Read system state — agents, worlds, writs
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
the workflow is re-instantiated and restarts from step 1. In this case,
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

// Skill directory names (also the frontmatter name in each SKILL.md).
const (
	SkillDispatch   = "sol-dispatch"
	SkillCaravan    = "sol-caravan"
	SkillTetherMgmt = "sol-tether-mgmt"
	SkillNotify     = "sol-notify"
	SkillStatus     = "sol-status"
)

// GenerateDispatch returns the SKILL.md content for the dispatch skill.
func GenerateDispatch(world, solBinary string) string {
	tmpl := `---
name: sol-dispatch
description: Create writs and dispatch work to agents. Use when breaking down requests into tasks, setting up dependencies, or casting work.
---

# Dispatch Work

Create writs, set up dependencies, and cast work to agents.

## Creating Writs

§§§bash
{SOL_BINARY} writ create --world={WORLD} --title="..." --description="..."
§§§

Flags:
- §--title§ (required) — short summary of the work
- §--description§ — full context the agent needs to work autonomously
- §--kind§ — §code§ (default) or §analysis§
- §--priority§ — 1 (high), 2 (normal), 3 (low)
- §--label§ — repeatable label for filtering
- §--metadata='{"key":"value"}'§ — structured metadata

### Kind Selection

- §--kind=code§ (default) — produces code changes. Resolve pushes a branch and creates a merge request that flows through the forge merge pipeline.
- §--kind=analysis§ — produces findings, reports, or structured data written to the output directory. Resolve closes the writ directly — no branch, no MR, no forge involvement.

## Dependencies

Add a dependency edge so §<from-id>§ depends on §<to-id>§:

§§§bash
{SOL_BINARY} writ dep add <from-id> <to-id> --world={WORLD}
§§§

A writ is not ready for dispatch until all of its dependencies are closed.

## Casting Work

Assign a writ to an agent and start its session:

§§§bash
{SOL_BINARY} cast <writ-id> --world={WORLD}
§§§

The system auto-selects an idle agent. To target a specific agent: §--agent=<name>§.

## Querying Ready Writs

List writs that have no unresolved dependencies and are ready for dispatch:

§§§bash
{SOL_BINARY} writ ready --world={WORLD}
§§§

## Agent Availability

Check which agents are available for work:

§§§bash
{SOL_BINARY} agent list --world={WORLD}
§§§

## Guidance

- **One concern per writ** — keep each writ focused on a single task or change
- **Include enough context** — the agent works autonomously and needs full context in the description
- **Set dependencies** — if writ B needs writ A's output, add the dependency edge
- **Check availability** — verify an agent is idle before casting
`

	tmpl = strings.ReplaceAll(tmpl, "§", "`")
	tmpl = strings.ReplaceAll(tmpl, "{WORLD}", world)
	tmpl = strings.ReplaceAll(tmpl, "{SOL_BINARY}", solBinary)
	return tmpl
}

// GenerateCaravan returns the SKILL.md content for the caravan skill.
func GenerateCaravan(world, solBinary string) string {
	tmpl := `---
name: sol-caravan
description: Organize related writs into phased batches. Use when grouping writs, setting execution order, or tracking batch progress.
---

# Caravans

Organize related writs into phased batches with ordered execution.

## Creating a Caravan

§§§bash
{SOL_BINARY} caravan create "caravan name" <item-id> [<item-id> ...] --world={WORLD}
§§§

Creates a caravan with optional initial items. Items start in phase 1.

## Adding Items

§§§bash
{SOL_BINARY} caravan add <caravan-id> <item-id> [<item-id> ...] --world={WORLD}
§§§

## Phase Sequencing

Items execute in phase order. Within the same phase, items run in parallel.

§§§bash
{SOL_BINARY} caravan set-phase <caravan-id> <item-id> <phase> --world={WORLD}
§§§

- Phase 1 items dispatch first
- Phase 2 items wait until all phase 1 items are closed
- Phase 3 items wait until all phase 2 items are closed
- And so on

## Caravan Lifecycle

- §drydock§ — initial state, add items and set phases
- §open§ — commissioned and active, items can be dispatched
- §closed§ — all items completed

§§§bash
{SOL_BINARY} caravan commission <caravan-id> --world={WORLD}   # drydock -> open
{SOL_BINARY} caravan drydock <caravan-id> --world={WORLD}      # open -> drydock
{SOL_BINARY} caravan close <caravan-id> --world={WORLD}        # mark completed
{SOL_BINARY} caravan reopen <caravan-id> --world={WORLD}       # closed -> drydock
§§§

## Checking Status

§§§bash
{SOL_BINARY} caravan status <caravan-id> --world={WORLD}
§§§

Shows items grouped by phase with their current status.

## Ready Query Interaction

§{SOL_BINARY} writ ready --world={WORLD}§ respects phase gating. A writ inside a caravan only appears as ready when:
1. All its writ-level dependencies are closed
2. All items in earlier phases of the same caravan are closed

## Launching Ready Items

§§§bash
{SOL_BINARY} caravan launch <caravan-id> --world={WORLD}
§§§

Dispatches all ready items in the caravan to available agents.
`

	tmpl = strings.ReplaceAll(tmpl, "§", "`")
	tmpl = strings.ReplaceAll(tmpl, "{WORLD}", world)
	tmpl = strings.ReplaceAll(tmpl, "{SOL_BINARY}", solBinary)
	return tmpl
}

// GenerateTetherMgmt returns the SKILL.md content for the tether management skill.
func GenerateTetherMgmt(world, agent string) string {
	tmpl := `---
name: sol-tether-mgmt
description: Manage writ tethers for persistent agents — bind, unbind, switch active focus.
---

# Tether Management

Bind, unbind, and switch active writs for persistent agents.

## Tether Directory Model

Each agent has a §.tether/§ directory. Each file in it represents one bound writ:
- One file per writ — the file's name is the writ ID
- Presence of the file means the writ is bound to this agent
- Multiple writs can be tethered simultaneously

## Lightweight Tether

Bind a writ to a persistent agent without creating a worktree or starting a new session:

§§§bash
sol tether <writ-id> --agent={AGENT} --world={WORLD}
§§§

This is a lightweight binding — the writ becomes associated with the agent, but no new session starts.

## Untether

Unbind a writ from a persistent agent:

§§§bash
sol untether <writ-id> --agent={AGENT} --world={WORLD}
§§§

## Activate

Switch which tethered writ is the active focus:

§§§bash
sol writ activate <writ-id> --agent={AGENT} --world={WORLD}
§§§

Only one writ can be active at a time. The active writ determines what the agent works on.

## Active Writ Concept

- **Database** tracks which writ is the active focus for each agent
- **Tether directory** (§.tether/§) tracks which writs are bound
- An agent can have many tethered writs but only one active writ
- Switching the active writ restarts the session with the new writ's context

## Cast vs Tether

| | Cast | Tether |
|---|---|---|
| Creates worktree | Yes | No |
| Starts session | Yes | No |
| Binds writ | Yes | Yes |
| For | Outpost agents (one-shot workers) | Persistent agents (envoy, governor) |
`

	tmpl = strings.ReplaceAll(tmpl, "§", "`")
	tmpl = strings.ReplaceAll(tmpl, "{WORLD}", world)
	tmpl = strings.ReplaceAll(tmpl, "{AGENT}", agent)
	return tmpl
}

// GenerateNotify returns the SKILL.md content for the notification handling skill.
func GenerateNotify(world string) string {
	tmpl := `---
name: sol-notify
description: Handle system notifications — AGENT_DONE, MERGED, MERGE_FAILED, RECOVERY_NEEDED.
---

# Notification Handling

System notifications arrive via the UserPromptSubmit hook. They appear as
§[NOTIFICATION] TYPE: Subject — Body§ in your context at each turn boundary.

## AGENT_DONE

An outpost agent resolved a writ.

**Fields:** §writ_id§, §agent_name§, §branch§, §title§, §merge_request_id§

**Response:**
1. Check caravan status for the completed writ's caravan
2. Look for newly unblocked items to dispatch
3. If this was the last item in a caravan, note caravan completion
4. Dispatch next ready work if agents are available
5. Update your brief

## MERGED

The forge successfully merged a writ's branch.

**Fields:** §writ_id§, §merge_request_id§

**Response:**
1. Update brief — note the item is merged
2. Check if the caravan is fully merged — note completion if so
3. Check if blocked items in other caravans are now unblocked
4. Dispatch any newly ready work

## MERGE_FAILED

The forge failed to merge a writ's branch.

**Fields:** §writ_id§, §merge_request_id§, §reason§

**Response:**
1. Assess the failure reason
2. Consider re-dispatching to an outpost for conflict resolution
3. Escalate if repeated failures occur

## RECOVERY_NEEDED

Sentinel exhausted respawn attempts for an agent.

**Fields:** §agent_id§, §writ_id§, §reason§, §attempts§

**Response:**
1. Assess whether to re-dispatch the writ to a different agent
2. Escalate if the failure is systemic
3. Update brief with dead agent info

## MR_READY

An outpost resolved a writ and created a merge request (forge-specific).

**Fields:** §writ_id§, §merge_request_id§, §branch§, §title§

**Response:**
- The merge request should appear in the forge ready queue
- No action needed from coordinators — forge handles it

## FORGE_PAUSED

The operator paused the forge (forge-specific).

**Response:**
- Do not expect merges to complete until resumed
- Writs can still be dispatched and resolved

## FORGE_RESUMED

The operator resumed the forge (forge-specific).

**Response:**
- Merges resume processing
- Check for any queued merge requests
`

	tmpl = strings.ReplaceAll(tmpl, "§", "`")
	tmpl = strings.ReplaceAll(tmpl, "{WORLD}", world)
	return tmpl
}

// GenerateStatus returns the SKILL.md content for the status skill.
func GenerateStatus(world, solBinary string) string {
	tmpl := `---
name: sol-status
description: Read system state — agent status, world overview, writ progress.
---

# System Status

Read agent status, world overview, and writ progress.

## Sphere Overview

High-level view of all worlds and sphere processes:

§§§bash
{SOL_BINARY} status
§§§

## Per-World Detail

Detailed status for a specific world — agents, writs, forge, services:

§§§bash
{SOL_BINARY} status --world={WORLD}
§§§

## Agent Listing

List all agents with their current state and tethered work:

§§§bash
{SOL_BINARY} agent list --world={WORLD}
§§§

## Ready Writs

Show writs that are ready for dispatch (dependencies satisfied, phase-gated):

§§§bash
{SOL_BINARY} writ ready --world={WORLD}
§§§

## World Sync

Sync the managed repo with its remote to get the latest code:

§§§bash
{SOL_BINARY} world sync --world={WORLD}
§§§
`

	tmpl = strings.ReplaceAll(tmpl, "§", "`")
	tmpl = strings.ReplaceAll(tmpl, "{WORLD}", world)
	tmpl = strings.ReplaceAll(tmpl, "{SOL_BINARY}", solBinary)
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
