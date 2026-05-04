# System Principles

Quick-reference for sol's design philosophy, architectural patterns, and
operational conventions. Drawn from the manifesto, target architecture,
failure modes specification, and ADRs.

---

## Named Principles

### ZFC — Zero Filesystem Cache

Never cache **coordination state** in memory. Always derive from the
authoritative source (database, filesystem, tmux) at point of use.

> "With 30 concurrent agents mutating state, any cache is a lie waiting to
> happen. This is how Unix tools work — `ls` reads the directory fresh every
> time — and it's the right model." — manifesto

**Rationale:** Stale reads of coordination state cause silent failures in
concurrent systems (an agent reads a stale assignment, two writers collide
on a writ, the autarch sees the wrong status). ZFC eliminates that class of
bugs by making fresh reads the only path for the data agents coordinate on.

**Scope:** ZFC applies to coordination data — writs, agent state, tether
contents, merge requests, escalations. Per-component caches that are
**reconstructible from a source of truth** are exempt: the ledger's
in-memory `sessionKey → history_id` map (rebuilt lazily on first event per
session, see `internal/ledger/ledger.go`) and the broker's per-runtime
probe results are caches over telemetry/probe data, not coordination state,
and rebuild from durable rows on restart. The test for "is this cache OK?"
is whether a stale read of it can cause divergent decisions across agents.

**Enforcement:** Code review. No in-memory state maps for coordination data.
Store queries and tether/file reads happen at point of use, never cached
across call boundaries. Per-component caches must document the source of
truth they rebuild from.

### GUPP — Universal Propulsion Principle

> "If you find something on your tether, YOU RUN IT."

When an agent starts and discovers work on its tether, it executes
immediately. No confirmation, no polling, no waiting. The tether IS the
instruction.

**Rationale:** Idle agents are wasted capacity. A system where 30 agents
each wait for prefect acknowledgment creates a bottleneck. Tether durability
(work survives restarts) makes immediate execution safe.

**Enforcement:** `sol prime` reads the tether and injects execution context
on session start. The agent's persona instructs immediate execution.

**Adaptation for the persistent agent role (envoy):** Outposts fire on
session start — unchanged. The envoy (the only persistent agent role after
ADR-0035 and ADR-0037) fires on autarch direction via `sol writ activate`.
The trigger changes (session start vs autarch command) but the principle
holds: when directed to a writ, the agent executes immediately. No
confirmation loop, no polling. Propulsion is preserved — agents execute
immediately when directed. See ADR-0025.

### CRASH — Crash Recovery As Standard Handling

Every component has a defined crash recovery path. This is a first-class
design requirement, not optional hardening.

For each component, four questions must be answered:
1. What state survives a crash? (Durable state in SQLite/files)
2. What state is lost? (In-memory caches, pending operations)
3. How does it recover? (Re-derive from durable state on restart)
4. What is the recovery time? (Bounded, documented)

**Rationale:** Agents crash. Sessions die. Storage hiccups. Designing for
the happy path and patching failures later produces brittle systems.

**Enforcement:** Recovery matrix in `docs/failure-modes.md`. Each component
documents crash behavior. The core invariant: *an agent with work on its
tether and a local worktree needs nothing else to execute.*

See: `docs/failure-modes.md` for per-component recovery details.

### GLASS — Glass Box Operations

The autarch must be able to answer at any time, using standard tools:

1. What is each agent doing? (`tmux attach`, log tail)
2. What work is pending? (SQLite query or CLI command)
3. What failed and why? (Structured logs, error records)
4. What is the system's overall health? (`sol status`)

No component may be a black box. State must be inspectable with `sqlite3`,
`cat`, `ls`, and `jq`. Logs must be structured and greppable.

**Rationale:** Inspectability is a production requirement. Files are the
most inspectable interface humans have.

**Enforcement:** Configuration lives in files (`world.toml`, envoy auto-memory
at `<envoyDir>/memory/MEMORY.md`). Database serves as cache/registry, not sole
source of truth for autarch-facing state. ADR-0008.

### DEGRADE — Graceful Degradation

When a subsystem is down, the system continues in reduced capacity rather
than halting. Nothing is lost.

| Subsystem Down | Behavior |
|----------------|----------|
| SQLite store | Tethered agents continue. New dispatch fails. |
| Prefect | Running agents continue. No crash recovery. |
| Sentinel | Outposts work normally. While down: stalled agents undetected, no AI progress assessment, stale MR claims aren't released, failed MRs aren't recast, orphaned conflict-resolution writs aren't redispatched, idle agents aren't reaped, zombie sessions aren't cleaned up, quota rotation pauses, and orphaned worktree/tether resources accumulate. All of this resumes when the sentinel restarts. |
| Forge | Merge queue accumulates. No merges land. |
| Network/git | Agents work locally. Push retries on resolve. |

