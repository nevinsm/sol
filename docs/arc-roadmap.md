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

### Status Overhaul

Polished status output using Charmbracelet lipgloss. New sphere-level
overview for system-wide visibility.

- `sol status` (no args) — sphere overview: sphere processes (prefect,
  consul, chronicle), worlds table with per-world summary, open caravans
- `sol status <world>` — updated rendering with lipgloss styling
- Charmbracelet lipgloss for section headers, tables, colored status
  indicators. `--json` bypasses all styling.
- Rendering separated from data gathering (`status/render.go`)
- Consul status added to sphere process checks (missing today)
- Sphere health: aggregate of all world health + sphere process health

Role-aware sections (outposts/envoys/governor) land with Arc 3 when
those roles exist.

**Acceptance:** A new operator can go from zero to first successful `cast` with
clear guidance at every step. `sol doctor` catches all common setup issues.
`sol status` gives a system-wide overview at a glance.

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
- Resolve creates MR (through forge) but does not kill session or clear worktree.
  Worktree reset is agent-managed: CLAUDE.md tells envoy to checkout main
  and pull before starting new work. No forge-to-envoy coupling.
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

- Shared namespace with outposts: agent IDs remain `{world}/{name}`
- Both use persistent Claude sessions with context that survives restarts

### Brief System

Context persistence across Claude Code sessions. Agent-maintained markdown
files injected on session start, re-injected after compaction, save-checked
on stop. Shared by envoy, governor, and senate (Arc 4).

**Files:**

| File | Owner | Purpose |
|------|-------|---------|
| `.brief/memory.md` | envoy, governor, senate | Internal accumulated knowledge (freeform) |
| `.brief/world-summary.md` | governor only | External-facing world summary (structured) |

**CLI:**

- `sol brief inject --path=<path> --max-lines=200` — reads brief, truncates
  if over limit, outputs framed content to stdout. Also writes
  `.brief/.session_start` timestamp for the stop hook.
- `sol brief check-save <path>` — stop hook command. Checks brief mtime
  against `.session_start`. Blocks stop if brief hasn't been updated.
  Checks `stop_hook_active` to prevent infinite loops.

**Hooks (installed by `sol envoy/governor/senate start`):**

| Event | Matcher | Command | Purpose |
|-------|---------|---------|---------|
| `SessionStart` | `startup\|resume` | `sol brief inject` | Inject brief into session context |
| `SessionStart` | `compact` | `sol brief inject` | Re-inject after context compaction |
| `Stop` | — | `sol brief check-save` | Nudge agent to update brief before exit |

**Size management (three layers):**

1. CLAUDE.md guidance: "Keep your brief under 200 lines. Consolidate
   older entries."
2. AI self-management: agent prunes organically, especially at stop
   consolidation points.
3. Injection truncation: `sol brief inject` hard-caps at 200 lines.
   Truncation notice tells agent to read full file and consolidate.

**Brief format:**

- `memory.md` — freeform with CLAUDE.md guidance. Agent organizes
  naturally (same model as Claude Code's own MEMORY.md).
- `world-summary.md` — prescribed sections for external consumers:

```
# World Summary: {world}
## Project       — what this codebase is
## Architecture  — key modules, patterns, tech stack
## Priorities    — active work themes, what's in flight
## Constraints   — known problem areas, things to avoid
```

**Crash recovery:** Brief files survive crashes. Next startup injects
whatever was last saved (possibly stale). CLAUDE.md tells agent to review.

### Caravan Phases — Cross-World Sequencing

Schema V6: `phase INTEGER` column on `caravan_items` in sphere.db.
Phase 0 items dispatch immediately. Phase N items wait until all items
in phases < N are done/closed.

- Composes with within-world dependencies: phases for cross-world
  sequencing, `dependencies` table for within-world DAGs
- Folds into `CheckCaravanReadiness` — consul and governor just check
  `Ready`, no phase-specific code needed
- Backward compatible: existing items default to phase 0
- Governor checks caravan readiness (including phases) before dispatch
- Senate (Arc 4) creates phased caravans for cross-world work planning

### Status Display — Role-Aware Sections

Update `sol status <world>` with role-separated sections (building on
the lipgloss rendering from Arc 2):

- **Processes:** forge, sentinel, governor (as a singleton like forge)
  plus sphere-level (prefect, consul, chronicle)
- **Outposts:** agents with role=agent (existing behavior, filtered)
- **Envoys:** agents with role=envoy, with BRIEF column (mtime age)
- Sections omitted when empty (no envoys → no Envoys section)
- Governor and envoy session status do NOT affect health — they are
  operator-managed, not system-critical
- Caravan display gains phase progress breakdown

**Acceptance:** Operator can create persistent envoys for collaborative work
and start a governor for natural language work dispatch. Brief context survives
session restarts and compaction. Caravan phases enforce cross-world work
ordering. `sol status` cleanly separates outposts, envoys, and governor.

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
- Governor notification: caravan items need no notification (consul
  handles stranded caravans). Standalone items — Senate sends mail
  explicitly to `{world}/governor` with `WORK_ITEM_CREATED` subject.
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
