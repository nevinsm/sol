# ADR-0035: Remove Chancellor Role

Status: accepted
Date: 2026-03-26

Supersedes: ADR-0011 (Senate as Sphere-Scoped Planner), ADR-0029 (Rename Senate to Chancellor)

## Context

The chancellor (originally "senate", ADR-0011, renamed in ADR-0029) was a
sphere-scoped cross-world planning agent. It existed to decompose strategic
goals into writs across multiple worlds and present plans to the autarch for
approval.

The chancellor's design assumed a dedicated planning role was needed because:
1. Governors were per-world and couldn't plan across worlds
2. A three-tier context model (brief → world summary → live governor query)
   was needed to gather cross-world intelligence cheaply

In practice, envoys (ADR-0009) handle cross-world planning better:
- Envoys have direct codebase access via worktrees — no need for the
  three-tier context abstraction that existed because the chancellor lacked
  direct access
- Envoys already have cross-world CLI access (`sol writ create --world=<other>`,
  `sol caravan create`)
- Planning is inherently interactive and human-directed, which is exactly
  the envoy model (persistent human-directed agent with brief system)
- A planner persona template gives any envoy the chancellor's planning
  capabilities without a dedicated role

The chancellor added a role, CLI subcommands, a package, skills, and status
display complexity — all for a function that an envoy with the right persona
handles naturally.

## Decision

Remove the chancellor role entirely. Planning becomes an envoy function:

- **Removed**: `internal/chancellor/` package, `cmd/chancellor.go` CLI,
  chancellor persona prompt, chancellor skills (`world-queries`,
  `writ-planning`), chancellor entries in status display, config model/runtime
  resolution, and all chancellor tests
- **Removed**: The three-tier context model concept — it only existed because
  the chancellor lacked worktree access
- **Preserved**: Caravan system (used by envoys), cross-world writ creation,
  `sol status` (without chancellor in process list)
- **Preserved**: `sol world summary` and `sol world query` — these remain
  available for governor and envoy use
  *(Note: subsequently removed by [ADR-0037](0037-remove-governor-role.md))*

Existing `$SOL_HOME/chancellor/` directories are left in place on existing
installations but no new ones are created.

## Consequences

- One fewer role to understand, configure, and maintain
- Planning is accessible to any envoy session, not locked to a singleton
- The `sol chancellor` command tree no longer exists
- Status displays no longer show chancellor as a sphere process
- ADR-0011 and ADR-0029 are superseded by this decision