**Rationale:** The coordination layer exists to improve throughput, not to
gate execution. Agents should never be blocked by infrastructure they don't
directly need.

**Enforcement:** The tether is a local file, not a database record. Work
execution depends only on the tether and worktree — both local to the agent.

### EVOLVE — Explicit Migration Paths

Every schema, config format, and file layout must be versioned. Every version
change must have a migration path that runs automatically.

- Database schemas use a `schema_version` table
- Migrations are numbered, sequential, and idempotent
- **Forward-only:** there is no rollback. Each migration is expected to be
  idempotent so re-running is always safe (`internal/migrate/migrate.go`).
- No "delete and recreate" — autarch data is sacred

**Rationale:** The system will change. Storage formats, communication
mechanisms, and workflow patterns must be replaceable without rebuilding.
Forward-only migrations let migration authors write a single transformation
without having to design and test a reverse one — the autarch's escape hatch
for a bad migration is a backup restore, not a code-defined down-migration.

**Enforcement:** Schema migrations in `internal/store/` and discoverable
upgrade steps in `internal/migrate/migrations/`. Config files include
version fields. Pending migrations surface via `sol doctor` and the `sol up`
banner so operators see breaking changes the moment they matter.

---

## Architectural Patterns

### Deterministic Go + Targeted AI Call-outs

System processes (sentinel, consul, forge) run as deterministic Go binaries.
AI calls fire only when heuristics detect trouble — a few times per hour, not
continuously.

> Keeps costs proportional to actual problems, not routine operations.

See: ADR-0001 (sentinel), ADR-0003 (output hashing), ADR-0007 (consul),
ADR-0028 (forge — current; replaces the earlier deterministic-Go forge ADR).

### Claude Session + Go CLI Toolbox

AI-driven roles (envoy) run as persistent Claude sessions
with `sol` subcommands as their toolbox. Claude handles judgment and strategy;
Go handles mechanical operations.

> The AI decides *what* to do. The CLI does *how*.

See: ADR-0009 (envoy).

### Dual-Store: File Primary, DB as Registry

Configuration and envoy persistent memory live in files (`world.toml`,
`<envoyDir>/memory/MEMORY.md` via Claude Code auto-memory). The database
provides indexing, querying, and transactional writes for coordination state
(writs, agent records, messages).

Files are authoritative for autarch-facing state. The database is authoritative
for coordination state. Neither duplicates the other's role.

See: ADR-0008 (world lifecycle).

### Workflow-as-Directory

Workflows (TOML manifests) define work as explicit DAGs with steps,
dependencies, and execution phases. Each step is a directory entry you can
`ls` and `cat`. State tracked in `state.json`.

See: ADR-0032 (workflow type unification — current; supersedes the earlier
workflow-manifest ADR).

### Envoy Persistent Memory

Envoys maintain their own long-lived context via Claude Code's native
auto-memory at `<envoyDir>/memory/MEMORY.md`, managed through the `/memory`
REPL command and natural-language saves. Sol points Claude at this directory
through the adapter's `autoMemoryDirectory` setting, so memory persists across
sessions and survives worktree rebuilds (it lives outside the worktree).

Memory files survive crashes. Missing memory = clean start (not failure).
Stale memory = reduced context (not error).

---

## Operational Maxims

### Stability Is the Feature

> "The system that works reliably with 5 agents is more valuable than the
> system that sometimes works with 30." — manifesto

Not more commands. Not more integrations. Not more configuration. Reliability
first, features second.

### Composition over Monoliths

> "A tether attacher that attaches tethers. A session manager that manages
> sessions. Not a 2000-line monolith that does both plus workflow
> instantiation plus caravan creation." — manifesto

The dispatch operation is a sequence of atomic steps. Each step is
independently understandable, testable, and replaceable.

### All Code through Forge

Agent-produced code merges through the forge pipeline — never direct to main.
Forge applies quality gates (build, test, lint) before merge. This is the
trust boundary between autonomous agents and the shared codebase.

**Never lose work.** Work enters the merge queue because an agent completed
it. The forge lands that work on main — it does not decide whether the work
deserves to land. Gates can reject a merge attempt, but the work stays in
the queue for retry. Branches persist until their contents are on main.
It's up to the agent on the next attempt to make whatever changes are needed.
See ADR-0028 (the current forge design).

