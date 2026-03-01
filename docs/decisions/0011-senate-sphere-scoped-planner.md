# ADR-0011: Senate as Sphere-Scoped Planning Session

Status: accepted
Date: 2026-03-01
Arc: 4

## Context

The governor (ADR-0010) handles per-world work coordination: natural language
dispatch, caravan creation, and cast coordination within a single world. The
consul (ADR-0007) handles sphere-wide infrastructure patrol: stale tether
recovery, stranded caravan feeding, and lifecycle management.

Neither component handles cross-world work planning — the task of breaking a
high-level goal into work items, dependencies, and caravans that span multiple
worlds. This gap surfaces when an operator needs to coordinate features or
changes across projects:

- "Add authentication across the API, web, and shared-libs worlds"
- "Refactor the database schema in core, then update consumers in services"
- "Plan the release 2.0 work across all worlds"

Three approaches were evaluated:

1. **Governor with cross-world reach** — let any governor create items in other
   worlds. Rejected: muddies the governor's per-world role. A governor governs
   its world; creating work in another world crosses that boundary.

2. **Consul AI enhancement** — add planning via targeted `claude -p` callouts.
   Rejected: consul is a patrol process. Planning is creative and analytical,
   not a patrol task. Wrong abstraction.

3. **New sphere-scoped component** — a dedicated planning session above
   governors. Clean separation: governors govern, the planner plans across
   worlds. Governors provide world context; the planner synthesizes and creates
   cross-world work.

## Decision

Introduce **Senate** as a sphere-scoped planning component. Senate is a Claude
session backed by sol CLI subcommands, following the forge/governor
architecture pattern (ADR-0005). It is a sphere-scoped singleton — one Senate
session for the entire sphere.

**Session lifecycle:**

Senate is operator-started and operator-stopped. It is not supervised by
prefect or consul, not always-running, and has no heartbeat or respawn logic.
The human is the supervisor — same model as envoy (ADR-0009). Senate exists
when the operator needs it and doesn't when they don't.

**What Senate does:**

- Reads governor-maintained world summaries for cross-world context
- Queries governors interactively for world-specific knowledge
- Proposes work item breakdowns across worlds
- Creates work items via `sol store create-item --world=X`
- Sets up cross-world dependencies via caravan phases
- Creates caravans grouping related cross-world items
- Delegates per-world dispatch to governors

**What Senate does NOT do:**

- Read or write code (no mirror, no worktree)
- Dispatch work to agents (governor's responsibility)
- Monitor health or execution (sentinel/consul)
- Patrol for stale state (consul)
- Per-world work planning (governor)

**Context model:**

Senate's startup is lean. Its CLAUDE.md tells it that world summaries are
available via `sol world summary {name}` and governors are queryable via
`sol world query {name} "question"`, but it does not pre-load any world
context. Senate pulls summaries on demand when the conversation warrants it.

Senate uses the brief system (ADR-0009 infrastructure) for accumulated
sphere-wide knowledge — project relationships, past planning decisions,
cross-world patterns, operator preferences.

**Senate-Governor interaction:**

Senate queries governors through a synchronous CLI mechanism:

1. `sol world query {world} "question"` injects the question into the
   governor's tmux session
2. Governor processes the question, writes response to a known file
3. CLI reads the response and returns it to Senate

If the governor isn't running, the query returns an error and Senate falls
back to the static world summary. DEGRADE-clean.

**Governor notification after planning:**

Two paths based on whether items are in a caravan:

- Caravan items: no notification needed. Consul already patrols for stranded
  caravans with ready items (ADR-0007).
- Standalone items: Senate sends mail explicitly to `{world}/governor` with
  subject `WORK_ITEM_CREATED`. Senate's CLAUDE.md instructs this — the CLI
  stays generic.

## Consequences

**Benefits:**

- Clean role separation: governor governs per-world, Senate plans across
  worlds. Neither component's responsibilities are muddied.
- Not always-running — zero cost when not in use. Operator starts a session
  for planning, stops when done.
- Follows established patterns: Claude session + Go toolbox (ADR-0005), brief
  system (ADR-0009), operator-managed lifecycle (envoy pattern)
- Hierarchical delegation: Senate reads up from governors (world summaries)
  and writes down to worlds (work items, caravans). Governors handle execution
  within their worlds.
- Caravan phases (sphere.db V6) provide the cross-world sequencing that
  Senate needs — no new dependency mechanism required.

**Tradeoffs:**

- Introduces a new component (Senate) rather than extending an existing one.
  Justified by role clarity — governor and consul should not absorb planning.
- Synchronous query protocol between Senate and governor sessions is a new
  interaction pattern. Adds complexity to the governor (must handle query
  injections) and introduces a timeout/failure mode.
- Senate depends on governor-maintained world summaries being current. If a
  governor's summary is stale, Senate plans with stale context. Mitigated by
  the interactive query mechanism for critical decisions.

**Code changes:**

- New `internal/senate/` package for Senate lifecycle (start, stop, attach)
- New `cmd/senate.go` for CLI commands
- `protocol`: new `SenateCLaudeMD()` generator with sol CLI reference
- `cmd/world.go`: add `sol world summary` and `sol world query` subcommands
- Governor: handle query injection protocol (respond to `.query/pending.md`)
- Governor CLAUDE.md: add world-summary.md maintenance instructions

**Comparison with other components:**

| Aspect | Senate | Governor | Forge | Consul |
|--------|--------|----------|-------|--------|
| Scope | Sphere | Per-world | Per-world | Sphere |
| Session type | Claude + sol CLI | Claude + sol CLI | Claude + sol CLI | Go process |
| Purpose | Cross-world planning | Per-world dispatch | Merge pipeline | Sphere patrol |
| Lifecycle | Operator-managed | Operator-managed | Prefect-managed | Prefect-managed |
| Brief? | Yes | Yes | No | No |
| Always running? | No | While world active | While world active | Yes |
