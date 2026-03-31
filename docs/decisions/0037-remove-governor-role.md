# ADR-0037: Remove Governor Role

Status: accepted
Date: 2026-03-26
Supersedes: [ADR-0010](0010-governor-per-world-coordinator.md)

## Context

The governor was introduced in Arc 3 (ADR-0010) as a per-world Claude session
for natural language dispatch — parsing work requests into writs, forming
caravans, and dispatching via `sol cast`. In practice, the operator keeps the
governor offline and routes all planning work through envoys instead.

The governor's responsibilities decompose into three categories, each already
covered:

1. **Planning and writ creation** — envoys handle this directly. An envoy with
   world context and the brief system produces better writs than a separate
   coordinator because it has hands-on familiarity with the codebase.

2. **Mechanical dispatch** — `sol cast`, `sol tether`, and caravan commands are
   CLI operations that don't need a persistent Claude session. The autarch
   manages dispatch decisions directly.

3. **Notification handling** — the governor reacted to MERGED, MERGE_FAILED,
   and AGENT_DONE events. These are already covered: consul handles stale
   caravans (ADR-0007), sentinel handles agent failures (ADR-0001), and the
   autarch manages dispatch decisions. The notification event types remain for
   observability but no autonomous process reacts to them.

Running a persistent Claude session purely for dispatch coordination is
expensive and unnecessary. The governor adds API cost, operational complexity
(session management, brief maintenance, mirror sync), and a role boundary that
creates friction rather than reducing it.

## Decision

Remove the governor role entirely:

- Delete `internal/governor/` package
- Delete `cmd/governor.go` CLI commands
- Remove governor persona, skills, and CLAUDE.md generation
- Remove governor references from prefect, sentinel, forge, consul, status
- Remove `sol world query` and `sol world summary` commands
- Keep notification event types for observability (no autonomous handler)

**Intentionally not replaced:** No new notification handler, dispatcher
process, or automated phase advancement is introduced. Dispatch remains
human-directed through CLI commands.

## Consequences

**Benefits:**
- Eliminates unused API cost (persistent Claude session)
- Simplifies the role model — fewer moving parts to manage
- Removes operational surface area (governor brief, mirror, query protocol)
- Planning through envoys provides better context (direct codebase access)

**Tradeoffs:**
- No autonomous reaction to MERGED/MERGE_FAILED/AGENT_DONE events — the
  autarch must monitor and dispatch manually. This is intentional: human
  control over dispatch decisions.
- `sol world query` no longer available — world state inspection is through
  `sol status` and envoy conversations instead.

**Migration:**
- Governor agent records remain in the database (historical data)
- No schema migration needed — the agents table is generic
- `world.toml` governor model fields are retained for backwards compatibility
  but ignored
