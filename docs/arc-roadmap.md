# Sol — Arc Roadmap

Arcs are the post-build iteration model. Loops 0–5 built the system from
scratch. Arcs refine, rename, and operationalize it.

---

## Arc 0: Rename (gt → sol)

Full codebase rename from Gastown (gt) to Sol. Mechanically large, conceptually simple.

- Renamed binary, Go module, env vars: gt/GT_HOME → sol/SOL_HOME
- Renamed all domain terms (see `docs/naming.md` for full glossary)
- Updated all Go code, tests, docs, Makefile, schema migration V4
- Full migration reference: `docs/naming.md`

**Acceptance:** `make build && make test` passes. All CLI commands use new names.
Binary is `bin/sol`. No remaining references to old naming in source.

**Status:** Complete. Initial rename (b760ea2), review (d9662ab), final cleanup (28986b1).

---

## Arc 1: World Lifecycle

Explicit world management — the biggest operational gap.

- `sol world init <name>` — create world database, directory structure, optional source repo association
- `sol world list` — list all registered worlds from sphere database
- `sol world status <name>` — aggregate view (agents, work items, active sessions, config)
- `sol world delete <name>` — safe teardown with confirmation
- Source repo association — persisted in world.toml, no longer relies on cwd for `cast`
- Configuration files: `sol.toml` (global), `world.toml` (per-world)
- Configuration surface: quality gates, agent capacity, model tier, name pool path
- Hard gate: `sol world init` required before any world operation

**Acceptance:** Operator can fully manage world lifecycle through CLI.
Worlds are explicit, discoverable, and configurable.

**Status:** Complete.
- Schema V5: `worlds` table in sphere.db
- Config: `world.toml` (per-world), `sol.toml` (global), three-layer resolution
- ADR-0008: Dual-store design rationale

---

## Arc 2: Operator Onboarding

Make the system approachable for first-time operators.

- `sol doctor` — validate prerequisites (tmux, git, claude, writable dirs, SQLite WAL)
- `sol init` — guided first-time setup (create SOL_HOME, first world)
- Actionable error messages when prerequisites fail (not just "exec: tmux: not found")
- Operator quick-start documentation

**Acceptance:** A new operator can go from zero to first successful `cast` with
clear guidance at every step. `sol doctor` catches all common setup issues.

---

## Arc 3: Envoy + Governor

Persistent agents and per-world work coordination. See ADR-0009 (envoy) and
ADR-0010 (governor).

### Envoy — Context-Persistent Agents

Human-directed, persistent agents for pair programming, research, and design
collaboration. Maintain accumulated context (brief) across sessions.

- Agent role `envoy` in agents table. Directory at `$SOL_HOME/{world}/envoys/{name}/`
- Persistent worktree (like forge) — not torn down on resolve
- Brief system: agent-maintained `.brief/memory.md` (GLASS-inspectable)
- Claude Code hooks: `SessionStart` injects brief, `Stop` prompt hook ensures save before exit
- `SessionStart` compact hook re-injects brief after context compaction
- Voluntary tether: envoy can bind to existing work items or create its own
- Resolve creates MR (through forge) but does not kill session or clear worktree
- Sentinel and prefect skip `role=envoy` — human-supervised, not auto-respawned
- CLI: `sol envoy create/start/stop/attach/list/brief/debrief`

### Governor — Per-World Work Coordinator

Singleton Claude session per world that handles natural language work dispatch.
Architecturally similar to forge: Claude session + sol CLI toolbox (ADR-0005 pattern).

- Agent role `governor` in agents table. Directory at `$SOL_HOME/{world}/governor/`
- Read-only mirror of main at `governor/mirror/` — for codebase research, never edited
- Mirror auto-refreshes on session start + periodic pulls
- Uses brief system for accumulated world knowledge (patterns, agent capabilities, preferences)
- NL work dispatch: parses operator intent → creates work items, caravans, dispatches via cast
- Claude handles NL parsing and coordination logic; Go CLI handles mechanical operations
- CLI: `sol governor start/stop/attach/brief/debrief` (singleton — no `create`)

### Shared Infrastructure

- Brief system (`.brief/memory.md` + hooks) shared between envoy and governor
- Both use persistent Claude sessions with context that survives restarts
- Shared namespace with outposts: agent IDs remain `{world}/{name}`

**Acceptance:** Operator can create persistent envoys for collaborative work
and start a governor for natural language work dispatch. Brief context survives
session restarts and compaction.

---

## Arc 4: Senate — Sphere-Scoped Planning

Cross-world work planning and coordination. See ADR-0011 (senate).

### Senate — Sphere-Scoped Work Planner

Claude session for multi-world planning. Operator-started/stopped — not
always running, not supervised. Sits above governors in the planning hierarchy.

- Sphere-scoped singleton. Directory at `$SOL_HOME/senate/`
- Claude session + sol CLI toolbox (ADR-0005 pattern)
- Brief system for accumulated sphere-wide knowledge
- Lazy world summary loading — persona boot says summaries exist, pulls on demand
- Interactive governor queries via synchronous CLI (`sol world query`)
- Creates cross-world work items, caravans, dependencies
- Delegates per-world dispatch to governors
- CLI: `sol senate start/stop/attach/brief/debrief`

### Governor Enhancements

- Governor maintains `world-summary.md` — separate file from brief, read by Senate
- Governor handles query injection protocol — responds to Senate questions synchronously

### Supporting CLI

- `sol world summary {name}` — read governor-maintained world summary
- `sol world query {name} "question"` — synchronous query to a world's governor

**Acceptance:** Operator can start a Senate session, plan work across
multiple worlds through conversation, and have items/caravans/dependencies
created across worlds. Senate can query governors for world-specific context.

---

## Arc 5: Agent History & Cost Tracking

Audit trail and cost visibility — specified in target architecture, never built.

- `agent_history` table: agent_id, work_item_id, action, started_at, ended_at, summary, cost_usd
- Instrument cast/resolve to write history records
- `sol agent history <name>` — show work trail for an agent
- `sol agent history --world=<name>` — show all agent activity in a world
- Cost aggregation views (per-agent, per-world, per-time-period)

**Acceptance:** Every cast/resolve cycle produces a history record.
Operators can answer "what did agent X work on?" and "what did world Y cost?"

---

## Arc 6: Operational Tooling

Production operations at scale.

- `sol world export <name>` — backup world state (database + directory tree)
- `sol world import <archive>` — restore from backup
- Multi-world prefect selection: `sol prefect run --worlds=a,b`
- Schema migration tooling for upgrades
- World cloning: `sol world clone <source> <target>`

**Acceptance:** Operators can backup, restore, and manage multiple worlds
without manual filesystem operations.

---

## Future: Consul AI Enhancement

Proactive coordination at sphere level. Enhancement to consul (not a new
component), following the sentinel pattern (ADR-0001/ADR-0003): Go patrol loop
with targeted `claude -p` call-outs when judgment is needed. Rebalancing,
intelligent escalation, cross-world pattern detection.
