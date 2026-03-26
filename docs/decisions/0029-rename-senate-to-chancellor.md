# ADR-0029: Rename Senate to Chancellor

Status: superseded by ADR-0035 (Remove Chancellor Role)

## Context

The Senate component (ADR-0011) was introduced as a sphere-scoped cross-world planning agent managed by the autarch. The name "senate" implied a deliberative body of multiple members, which did not match the actual implementation: a single persistent AI session with sphere-level visibility and planning authority.

As the system matures, this role is evolving into the primary authority for sphere-wide coordination — managing world lifecycle decisions, cross-world writ planning, and strategic resource allocation. This authority profile fits a "chancellor" archetype more precisely: a single executive with broad jurisdiction, acting as the sovereign's trusted delegate.

## Decision

Rename the Senate component to Chancellor across the entire codebase:

- `internal/senate/` → `internal/chancellor/` (package, all types and functions)
- `cmd/senate.go` → `cmd/chancellor.go` (CLI: `sol senate` → `sol chancellor`)
- Session name constant: `sol-senate` → `sol-chancellor`
- Directory path: `$SOL_HOME/senate/` → `$SOL_HOME/chancellor/`
- All references in prefect, status, protocol, config, dash, and tests updated accordingly

The role's skill set (world-queries, writ-planning, memories) is unchanged. No behavioral changes are introduced.

## Consequences

- `sol chancellor start/stop/attach/brief/debrief/status` replace the equivalent `sol senate` commands
- The chancellor's working directory moves from `$SOL_HOME/senate/` to `$SOL_HOME/chancellor/` — existing senate state (brief, hooks) must be migrated manually on live deployments
- The tmux session name changes from `sol-senate` to `sol-chancellor` — any running senate session must be stopped and restarted as chancellor
- Status displays (sphere overview, world detail, dashboard) now show "Chancellor" instead of "Senate"
- The name better communicates the role's authority: a sphere-scoped executive with world lifecycle authority, not a deliberative assembly
