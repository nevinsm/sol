# ADR-0011: Senate as Sphere-Scoped Planning Session

Status: accepted (component renamed to Chancellor in ADR-0029)
Date: 2026-03-01
Arc: 4

## Context

The governor (ADR-0010) handles per-world work coordination: natural language
dispatch, caravan creation, and cast coordination within a single world. The
consul (ADR-0007) handles sphere-wide infrastructure patrol: stale tether
recovery, stranded caravan feeding, and lifecycle management.

Neither component handles cross-world work planning — the task of breaking a
high-level goal into writs, dependencies, and caravans that span multiple
worlds. This gap surfaces when the autarch needs to coordinate features or
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

Senate is autarch-started and autarch-stopped. It is not supervised by
prefect or consul, not always-running, and has no heartbeat or respawn logic.
The human is the supervisor — same model as envoy (ADR-0009). Senate exists
when the autarch needs it and doesn't when they don't.

**What Senate does:**

- Reads governor-maintained world summaries for cross-world context
- Queries governors interactively for world-specific knowledge
- Proposes writ breakdowns across worlds
- Creates writs via `sol writ create --world=X`
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
cross-world patterns, autarch preferences.

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
  subject `WRIT_CREATED`. Senate's CLAUDE.md instructs this — the CLI
  stays generic.

## Consequences

**Benefits:**

- Clean role separation: governor governs per-world, Senate plans across
  worlds. Neither component's responsibilities are muddied.
- Not always-running — zero cost when not in use. The autarch starts a session
  for planning, stops when done.
- Follows established patterns: Claude session + Go toolbox (ADR-0005), brief
  system (ADR-0009), autarch-managed lifecycle (envoy pattern)
- Hierarchical delegation: Senate reads up from governors (world summaries)
  and writes down to worlds (writs, caravans). Governors handle execution
  within their worlds.
- Caravan phases (sphere.db V6) provide the cross-world sequencing that
  Senate needs — no new dependency mechanism required.

**Cross-world dependency mechanism:**

Senate needs to express "item A in world X must complete before item B in
world Y starts." Three mechanisms were evaluated:

- **Full DAG at caravan level** — new `caravan_dependencies` table with
  per-item edges. More flexible but adds cycle detection, DAG traversal,
  and complexity. Phases + within-world deps cover all practical cases.
  Can be added later if phases prove insufficient (unlikely).
- **Convention-based** — Senate writes blocking instructions in writ
  descriptions, governors interpret them. No schema enforcement. Fragile —
  depends on AI correctly interpreting free text every time.
- **Caravan phases** (chosen) — `phase INTEGER` column on `caravan_items`
  (sphere.db V6). Phase 0 dispatches first; phase N waits for all items
  in phases < N to complete. Folds into existing `CheckCaravanReadiness` —
  consul and governor just check `Ready` without phase-specific code.
  Composes cleanly with within-world dependencies (which handle intra-world
  ordering). Simple, enforceable, GLASS-inspectable via `sqlite3`.

**Senate-governor query protocol:**

Synchronous injection was chosen over alternatives:

- **Mail-based async** — Senate sends mail, governor reads eventually.
  Too slow for interactive planning sessions where Senate needs an answer
  to continue the conversation.
- **Shared database queries** — Senate reads world DBs directly. Bypasses
  the governor's accumulated knowledge. Governor exists precisely to be
  the informed authority on its world.
- **Synchronous injection** (chosen) — inject question into governor's
  tmux session, poll for response file with timeout. Matches the existing
  session injection pattern (sentinel nudges). Governor responds using its
  full context — mirror, brief, and session knowledge.

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
| Lifecycle | Autarch-managed | Autarch-managed | Prefect-managed | Prefect-managed |
| Brief? | Yes | Yes | No | No |
| Always running? | No | While world active | While world active | Yes |
