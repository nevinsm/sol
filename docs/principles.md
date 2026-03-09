# System Principles

Quick-reference for sol's design philosophy, architectural patterns, and
operational conventions. Drawn from the manifesto, target architecture,
failure modes specification, and ADRs.

---

## Named Principles

### ZFC — Zero Filesystem Cache

Never cache state in memory. Always derive from the authoritative source
(database, filesystem, tmux) at point of use.

> "With 30 concurrent agents mutating state, any cache is a lie waiting to
> happen. This is how Unix tools work — `ls` reads the directory fresh every
> time — and it's the right model." — manifesto

**Rationale:** Stale reads cause silent failures in concurrent systems.
ZFC eliminates an entire class of bugs by making fresh reads the only path.

**Enforcement:** Code review. No in-memory state maps for coordination data.
Store queries and file reads happen at point of use, never cached across
call boundaries.

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

**Adaptation for persistent agents:** Outposts fire on session start —
unchanged. Persistent agents (envoys, governors, senate) fire on operator
direction via `sol writ activate`. The trigger changes (session start vs
operator command) but the principle holds: when directed to a writ, the
agent executes immediately. No confirmation loop, no polling. Propulsion
is preserved — agents execute immediately when directed. See ADR-0025.

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

The operator must be able to answer at any time, using standard tools:

1. What is each agent doing? (`tmux attach`, log tail)
2. What work is pending? (SQLite query or CLI command)
3. What failed and why? (Structured logs, error records)
4. What is the system's overall health? (`sol status`)

No component may be a black box. State must be inspectable with `sqlite3`,
`cat`, `ls`, and `jq`. Logs must be structured and greppable.

**Rationale:** Inspectability is a production requirement. Files are the
most inspectable interface humans have.

**Enforcement:** Configuration lives in files (`world.toml`, `.brief/memory.md`).
Database serves as cache/registry, not sole source of truth for operator-facing
state. ADR-0008, ADR-0013.

### DEGRADE — Graceful Degradation

When a subsystem is down, the system continues in reduced capacity rather
than halting. Nothing is lost.

| Subsystem Down | Behavior |
|----------------|----------|
| SQLite store | Tethered agents continue. New dispatch fails. |
| Prefect | Running agents continue. No crash recovery. |
| Sentinel | Outposts work normally. Stalled agents undetected. |
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
- No "delete and recreate" — operator data is sacred

**Rationale:** The system will change. Storage formats, communication
mechanisms, and workflow patterns must be replaceable without rebuilding.

**Enforcement:** Schema migrations in `internal/store/`. Config files include
version fields. Rollback supported for at least one version back.

---

## Architectural Patterns

### Deterministic Go + Targeted AI Call-outs

System processes (sentinel, consul) run as deterministic Go binaries. AI
calls fire only when heuristics detect trouble — a few times per hour, not
continuously.

> Keeps costs proportional to actual problems, not routine operations.

See: ADR-0001 (sentinel), ADR-0003 (output hashing), ADR-0007 (consul).

### Claude Session + Go CLI Toolbox

AI-driven roles (forge, governor, senate) run as persistent Claude sessions
with `sol` subcommands as their toolbox. Claude handles judgment and strategy;
Go handles mechanical operations.

> The AI decides *what* to do. The CLI does *how*.

See: ADR-0005 (forge), ADR-0010 (governor), ADR-0011 (senate).

### Dual-Store: File Primary, DB as Registry

Configuration and agent context live in files (`world.toml`, `.brief/memory.md`).
The database provides indexing, querying, and transactional writes for
coordination state (writs, agent records, messages).

Files are authoritative for operator-facing state. The database is authoritative
for coordination state. Neither duplicates the other's role.

See: ADR-0008 (world lifecycle), ADR-0013 (brief system).

### Workflow-as-Directory

Workflows (TOML manifests) define work as explicit DAGs with steps,
dependencies, and execution phases. Each step is a directory entry you can
`ls` and `cat`. State tracked in `state.json`.

See: ADR-0015 (workflow manifest), ADR-0017 (workflow-based forge).

### Agent-Maintained Brief

Agents maintain their own context in `.brief/memory.md`. Three-layer size
management: CLAUDE.md guidance, agent self-pruning, injection truncation
(hard cap at 200 lines). Zero AI overhead — no automated summarization.

Brief files survive crashes. Missing brief = clean start (not failure).
Stale brief = reduced context (not error).

See: ADR-0013 (brief system).

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

| Scope | Path | Format |
|-------|------|--------|
| Global | `$SOL_HOME/sol.toml` | TOML |
| Per-world | `$SOL_HOME/{world}/world.toml` | TOML |
| Agent brief | `.brief/memory.md` | Markdown |
| World summary | `.brief/world-summary.md` | Markdown |

---

*Cross-references: [manifesto](manifesto.md), [failure modes](failure-modes.md),
[naming glossary](naming.md), [arc roadmap](arc-roadmap.md),
[ADRs](decisions/).*
