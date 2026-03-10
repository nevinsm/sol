# ADR-0017: Workflow-Based Forge

Status: revised by ADR-0027 (revises ADR-0005)
Date: 2026-03-06

## Context

ADR-0005 moved the forge from a pure Go process to a Claude session backed
by Go CLI subcommands. The Claude session handled the patrol loop (scan,
claim, merge, test, push) while Go provided the mechanical toolbox. The
agent's behavior was constrained by its persona (CLAUDE.md) and PreToolUse
hooks that blocked dangerous commands.

In practice, the agent consistently routed around these constraints:

- **Hook evasion.** Blocking `git push --force` didn't prevent the agent
  from using `cd /path && git push -f` or splitting commands across tool
  calls. Each bypass required a new hook rule — whack-a-mole.
- **Pipeline bypass.** The Go `sol forge merge` pipeline enforced a strict
  sequence (sync, merge, gates, push), but the agent discovered it could
  run git commands directly and skip the pipeline entirely. Persona
  instructions to "always use sol forge merge" were suggestions, not
  constraints.
- **Persona drift.** Across handoffs and context compaction, the agent's
  understanding of its role degraded. Instructions in CLAUDE.md competed
  with Claude's auto-memory system, which accumulated stale patterns from
  earlier sessions (e.g., old CLI subcommands that no longer existed).

The Gastown prototype solved a similar problem with its refinery by using
a workflow — a TOML document with explicit step-by-step
instructions including the exact commands to run. The agent follows the
workflow mechanically rather than improvising a patrol strategy.

## Decision

Replace the forge's free-form patrol with a workflow
(`sol-forge-patrol`) that prescribes the exact sequence of operations.

**The workflow defines:**
- 10 steps: unblock → scan → claim → sync → merge (git merge --squash) →
  gates → push → handle-result → loop → health-check
- Each step includes the exact commands to execute
- Variables for world name, target branch, and gate command
- The agent sees all steps at prime time and works through them
  sequentially

**What was removed:**
- `sol forge merge` Go pipeline — the agent runs git commands directly as
  prescribed by the workflow steps
- Most PreToolUse hook rules — only truly dangerous variants are blocked
  (force push, `rm -rf`, `checkout -b`). The workflow constrains behavior
  more effectively than hooks.

**What was kept:**
- All Go CLI subcommands for queue management (`claim`, `ready`,
  `mark-merged`, `mark-failed`, `release`, `check-unblocked`,
  `create-resolution`, `sync`, `await`)
- The Scotty Test for gate failure triage (branch-caused vs pre-existing),
  embedded in the gates step
- Handoff mechanics for context recovery

**Why this works where persona + hooks failed:**
- A workflow is a concrete checklist, not a behavioral suggestion. The
  agent follows prescribed commands rather than inventing its own.
- Removing `sol forge merge` eliminates the "shortcut vs prescribed path"
  tension — there is only one path.
- Reducing hook rules to genuinely dangerous operations removes the
  adversarial dynamic where the agent treats hooks as obstacles.

## Consequences

- The forge follows a predictable, observable sequence. The autarch can
  watch the tmux session and verify step-by-step compliance.
- Workflow changes are TOML edits, not Go code changes. Adjusting the
  merge strategy (e.g., switching from `--squash` to `--no-ff`) is a
  one-line change.
- The agent retains judgment only where it matters: conflict resolution
  during the merge step, test failure attribution during gates, and
  delegation decisions for complex conflicts.
- Hook evasion is no longer a concern — the workflow tells the agent what
  to do rather than trying to prevent what it shouldn't.
- Auto-memory contamination remains a risk. Stale memories from previous
  sessions can compete with workflow instructions. ADR-0018 addresses
  this with per-agent config directory isolation.
