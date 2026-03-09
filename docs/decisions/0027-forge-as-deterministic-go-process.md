# ADR-0027: Forge as Deterministic Go Process

Status: accepted (supersedes ADR-0005, revises ADR-0017)
Date: 2026-03-09

## Context

ADR-0005 moved the forge from a pure Go process to a Claude session
because conflicts needed resolution and test failures needed attribution.
ADR-0017 constrained the Claude session with a workflow (`sol-forge-patrol`)
after persona drift and hook evasion undermined the free-form approach.

In practice, every judgment point resolved to mechanical branching:

- **Conflicts:** Delegated to outposts via `create-resolution` — an
  if-statement, not judgment. The forge never resolved conflicts itself.
- **Test failure attribution:** The Scotty Test is deterministic — stash
  the merge, run on base, compare exit codes. No AI reasoning required.
- **Push rejection:** Threshold-based release/fail — a counter comparison.

A workflow-constrained Claude session is a Go process with extra overhead:
API cost, persona drift risk, context compaction, handoff machinery, and
hook evasion surface. The workflow told the agent exactly what commands
to run at each step — the agent added no judgment.

The sentinel (ADR-0001) and consul (ADR-0007) already proved the pattern:
deterministic Go process with targeted AI callouts only when heuristics
detect trouble.

## Decision

Forge runs as a deterministic Go process inside a tmux session.

- Implements the full patrol cycle (unblock → scan → claim → sync →
  merge → gates → push → result → loop) as Go code
- Targeted `claude -p` callouts at failure points only (test failure
  analysis, escalation enrichment)
- Structured stdout for dashboard peek, log file with rotation for
  persistence
- Heartbeat file for prefect supervision
- No restart cycle — Go process runs indefinitely (no context rot)
- Preserves all existing toolbox functions and store interfaces unchanged

The Claude session infrastructure is removed:

- `internal/forge/role.go` — `ForgeRoleConfig()`, persona, hooks, skills,
  prime builder
- `internal/protocol/claudemd.go` — `ForgeClaudeMDContext`,
  `GenerateForgeClaudeMD()`, `InstallForgeClaudeMD()`
- `internal/protocol/prompts/forge.md` — Claude session system prompt
- `internal/protocol/hooks.go` — `InstallForgeHooks()`
- `internal/workflow/defaults/forge-patrol/` — 10-step workflow
- `internal/protocol/skills.go` — forge-patrol, forge-toolbox,
  merge-operations skills
- `startup.Register("forge", ...)` — role registration for session launch

## Consequences

- **Zero API cost during normal merge operations.** AI cost proportional
  to failures only.
- **Deterministic execution.** No persona drift, no auto-memory
  contamination, no hook evasion.
- **No handoff needed.** Go process runs indefinitely — no workflow
  system dependency, no context compaction.
- **Structured observability.** Live peek output, persistent rotated logs,
  heartbeat metrics in status.
- **Forge workflow removed.** The `forge-patrol` workflow is deleted —
  workflow system's scope narrows to formula-based work instantiation.
- **Consistent architecture.** The "deterministic Go + targeted AI"
  pattern is now used by sentinel, consul, and forge.