**Scope: code writs only.** Non-code writs (analysis, review, planning) produce
findings rather than code changes. They resolve by closing directly — no branch
push, no MR, no forge involvement. The forge pipeline was always scoped to code;
the writ kind system (ADR-0024) makes this scope explicit. Output directories
(`$SOL_HOME/{world}/writ-outputs/{writ-id}/`) provide the GLASS-inspectable
delivery surface for non-code writs.

### Fail Predictably

Every component has a defined failure mode. When storage is down, commands
that need storage fail fast with a clear error. Commands that don't need
storage still work. No hidden magic — behavior is traceable from inputs to
outputs.

### Pragmatism over Purity

> "If 30 concurrent agents need transactional writes, we use a database.
> Not because it's Unix, but because advisory locks on flat files at that
> concurrency level is reinventing a database badly." — manifesto

Use the right tool: SQLite for concurrent coordination state, tmux for
interactive agent sessions, single-binary transactions where atomic
multi-step execution needs rollback.

### No Trust Asymmetry

All agents — human-directed (envoy) or autonomous (outpost) — merge through
the same forge pipeline with the same quality gates. No agent bypasses review.
The system treats all code changes equally regardless of origin.

---

## Rejected Patterns

Lessons from the Gastown prototype — complexity that doesn't earn its keep.

### Universal Bus Coupling

Using a single state substrate for everything (writs, mail, agent
identity, workflows, escalations) creates deep coupling. When that layer
is unreliable, everything is unreliable. Sol uses purpose-specific schemas
within shared databases instead.

### Three-Layer Supervision

A dumb daemon spawning an ephemeral AI triage agent to monitor a persistent
AI watchdog to monitor per-world health agents to monitor workers. The
concept is sound — a hung process can't detect its own hang — but the
implementation produced real bugs at every layer boundary. Sol limits
supervision to 2–3 levels: prefect → sentinel/consul → outposts.

### 188 Commands

Feature accumulation over time. Many commands are slight variations of
others. Sol maintains a smaller, more coherent CLI surface. New commands
must justify their existence; prefer extending existing commands over
adding new ones.

---

## Conventions

### Identifiers and Naming

| Element | Format | Example |
|---------|--------|---------|
| Timestamps | RFC 3339, UTC | `2026-03-06T14:30:00Z` |
| Writ IDs | `sol-` + 16 hex chars | `sol-a1b2c3d4e5f6a7b8` |
| Session names | `sol-{world}-{agent}` | `sol-myworld-Toast` |
| Error messages | Include context | `"failed to open world database %q: %w"` |
| SQLite connections | WAL + busy timeout + FK | `journal_mode=WAL, busy_timeout=5000, foreign_keys=ON` |

### Commit Style

[Conventional Commits](https://www.conventionalcommits.org/): `type(scope): description`

Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`.
Use scope when helpful: `feat(store): add label filtering`.

### New Component Requirements

Every new component must provide:

1. **Status representation** in `sol status` (sphere overview and/or world detail)
2. **ADR** in `docs/decisions/` documenting the architectural decision
3. **CLI documentation** in `docs/cli.md` for any new commands
4. **Crash recovery path** documented in `docs/failure-modes.md`
5. **Worktree excludes** for any sol-managed paths written inside worktrees

### Configuration Paths

| Scope | Path | Format | Consumers |
|-------|------|--------|-----------|
| Global | `$SOL_HOME/sol.toml` | TOML | All commands via `config.LoadSolConfig()` |
| Per-world | `$SOL_HOME/{world}/world.toml` | TOML | All world-scoped commands via `config.LoadWorldConfig()` |
| Sphere secrets | `$SOL_HOME/.env` | dotenv | Loaded into agent sessions by `internal/envfile` (merged under per-world `.env`); validated by `sol doctor` (`env:sphere`) |
| Per-world secrets | `$SOL_HOME/{world}/.env` | dotenv | Loaded into agent sessions for that world by `internal/envfile` (overrides sphere keys); validated by `sol doctor` (`env:<world>`) |
| Envoy memory | `<envoyDir>/memory/MEMORY.md` | Markdown | Loaded by Claude Code at session start via the adapter's `autoMemoryDirectory` |

The two `.env` files are sol's exfiltration boundary for secrets — they are
never committed to a worktree (excluded via `setup.InstallExcludes`) and are
the canonical place to put API keys and other per-world credentials that
agent processes need at runtime.

---

*Cross-references: [manifesto](manifesto.md), [failure modes](failure-modes.md),
[naming glossary](naming.md), [ADRs](decisions/).*
