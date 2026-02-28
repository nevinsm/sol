# Sol — Arc Roadmap

Arcs are the post-build iteration model. Loops 0–5 built the system from
scratch. Arcs refine, rename, and operationalize it.

---

## Arc 0: Rename

Full codebase rename from sol to sol. Mechanically large, conceptually simple.

- Rename binary: `sol` → `sol`
- Rename Go module: `github.com/nevinsm/sol` → TBD
- Rename all internal references: SOL_HOME → SOL_HOME, world → world, etc.
- Rename directories: `outposts/` → `outposts/`, etc.
- Rename CLI commands, flags, help text, error messages
- Rename database references: `sphere.db` → `sphere.db`
- Update ID prefixes: `sol-` → `sol-`
- Update session naming: `sol-{world}-{agent}` → `sol-{world}-{agent}`
- Update all docs, prompts, CLAUDE.md, README
- Update Makefile, go.mod
- Full migration reference: `docs/naming.md`

**Acceptance:** `make build && make test` passes. All CLI commands use new names.
Binary is `bin/sol`. No remaining references to old naming in source.

---

## Arc 1: World Lifecycle

Explicit world management — the biggest operational gap.

- `sol world init <name>` — create world database, directory structure, optional source repo association
- `sol world list` — discover all worlds from `.store/` directory
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

## Arc 3: Agent History & Cost Tracking

Audit trail and cost visibility — specified in target architecture, never built.

- `agent_history` table: agent_id, work_item_id, action, started_at, ended_at, summary, cost_usd
- Instrument cast/resolve to write history records
- `sol agent history <name>` — show work trail for an agent
- `sol agent history --world=<name>` — show all agent activity in a world
- Cost aggregation views (per-agent, per-world, per-time-period)

**Acceptance:** Every cast/resolve cycle produces a history record.
Operators can answer "what did agent X work on?" and "what did world Y cost?"

---

## Arc 4: Operational Tooling

Production operations at scale.

- `sol world export <name>` — backup world state (database + directory tree)
- `sol world import <archive>` — restore from backup
- Multi-world prefect selection: `sol prefect run --worlds=a,b`
- Schema migration tooling for upgrades
- World cloning: `sol world clone <source> <target>`

**Acceptance:** Operators can backup, restore, and manage multiple worlds
without manual filesystem operations.
