# Target Architecture: Multi-Agent Orchestration System

> Informed by Gastown as a requirements document, Unix philosophy as a
> stability guide, and production-readiness as the primary constraint.

**Decision records:** Architectural decisions that diverge from this
document are recorded in [`docs/decisions/`](decisions/) using lightweight
ADR format.

---

## 1. Design Principles (Inherited and New)

### Inherited from Gastown

#### ZFC — Zero Filesystem Cache: **KEEP AS-IS**

Agents must not maintain in-memory caches of system state. State is always
derived from the authoritative source (database, filesystem, tmux) at the
point of use, never assumed from prior reads.

This is the most important principle in the system. With 30 concurrent agents
mutating state, stale reads cause silent failures. ZFC is already the Unix
philosophy applied to state management — `ls` reads the directory fresh,
`ps` reads `/proc` fresh. No change needed.

#### GUPP — Universal Propulsion Principle: **KEEP AS-IS**

> "If you find something on your tether, YOU RUN IT."

When an agent starts a session and discovers work on its tether, it must begin
execution immediately. No confirmation, no waiting. The tether IS the assignment.

This principle is essential for throughput. A system with 30 agents where each
waits for a prefect acknowledgment before starting work creates a
bottleneck at the prefect. GUPP eliminates that bottleneck. The tether
durability primitive (work survives session restarts) makes this safe.

#### Beads-as-Bus — Universal State Substrate: **EVOLVE**

**Core idea to keep:** All structured state — work items, agent identity,
mail, workflows — flows through a single storage substrate with consistent
querying and labeling semantics. One consistency model, one backup strategy,
one audit trail.

**What changes:** The substrate is no longer Dolt/beads. It becomes SQLite
(see Section 3). The principle that "everything is an issue" was a useful
simplification in Gastown but created unnecessary coupling. The new system
uses purpose-specific schemas within a shared database — work items, messages,
and agent records are distinct tables with proper types, not overloaded issue
records discriminated by labels.

The new name: **single-source-of-truth storage** — all mutable coordination
state lives in the store, accessed through a single interface.

#### MEOW — Molecular Expression of Work: **EVOLVE**

**Core idea to keep:** Large goals must be broken into trackable, atomic units
that agents can execute autonomously. Multi-step workflows need explicit
state tracking so that progress survives session restarts and handoffs.

**What changes:** The molecule/formula system is simplified. Formulas become
directories of markdown step files with a TOML manifest. Molecules become
directory trees with a `state.json` tracking progress. The cook/wisp/bond
pipeline is eliminated — instantiation creates a directory from a template.
The propulsion loop becomes: read `state.json` → execute step → advance
`state.json`.

The new name: **workflow-as-directory** — a workflow is a directory you can
`ls`, `cat`, and `jq`.

#### NDI — Nondeterministic Idempotence: **DROP**

This principle appeared only in documentation with no code-level enforcement.
Its aspiration (eventual completion despite individual failures) is better
expressed as concrete operational requirements: retry policies, checkpoint
recovery, and supervision. "Nondeterministic idempotence" is too abstract to
guide implementation decisions.

What we keep from the intent: specific, testable recovery behaviors defined
per component (see Section 3).

### New Principles

#### CRASH — Crash Recovery As Standard Handling

Every component must have a defined crash recovery path. This is not optional
hardening — it is a first-class design requirement.

For each component:
- **What state survives a crash?** (Durable state in SQLite/files.)
- **What state is lost?** (In-memory caches, pending operations.)
- **How does the component recover?** (Re-derive from durable state on restart.)
- **What is the recovery time?** (Bounded, documented.)

Crash recovery is tested, not assumed. Each build loop includes crash recovery
acceptance tests.

**Recovery matrix** (per-component crash behavior at a glance):

| Component | State Survives | State Lost | Recovery Action | Recovery Time |
|-----------|---------------|------------|-----------------|---------------|
| Store (3.1) | DB file (WAL journal) | Open transactions | Reopen DB (WAL recovery) | <1s |
| Session Mgr (3.2) | `.runtime/sessions/*.json` | tmux server memory | Prefect restarts sessions | <3 min |
| Mail (3.3) | `messages` table | In-flight INSERT | Re-derive from DB | <1s |
| Nudge Queue (3.4) | Pending nudge files | Claimed (in-delivery) nudges | Re-derive from pending messages | <1s |
| Workflow Engine (3.5) | `state.json`, step files | In-memory step state | Re-read state.json on restart | <1s |
| Prefect (3.6) | PID file, session registry | Heartbeat loop state | Restart prefect (systemd/launchd) | <10s |
| Consul (3.7) | Heartbeat file | Patrol cycle state | Prefect restarts, re-patrols | <3 min |
| Sentinel (3.8) | Patrol state file | Current patrol cycle | Prefect restarts, re-patrols | <3 min |
| Forge (3.9) | `merge_requests` table, slot lock | In-progress merge | Prefect restarts Claude session; TTL expiry releases claimed MR | <30 min |
| Outpost (3.10) | Tether file, worktree, identity | Session memory | `sol prime` re-injects context (GUPP) | <30s |
| Event Feed (3.11) | JSONL files | Chronicle buffer | Chronicle restarts, tails from last position | <10s |

#### GLASS — Glass Box Operations

The operator must be able to answer these questions at any time, using standard
tools:

1. **What is each agent doing right now?** (tmux attach, log tail)
2. **What work is pending?** (SQLite query or CLI command)
3. **What failed and why?** (Structured logs, error records in store)
4. **What is the system's overall health?** (Single status command)

No component may be a black box. State must be inspectable with `sqlite3`,
`cat`, `ls`, and `jq`. Logs must be structured and greppable.

#### DEGRADE — Graceful Degradation

When a subsystem is down, the system must continue operating in a reduced
capacity rather than halting entirely.

| Subsystem Down | System Behavior |
|----------------|-----------------|
| SQLite store | Agents with tethered work continue executing (tether is a local file). New dispatch fails. Pending messages unavailable. |
| Prefect | Running agents continue. No crash recovery or new spawns. |
| Sentinel | Outposts work normally. Completed work waits in queue. |
| Forge | Work accumulates in merge queue. No merges land. Go fallback (`sol forge run`) available without Claude API. |
| Network/git remote | Agents work locally. `sol resolve` push phase retries. |

The key insight: **an agent with work on its tether and a local worktree needs
nothing else to execute**. The entire coordination layer can be down and
in-flight work continues. Recovery happens when services return.

#### EVOLVE — Explicit Migration Paths

Every schema, config format, and file layout must be versioned. Every version
change must have a migration path that runs automatically on startup.

- Database schemas include a `schema_version` table.
- Config files include a `version` field.
- Migrations are numbered, sequential, and idempotent.
- Rollback is supported for at least one version back.

No "just delete and recreate" — operator data is sacred.

---

## 2. Problem Decomposition

### 2.1 Work Tracking (Essential)

**Problem:** The system needs to know what work exists, its status, priority,
dependencies, and assignment.

**Hard constraints:**
- 30+ concurrent readers and writers
- Transactional status transitions (tethered → working → resolve must not lose state)
- Label/priority-based queries for dispatch decisions
- Dependency graph traversal for caravan readiness

**Existing tools that solve this well:** SQLite (concurrent reads via WAL,
serialized writes, SQL queries, single-file deployment). The query patterns
from Gastown's behavioral spec — label intersection, priority sorting, assignee
filtering, dependency graph — are all natural SQL.

### 2.2 Work Dispatch (Essential)

**Problem:** Assigning work to agents — selecting the right agent, preparing
the execution context, and starting the session.

**Hard constraints:**
- Must be serialized per-work-item (prevent double-assignment)
- Must be atomic: if session start fails, the work item returns to open status
- Must support batch dispatch (10+ work items at once)
- Latency budget: <5s per single dispatch, <30s for batch of 10

**Existing tools:** Per-work-item flock (advisory file lock) for serialization.
The dispatch operation is inherently sequential per work item — validate, assign,
prepare context, start session. Composition of smaller operations.

### 2.3 Agent Lifecycle (Essential)

**Problem:** Starting, monitoring, and stopping AI agent sessions. Detecting
crashes and triggering recovery.

**Hard constraints:**
- Agents run as long-lived processes (minutes to hours)
- Operator needs interactive attachment for debugging
- Health checks need to distinguish: process alive, agent responsive, agent making progress
- Crash detection latency: <3 minutes

**Existing tools:** Tmux (process container with attachment, injection,
capture). PID files for liveness. Heartbeat files for responsiveness.

### 2.4 Agent Identity (Essential)

**Problem:** Persistent records that survive session restarts — work history,
skill tracking, cost accounting.

**Hard constraints:**
- Identity must survive session crashes, restarts, and handoffs
- Work history is append-only (never rewritten)
- Must support 50+ identities per world (name pool)
- Queries: find idle agents, find agent by name, get agent history

**Existing tools:** SQLite table for identity records. Agent directories on
the filesystem for local state.

### 2.5 Work Execution Context (Essential)

**Problem:** When an agent starts (or restarts), it needs to know what to do —
what's on its tether, what step it's on, what the instructions say.

**Hard constraints:**
- Must work after session crash (derive from durable state, not memory)
- Must be fast (<2s to assemble context)
- Must include: tethered work, workflow step, pending messages, checkpoint state

**Existing tools:** A `prime` command that reads tether file, workflow state,
and message inbox, then outputs structured context to stdout. This is the
right pattern — a read-only operation that assembles context from durable
state.

### 2.6 Session Management (Essential)

**Problem:** Process containers for AI agents — isolated environments with
controlled stdin/stdout, environment variables, and working directories.

**Hard constraints:**
- Must support interactive attachment (operator debugging)
- Must support text injection (nudges)
- Must support output capture (health checks)
- Must handle 30+ concurrent sessions

**Existing tools:** Tmux. Despite its quirks, tmux provides all four
requirements. The alternatives (process groups + FIFOs, systemd user units)
each lack at least one.

### 2.7 Inter-Agent Communication (Essential)

**Problem:** Agents need to exchange messages — protocol messages (MERGE_READY,
OUTPOST_DONE), nudges, and freeform mail.

**Hard constraints:**
- Must be durable (messages survive sender/receiver crashes)
- Must be inspectable (operator can read pending messages)
- At-most-once delivery for nudges (prevent duplicate execution)
- Must support: direct, broadcast, and queue routing

**Existing tools:** SQLite `messages` table for durable messages (same WAL-mode
database as all other coordination state). File-based nudge queue for tmux
injection (ephemeral delivery notifications, not the messages themselves).

### 2.8 Merge/Integration (Essential)

**Problem:** Getting completed work from agent worktrees into the shared
codebase — rebasing, running tests, handling conflicts, merging.

**Hard constraints:**
- Serialized merges (one at a time per world to prevent conflict explosion)
- Quality gates (tests, build, lint) must pass before merge
- Conflict detection with recovery path
- Must not merge broken code to main

**Existing tools:** Git for the merge operations themselves. File-based lock
for merge slot serialization. The merge queue processor is a long-running
agent that polls for ready work.

### 2.9 Supervision (Essential)

**Problem:** Detecting when agents crash or hang, and recovering them.

**Hard constraints:**
- Detection latency: <3 minutes for crashes, <15 minutes for hangs
- Must not cascade (restarting one agent must not destabilize others)
- Must detect mass failures and enter degraded mode
- Must distinguish: session dead, agent process dead, agent hung

**Existing tools:** A prefect process that checks PID/session liveness on
a heartbeat. Tmux session existence for crash detection. Heartbeat files for
hang detection. Mass-death throttle (3+ deaths in 30s → pause respawns).

### 2.10 Observability (Enhancing)

**Problem:** Knowing what the system is doing — activity feeds, structured
logs, health dashboards.

**Hard constraints:**
- Events must not block primary operations (best-effort logging)
- Must support `tail -f` workflow for real-time monitoring
- Must handle high event volume (30+ agents generating events)

**Existing tools:** Append-only JSONL files with cross-process flock.
Chronicle process for dedup/aggregation. Standard Unix tools (`tail`, `jq`,
`grep`) for consumption.

### 2.11 Workflow Orchestration (Enhancing)

**Problem:** Multi-step coordinated work — a sequence of instructions that
an agent follows, with progress tracking and step transitions.

**Hard constraints:**
- Must survive session restarts (step state is durable)
- Must support step dependencies (DAG, not just linear)
- Must support variable substitution in step instructions
- Ephemeral workflows (patrol loops) must not accumulate state

**Existing tools:** Directory-based workflows. TOML manifests for templates.
JSON state files for progress. Topological sort for dependency resolution.

---

## 3. Architecture Overview

### Component Diagram

```
                    ┌─────────────────────────────────────┐
                    │           Human Operator             │
                    │   (sol CLI, tmux attach, sqlite3)     │
                    └────────────┬────────────────────────┘
                                 │
                    ┌────────────┴────────────────────────┐
                    │            sol CLI                     │
                    │   (porcelain: cast, done, up,       │
                    │    down, status, mail, caravan)        │
                    └────────────┬────────────────────────┘
                                 │
          ┌──────────┬───────────┼───────────┬──────────────┐
          │          │           │           │              │
          ▼          ▼           ▼           ▼              ▼
    ┌──────────┐ ┌────────┐ ┌────────┐ ┌─────────┐  ┌──────────┐
    │  Store   │ │ Session│ │  Mail  │ │Workflow │  │  Events  │
    │ (SQLite) │ │ (tmux) │ │(SQLite)│ │ (dirs)  │  │ (JSONL)  │
    └────┬─────┘ └───┬────┘ └───┬────┘ └────┬────┘  └────┬─────┘
         │           │          │           │             │
         └─────┬─────┴──────┬──┴───────────┴─────────────┘
               │            │
               ▼            ▼
    ┌──────────────┐  ┌──────────────┐
    │  Prefect   │  │   Consul     │
    │  (Go process) │  │  (AI agent)  │
    └──────┬───────┘  └──────┬───────┘
           │                 │
     ┌─────┼─────┐     ┌────┼────┐
     │     │     │     │    │    │
     ▼     ▼     ▼     ▼    ▼    ▼
   Wit.  Ref.  Wit.  Dogs  Stale  Stranded
  (Go+AI)(Go) (Go+AI)      tethers  caravans
  (rig1)(rig1)(rig2)
     │
     ├── Outpost sessions (×M per world)
     └── Crew sessions (×K per world, human-managed)
```

### 3.1 Store (SQLite)

**What it does:** Provides the single source of truth for all coordination
state — work items, agent identities, merge requests, caravans, and
escalations. Replaces Dolt/beads with SQLite in WAL mode.

**Dependencies:** None (SQLite is a library, not a server).

**What depends on it:** Everything that reads or writes coordination state —
the CLI, prefect, all agent types.

**Failure mode:** If the database file is corrupted or locked, operations
that require coordination state fail. Agents with tethered work continue
executing (DEGRADE principle). The store is a file, so corruption recovery
is `cp backup.db store.db`.

**State:** One SQLite database per world, plus one sphere-level database.
- Sphere DB: `~/sol/.store/sphere.db` — agent identities, mail, caravans, escalations
- World DBs: `~/sol/.store/{world}.db` — work items, merge requests

**Schema:**

```sql
-- sphere.db
CREATE TABLE agents (
    id          TEXT PRIMARY KEY,    -- e.g., "myworld/Toast"
    name        TEXT NOT NULL,       -- e.g., "Toast"
    world         TEXT NOT NULL,       -- e.g., "myworld"
    role        TEXT NOT NULL,       -- outpost|sentinel|forge|crew
    state       TEXT NOT NULL DEFAULT 'idle',  -- idle|working|stalled|stuck|zombie
    hook_item   TEXT,                -- currently tethered work item ID
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE agent_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id    TEXT NOT NULL REFERENCES agents(id),
    work_item_id TEXT NOT NULL,
    action      TEXT NOT NULL,       -- assigned|completed|escalated|deferred
    started_at  TEXT NOT NULL,
    ended_at    TEXT,
    summary     TEXT,
    cost_usd    REAL
);

CREATE TABLE messages (
    id          TEXT PRIMARY KEY,
    sender      TEXT NOT NULL,
    recipient   TEXT NOT NULL,
    subject     TEXT NOT NULL,
    body        TEXT,
    priority    INTEGER NOT NULL DEFAULT 2,
    type        TEXT NOT NULL DEFAULT 'notification',
    thread_id   TEXT,
    delivery    TEXT NOT NULL DEFAULT 'pending',  -- pending|acked
    read        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    acked_at    TEXT
);
CREATE INDEX idx_messages_recipient ON messages(recipient, delivery);
CREATE INDEX idx_messages_thread ON messages(thread_id);

CREATE TABLE caravans (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open',
    owner       TEXT,
    created_at  TEXT NOT NULL,
    closed_at   TEXT
);

CREATE TABLE convoy_items (
    convoy_id    TEXT NOT NULL REFERENCES caravans(id),
    work_item_id TEXT NOT NULL,
    world          TEXT NOT NULL,
    PRIMARY KEY (convoy_id, work_item_id)
);

CREATE TABLE escalations (
    id          TEXT PRIMARY KEY,
    severity    TEXT NOT NULL,       -- low|medium|high|critical
    source      TEXT NOT NULL,
    description TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open',
    acknowledged INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE schema_version (version INTEGER NOT NULL);

-- {world}.db
CREATE TABLE work_items (
    id          TEXT PRIMARY KEY,    -- e.g., "sol-abc"
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'open',
    priority    INTEGER NOT NULL DEFAULT 2,
    assignee    TEXT,
    parent_id   TEXT,
    created_by  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    closed_at   TEXT
);
CREATE INDEX idx_work_status ON work_items(status);
CREATE INDEX idx_work_assignee ON work_items(assignee);
CREATE INDEX idx_work_priority ON work_items(priority);

CREATE TABLE labels (
    work_item_id TEXT NOT NULL REFERENCES work_items(id),
    label        TEXT NOT NULL,
    PRIMARY KEY (work_item_id, label)
);
CREATE INDEX idx_labels_label ON labels(label);

CREATE TABLE dependencies (
    from_id     TEXT NOT NULL REFERENCES work_items(id),
    to_id       TEXT NOT NULL REFERENCES work_items(id),
    PRIMARY KEY (from_id, to_id)
);

CREATE TABLE merge_requests (
    id          TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL,
    branch      TEXT NOT NULL,
    phase       TEXT NOT NULL DEFAULT 'ready',
    claimed_by  TEXT,
    claimed_at  TEXT,
    priority    INTEGER NOT NULL DEFAULT 2,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    merged_at   TEXT
);
CREATE INDEX idx_mr_phase ON merge_requests(phase);

CREATE TABLE schema_version (version INTEGER NOT NULL);
```

**Interface:** A `sol store` CLI (plumbing) wrapping SQLite operations:

```
sol store create --world=<world> --title="..." --priority=2 --label=sol:task
sol store get <id>
sol store list --world=<world> --status=open --label=sol:task --assignee=<agent> --json
sol store update <id> --status=tethered --assignee=<agent>
sol store close <id>
sol store query --world=<world> --sql="SELECT ..." (escape hatch for complex queries)
```

**Concurrency handling:** SQLite WAL mode supports concurrent readers. Writers
are serialized by SQLite's built-in locking with `PRAGMA busy_timeout=5000`
(5s retry). For the actual workload (30 agents, mostly non-overlapping writes
to different work items), this is sufficient. Write contention happens only
when multiple agents try to claim the same work item — handled by per-work-item
flock before the SQLite write.

**Why not Dolt:** Dolt is a server process (single point of failure), requires
MySQL protocol overhead for simple reads, and its git-native versioning
(while elegant) adds complexity that isn't justified for the production use
case. SQLite is a library — no server, no port, no connection management.
The database is a single file copyable with `cp`.

**Why not plain files:** 30 concurrent agents writing labeled, prioritized,
dependency-linked work items is a database workload. Plain files with symlink
indexes and advisory locks would be reinventing SQLite badly. Label
intersection queries (`status=open AND label=sol:task AND priority < 2`) are
one SQL statement but would require directory traversal and set intersection
with files.

### 3.2 Session Manager (tmux)

**What it does:** Provides process containers for AI agents — creating
sessions, injecting text, capturing output, checking health, and enabling
interactive attachment.

**Dependencies:** tmux (>= 3.2).

**What depends on it:** All agent types (outposts, sentinels, forges,
crew). The prefect for liveness checks. The nudge system for message
injection.

**Failure mode:** If tmux server crashes, all sessions die. The prefect
detects this (all PID checks fail simultaneously) and enters degraded mode.
Recovery: prefect restarts tmux server, then restarts agents. Tethered work
is durable — agents recover via GUPP on restart.

**State:** Tmux server state (in-memory, volatile). Session metadata stored
as tmux environment variables and in `.runtime/sessions/{name}.json` files.

**Interface:**

```
sol session start <name> --workdir=<dir> --cmd=<command> --env=KEY=VAL
sol session stop <name> [--force]
sol session list [--json]
sol session health <name> [--max-inactivity=30m]
    → exit 0: healthy, 1: dead, 2: agent-dead, 3: hung
sol session capture <name> --lines=N
sol session attach <name>
sol session inject <name> --message=<text>  (low-level nudge)
```

**Why tmux over alternatives:**

| Requirement | tmux | Process groups + FIFOs | systemd user units |
|-------------|------|------------------------|-------------------|
| Process spawn | Yes | Yes | Yes |
| Text injection | send-keys | Write to FIFO | No |
| Output capture | capture-pane | tail log file | journalctl |
| Interactive attach | tmux attach | No | No |
| 30+ concurrent | Yes | Yes | Yes |

Interactive attachment is non-negotiable for debugging AI agents. When an
agent behaves unexpectedly, the operator needs to see its live session, read
its context, and inject corrections. Only tmux provides this.

### 3.3 Mail System (SQLite-backed)

**What it does:** Provides durable inter-agent messaging with routing,
delivery tracking, and priority ordering.

**Dependencies:** Store (SQLite `messages` table in `sphere.db`), nudge queue
(for delivery notifications).

**What depends on it:** All agent communication — protocol messages
(MERGE_READY, OUTPOST_DONE), nudges, escalations, human-agent messages.

**Failure mode:** Same as store — if SQLite is unavailable, message writes
fail. Agents with tethered work continue executing (DEGRADE principle). Pending
messages are also unavailable when the store is down.

**State:** The `messages` table in `sphere.db` (schema in Section 3.1). This is
the same WAL-mode SQLite database used for all other coordination state. One
source of truth, one consistency model, one backup strategy.

**Interface:**

```
sol mail send --to=<recipient> --subject=<text> --body=<text> [--priority=N]
sol mail inbox [--identity=<addr>] [--json]
sol mail read <message-id>
sol mail ack <message-id>
sol mail check [--identity=<addr>]   (count unread)
```

**Delivery:** On send, a row is inserted into the `messages` table with
`delivery='pending'`. If the recipient is idle (tmux prompt detected), a
nudge is injected immediately via the nudge queue (Section 3.4). If busy,
a nudge file is queued for turn-boundary delivery. The nudge is the
*notification* that a message exists — the message itself lives in SQLite.

**Concurrency note:** Messages use the same WAL-mode SQLite as everything
else. With `PRAGMA busy_timeout=5000`, concurrent message writes from 30
agents are serialized by SQLite's built-in locking. The actual write
contention is low — each write is a single INSERT, taking microseconds.
The nudge queue (Section 3.4) handles delivery notification separately
as a filesystem operation, so nudge delivery is never blocked by a
database write.

**Protocol messages:** MERGE_READY, OUTPOST_DONE, MERGED, MERGE_FAILED,
REWORK_REQUEST, CARAVAN_NEEDS_FEEDING, RECOVERED_BEAD, RECOVERY_NEEDED,
HANDOFF, HELP. These use the same `messages` table with the `type` field
set to `'protocol'` and structured JSON in the body. Receivers query by
`type='protocol'` and parse the subject prefix.

### 3.4 Nudge Queue

**What it does:** Non-destructive message delivery to AI agents at turn
boundaries. When an agent is busy (mid-tool-call), nudges queue until the
agent reaches a natural pause point.

**Dependencies:** Filesystem, tmux (for injection).

**What depends on it:** Mail system (delivery notifications), prefect
(health pings), human operator (direct nudges).

**Failure mode:** If nudge delivery fails, the nudge file remains in the
queue and is retried next turn boundary. Expired nudges are silently
discarded.

**State:** File-based FIFO queues (carried forward from Gastown — this design
is sound):

```
~/sol/.runtime/nudge_queue/{session}/
├── {nanosecond-timestamp}-{hex}.json     Pending nudge
├── {nanosecond-timestamp}-{hex}.claimed  Being delivered
```

**Interface:**

```
sol nudge <target> [message]
sol nudge --session=<name> --message=<text> [--priority=normal|urgent]
```

**Delivery sequence:** Unchanged from Gastown — acquire per-session lock,
find agent pane, send text in literal mode, wait 500ms, ESC, Enter with
retry, SIGWINCH for detached sessions. The Claude Code `UserPromptSubmit`
tether drains the queue at turn boundaries.

**Invariants:** Max queue depth 50 per session. FIFO within priority. TTL:
normal=30min, urgent=2hr. Atomic claim via file rename.

### 3.5 Workflow Engine (Directory-based)

**What it does:** Manages multi-step workflows — templates (formulas),
instances (molecules), step tracking, and progress state.

**Dependencies:** Filesystem, store (for linking workflows to work items).

**What depends on it:** Work dispatch (instantiating workflows on assignment),
agents (reading current step), `sol resolve` (advancing/completing workflows).

**Failure mode:** If a workflow state file is corrupted, the agent loses its
place. Recovery: the operator or prefect re-reads the step directory to
reconstruct state. Each step's completion is idempotent — re-running a
completed step is safe.

**State:**

```
~/sol/formulas/                          Templates (checked into repo)
├── default-work/
│   ├── manifest.toml                   Metadata: name, type, variables
│   └── steps/
│       ├── 01-load-context.md          Step instructions
│       ├── 02-implement.md
│       └── 03-verify.md
└── sentinel-patrol/
    ├── manifest.toml
    └── steps/
        └── 01-patrol.md

~/sol/{world}/outposts/{name}/.workflow/   Active workflow instance
├── manifest.json                       {formula, work_item_id, vars}
├── state.json                          {current_step, completed: [], status}
└── steps/
    ├── 01-load-context.json            {id, title, status, started_at}
    ├── 02-implement.json
    └── 03-verify.json
```

**Manifest TOML format:**

```toml
name = "default-work"
type = "workflow"            # workflow|caravan|expansion
description = "Standard outpost work execution"

[variables]
issue = { required = true }
base_branch = { default = "main" }

[[steps]]
id = "load-context"
title = "Load work context"
instructions = "steps/01-load-context.md"

[[steps]]
id = "implement"
title = "Implement the change"
instructions = "steps/02-implement.md"
needs = ["load-context"]

[[steps]]
id = "verify"
title = "Verify the implementation"
instructions = "steps/03-verify.md"
needs = ["implement"]
```

**Interface:**

```
sol workflow instantiate <formula-dir> --item=<id> --var=key=val
    → creates .workflow/ directory with state.json and step files
sol workflow current [--agent=<name>]
    → outputs current step instructions to stdout
sol workflow advance [--agent=<name>]
    → marks current step complete, finds next ready step, updates state.json
sol workflow status [--agent=<name>]
    → outputs progress summary
```

**Propulsion loop (what agents execute):**
```
1. sol workflow current     → read step instructions
2. Execute step
3. sol workflow advance     → close step, advance to next
4. If status != "done": GOTO 1
```

**Ephemeral workflows:** For patrol loops (sentinel, consul), the workflow
directory is created in a temporary location and deleted when the patrol
cycle completes. State is not preserved across cycles (same as Gastown's
wisps, but using filesystem semantics).

### 3.6 Prefect

**What it does:** A Go process that monitors agent session liveness, restarts
crashed sessions, and provides the heartbeat loop. Replaces the three-layer
daemon → boot → consul chain.

**Dependencies:** Tmux (for session checks), filesystem (PID files, heartbeat
files).

**What depends on it:** All agent sessions (for crash recovery). The operator
(for system startup/shutdown).

**Failure mode:** If the prefect crashes, running agents continue
unaffected. No new agents are spawned, and crashed agents are not recovered.
The operator must restart the prefect. A system-level prefect (systemd
or launchd) can restart the prefect process itself.

**State:**

```
~/sol/.runtime/
├── prefect.pid              Prefect process PID
├── prefect.log              Structured JSON log
├── sessions/
│   └── {session-name}.json     {pid, started_at, role, world}
└── heartbeats/
    └── {session-name}.json     {timestamp, cycle, status}
```

**Design — why two layers instead of three:**

Gastown's three-layer chain (daemon → boot → consul) was designed to solve the
problem that a Go process can't reason about agent behavior and an AI agent
can't detect its own hang. In practice, this produced session-confusion bugs
and over-engineering.

The new design uses two layers:

1. **Prefect (Go process):** Handles what code can handle — process
   liveness, session existence, heartbeat freshness, mass-death detection,
   respawn with backoff. This replaces both daemon and boot.

2. **Consul (AI agent):** Handles what requires judgment — stale tether
   recovery, stranded caravan feeding, quality assessment of stuck agents.
   Monitored by the prefect via heartbeat, just like any other agent.

The boot agent is eliminated. Its triage logic ("is the consul's heartbeat
stale? should I nudge or wake?") becomes a simple function in the prefect:

```go
func triageDeacon(heartbeat time.Time) action {
    age := time.Since(heartbeat)
    switch {
    case age < 5*time.Minute:
        return doNothing
    case age < 15*time.Minute:
        return nudgeDeacon
    default:
        return restartDeacon
    }
}
```

**Heartbeat loop (every 3 minutes):**
1. Check prefect PID file (single-instance guard)
2. For each registered session:
   a. Check tmux session exists
   b. If missing: respawn (with backoff — 30s, 1m, 2m, 5m)
   c. If exists: check heartbeat freshness
   d. If stale (>15 min): restart session
3. Mass-death detection: 3+ deaths in 30s → enter degraded mode (log only,
   no respawns, notify operator)
4. Consul triage (see above)

**Interface:**

The prefect is sphere-level — one instance monitors all worlds. It reads
all agents from `sphere.db` and checks sessions across every world.

```
sol prefect run       (foreground, writes PID file, blocks until interrupted)
sol prefect stop      (sends SIGTERM to running prefect)
sol status <world>         (show agents, sessions, tethered work, prefect health)
```

Daemon mode (`sol prefect start` as a background process) is deferred.
`sol prefect logs` is deferred — the operator can `tail -f` the log
file directly.

### 3.7 Consul (AI Agent)

**What it does:** Sphere-level AI agent that handles tasks requiring judgment —
stale tether recovery, stranded caravan feeding, cross-world coordination. Narrower
scope than Gastown's consul because process liveness is handled by the
prefect.

**Dependencies:** Store, mail, session manager.

**What depends on it:** Caravan dispatch, stale tether recovery, dog management.

**Failure mode:** If the consul crashes, the prefect restarts it. While
down: stale tethers accumulate (resolved on restart), caravans with ready work
wait (dispatched on restart). No data loss.

**State:** Heartbeat file (`~/sol/consul/heartbeat.json`), patrol state.

**Patrol cycle:**
1. Write heartbeat
2. Check for stale tethers (work items with `status=tethered` and no active session for the assignee). Unhook and return to open.
3. Check for stranded caravans (caravans with closed items that have unstarted dependent work). Dispatch ready items.
4. Process lifecycle requests (cycle, restart, shutdown from operator mail).
5. Run health checks on agents with low activity (complement prefect's process-level checks with behavior-level assessment).

### 3.8 Sentinel (Go Process with AI Call-outs, Per-World)

**What it does:** Per-world health monitor that patrols outposts, detects
stalled and zombie sessions, triggers recovery, and uses targeted AI
assessment to evaluate potentially stuck agents. The sentinel is primarily a
Go process for speed and determinism — the patrol loop, state detection,
respawn logic, and zombie cleanup are all deterministic code. AI is used
only for judgment calls: when the heuristic detects no progress, the sentinel
shells out to `claude -p` for a one-off assessment of the agent's session
output.

**Dependencies:** Store, mail, session manager, tmux, `claude` CLI (for
AI-assisted assessment).

**What depends on it:** Outposts (for crash recovery, stuck detection).

**Failure mode:** If the sentinel crashes, the prefect restarts it. While
down: crashed outposts are not respawned at the work level (prefect
handles session restarts, but sentinel handles work-level recovery like
returning work to the open pool after max respawns). In-memory state
(respawn counts, output hashes) is lost on crash and re-derived on restart.
No data loss.

**State:** In-memory respawn counts and tmux output hashes (lost on crash,
re-derived). Agent record in sphere.db (`{world}/sentinel`).

**Patrol cycle:**
1. List outpost agents in world (filter by role=outpost)
2. For each outpost:
   a. Check tmux session liveness
   b. If session dead + work tethered → mark stalled, attempt respawn (max 2),
      then return work to open if max exceeded
   c. If session alive + working: capture tmux output, hash it, compare with
      previous patrol's hash. If unchanged → trigger AI assessment via
      `claude -p`. Assessment determines: progressing (no action), stuck
      (nudge with specific guidance), or stuck beyond help (escalate to
      operator). Low-confidence assessments are ignored.
   d. If idle + session alive + no tether → zombie, clean up session
3. Emit patrol summary event (healthy/stalled/zombie/assessed/nudged counts)

**AI assessment details:** The sentinel captures the last ~80 lines of tmux
output and sends it to `claude -p` with a structured prompt requesting JSON
assessment. The assessment command is configurable (`AssessCommand` in
sentinel config) for operators who want to use a different model or tool.
Assessment failure (timeout, parse error, AI unavailable) is non-blocking —
the sentinel logs a warning and continues its patrol. This keeps AI costs low
(calls only when heuristic triggers) while catching stuck agents that a pure
heuristic would miss.

### 3.9 Forge (Claude Session + Go Toolbox, Per-World)

> **See [ADR-0005](decisions/0005-forge-claude-session.md)** — supersedes
> the original pure Go design (ADR-0002).

**What it does:** Merge queue processor that claims, validates, and merges
completed work into the target branch. Claude runs the patrol loop and
makes judgment calls (conflict resolution, test failure attribution). Go
CLI subcommands provide the mechanical toolbox.

**Dependencies:** Store, mail, git, session manager, Claude API (degraded
fallback via `sol forge run` without API).

**What depends on it:** Nothing directly — it is a terminal consumer of
the merge pipeline.

**Failure mode:** If the forge session crashes, the prefect restarts
it. Claimed merge requests with expired TTL (30 min) are automatically
released for re-claim. No merges land while down; the queue accumulates.

**State:** Merge queue in world SQLite DB (`merge_requests` table, including
`blocked_by` column for conflict-resolution tracking). Merge slot lock
file (`~/sol/{world}/forge/.merge-slot.lock`).

**Claude handles:**
- The patrol loop (scan queue, claim, rebase, test, push, repeat)
- Rebase execution (where conflicts surface)
- Conflict judgment: trivial (resolve directly) vs complex (delegate to
  outpost via `sol forge create-resolution`)
- Test failure attribution (branch vs pre-existing)
- Wait/retry decisions

**Go CLI subcommands:**
- `sol forge ready/blocked/claim/release` — queue management
- `sol forge run-gates` — quality gate execution
- `sol forge push` — merge slot acquisition and push
- `sol forge mark-merged/mark-failed` — state updates
- `sol forge create-resolution` — conflict delegation
- `sol forge check-unblocked` — resolution tracking

**Merge pipeline:**
1. Poll for `phase=ready` merge requests, sorted by priority + age
2. Claim (set `phase=claimed`, `claimed_by`, `claimed_at`)
3. Rebase onto latest target branch
4. If trivial conflict → resolve directly
5. If complex conflict → delegate to outpost via `create-resolution`,
   set `phase=blocked`
6. Run quality gates (test, build, lint — configurable per world)
7. If gates fail → attribute failure; retry or mark failed
8. Merge to target branch → send MERGED to sentinel
9. If car-eligible → send CARAVAN_NEEDS_FEEDING to consul

### 3.10 Outpost (Worker Agent)

**What it does:** Executes work items. Each outpost has persistent identity,
a git worktree sandbox, and ephemeral tmux sessions.

**Dependencies:** Store (for identity), git (for worktree), tmux (for
session), workflow engine (for step tracking).

**What depends on it:** Sentinels (monitor outposts). Forges (receive
completed work).

**Failure mode:** Session crash → work remains tethered, worktree preserved.
On restart, `sol prime` reads tether, workflow state, and pending messages to
reconstruct context. Agent resumes from last durable state (GUPP + CRASH).

**Three-layer architecture (kept from Gastown):**

| Layer | What | Lifecycle | Storage |
|-------|------|-----------|---------|
| Identity | Agent record, work history | Permanent | SQLite `agents` + `agent_history` |
| Sandbox | Git worktree, branch | Persistent across assignments | `~/sol/{world}/outposts/{name}/world/` |
| Session | Claude in tmux | Ephemeral per step/handoff | tmux server memory |

**States:**

| State | Description | Session | Sandbox | Tether |
|-------|-------------|---------|---------|------|
| `idle` | Awaiting assignment | Dead | Preserved | Clear |
| `working` | Executing work | Alive | Exists | Set |
| `stalled` | Session crashed mid-work | Dead | Exists | Set |
| `stuck` | Explicitly requested help | Alive | Exists | Set |
| `zombie` | tmux exists but no worktree | Alive | Missing | — |

**Tether:** A file at `~/sol/{world}/outposts/{name}/.tether` containing the work
item ID. Existence of this file means work is assigned. The tether is the
durability primitive — it survives everything except explicit detachment.
On session start, `sol prime` reads `.tether`, looks up the work item in the
store, loads workflow state, and injects execution context.

### 3.11 Event Feed

**What it does:** Captures system activity as an append-only event log, with
a chronicle that produces a filtered/aggregated feed for agent consumption.

**Dependencies:** Filesystem (flock for concurrent writes).

**What depends on it:** Operator monitoring (`sol feed`). Agents (for
situational awareness, optional).

**Failure mode:** Event logging is best-effort — failures are silently
ignored. If the chronicle crashes, the raw log continues growing and the
curated feed is stale. The prefect restarts the chronicle. No primary
operations are affected.

**State:**
- Raw events: `~/sol/.events.jsonl` (append-only, flock-serialized)
- Curated feed: `~/sol/.feed.jsonl` (append-only, truncated at 10MB)

**Event format (unchanged from Gastown):**
```json
{"ts":"...","source":"sol","type":"cast","actor":"overseer","visibility":"both","payload":{...}}
```

**Chronicle:** A standalone process (`sol chronicle`) that tails the raw event
file, filters by visibility, deduplicates (10s window for done events),
aggregates (30s window for cast bursts), and appends to the curated feed.

**Interface:**
```
sol feed [--follow] [--limit=N] [--since=<time>] [--type=<type>]
sol log-event --type=<type> --actor=<actor> [--visibility=feed|audit|both]
sol chronicle start    (background process)
```

### 3.12 Agent Protocol Integration

**What it does:** Bridges the Go orchestration system and the AI agents
running inside tmux sessions. Defines how agents learn their commands,
receive their work context, and recover after crashes.

**Dependencies:** Store (for work item lookup), workflow engine (for step
state), session manager (for tether installation).

**What depends on it:** All agent types — every agent session needs protocol
integration to function.

#### CLAUDE.md Contract

Every outpost worktree gets a `.claude/CLAUDE.md` file generated by `sol cast`
that teaches the agent what it is and what it can do. This file is the
agent's entire understanding of the orchestration system.

**Minimal example:**

```markdown
# Outpost Agent: Toast (world: myworld)

You are a outpost agent in a multi-agent orchestration system.

## Your Assignment
- Work item: sol-abc
- Title: Add input validation to login form
- Description: The login form accepts any input. Add email format
  validation and password length checks.

## Commands
- `sol resolve` — Signal work complete (pushes branch, clears tether)
- `sol workflow current` — Show current step instructions
- `sol workflow advance` — Mark step complete, advance to next
- `sol mail inbox` — Check for messages
- `sol escalate` — Request help if stuck

## Protocol
Execute your assignment. When finished, run `sol resolve`.
If stuck, run `sol escalate` with a description of the problem.
```

#### Claude Code Tethers

`sol cast` installs Claude Code tether scripts per-agent:

- **`SessionStart`** → runs `sol prime` to inject execution context. `sol prime`
  reads the durable `.tether` file, looks up the work item in the store, loads
  workflow state, and outputs structured context. This is how GUPP works —
  the agent starts, the tether fires, context appears.

- **`UserPromptSubmit`** → drains the nudge queue (unchanged from Gastown).
  At each turn boundary, pending nudges are delivered to the agent's input.

Tether scripts are shell scripts installed in the agent's worktree at
`.claude/tethers/`. They call `sol` subcommands — no agent-specific logic
lives in the tethers themselves.

#### Context Recovery on Restart

When a session crashes and the prefect restarts it, the same tethers fire.
`sol prime` reads the durable `.tether` file, looks up the work item, loads
workflow state, and re-injects context. The agent doesn't know it crashed —
it just starts with instructions.

This is why the tether file is the durability primitive: it survives everything
except explicit detachment (`sol resolve` or operator `sol unhook`). As long as
`.tether` exists, `sol prime` can reconstruct full execution context from
durable state in SQLite and the filesystem.

---

## 4. What We're NOT Building

### Wasteland Federation

**Why not:** DoltHub-based cross-sphere sharing was experimental and tightly
coupled to Dolt as the storage backend. With SQLite, there is no natural
federation transport. Cross-instance work sharing is a fundamentally different
product. If needed later, a REST API or git-based sync protocol would be
designed from scratch.

### TUI Dashboard (Charmbracelet Stack)

**Why not:** The TUI added significant dependency weight (bubbletea, bubbles,
lipgloss, glamour) for a feature that competes with the operator's primary
interface: the terminal itself. `sol status --json | jq` and `tmux attach` are
more debuggable and more composable than a custom TUI. A TUI can be layered
on top later without architectural changes.

### Web Dashboard

**Why not:** Same reasoning as TUI. A web dashboard requires an HTTP server,
authentication, and frontend code — none of which are essential to the core
value proposition. SQLite databases are readable by any web framework if
someone wants to build one later.

### Multi-Agent-Runtime Support (Gemini, Codex, Cursor)

**Why not initially:** The abstraction layer for multiple AI runtimes adds
complexity to every component that interacts with agent sessions. The target
system is designed around Claude Code's specific capabilities (tethers,
`--dangerously-skip-permissions`, tool use patterns). Multi-runtime support
can be added later by abstracting the session start and prime interfaces.

### Cost Tier Management

**Why not initially:** Routing work to different Claude model tiers (Haiku
for simple tasks, Opus for complex ones) is an optimization. The initial
system uses a single configured model. Tier routing requires task complexity
estimation, which is itself a research problem.

### OpenTelemetry Integration

**Why not initially:** The JSONL event feed provides sufficient observability
for a single-operator system. OpenTelemetry is designed for distributed
systems with multiple teams consuming metrics. If the system scales to
multi-operator deployment, OTel integration is straightforward (the event
feed is already structured data).

### npm Package Distribution

**Why not:** The system is a single Go binary distributed as a compiled
executable. npm distribution adds Node.js as a runtime dependency for no
architectural benefit.

---

## 5. Incremental Build Loops

### Loop 0: Foundation — Single Agent Dispatch

**What it adds:** An operator dispatches a work item to one AI agent, the
agent executes it in an isolated worktree, and the operator can verify the
result.

**What it requires:**
- `sol store` CLI for work item CRUD (SQLite)
- `sol session` CLI for tmux session management
- Tether mechanism (`.tether` file)
- `sol cast` (minimal: create worktree, tether work, start session)
- `sol prime` (minimal: read tether, output context)
- `sol resolve` (minimal: push branch, clear tether, kill session)

**What it defers:** Multi-agent, sentinels, forge, mail, workflows,
caravans, prefect, consul, events.

**Definition of done:**
1. `sol store create --title="Add tests for login" --world=myworld` creates a work item
2. `sol cast <item-id> myworld` spawns a outpost in a fresh worktree with the work item context
3. The outpost session starts with work instructions injected (GUPP)
4. `sol resolve` (called by outpost) pushes the branch and returns the outpost to idle
5. Operator can `tmux attach` to observe the agent working
6. Operator can `sqlite3 ~/sol/.store/myworld.db "SELECT * FROM work_items"` to inspect state
7. Crash recovery: kill the tmux session, re-run `sol cast` → agent picks up tethered work

**Key risks:**
- SQLite concurrency model may need tuning (busy_timeout, journal_mode)
- Claude Code tether integration (SessionStart → `sol prime`) may have race conditions
- Worktree creation from bare repo may have git version-specific issues

### Loop 1: Multi-Agent with Supervision

**What it adds:** Multiple concurrent outposts per world. A prefect process
detects crashes and restarts sessions. Basic health monitoring.

**What it requires:**
- Themed name pool (a `go:embed` default list of 50+ names, with file
  override at `$SOL_HOME/{world}/names.txt`). When scanning 30 tmux sessions,
  "Toast" vs "Jasper" is immediately distinguishable; "agent-07" vs
  "agent-12" is not. This serves the GLASS principle. Allocation is
  scan-based: pick the first pool name not already in the agents table.
- Per-work-item flock for dispatch serialization
- Prefect process (sphere-level, heartbeat loop, session liveness checks,
  respawn with backoff, mass-death detection)
- `sol prefect run` / `sol prefect stop` (foreground prefect lifecycle)
- `sol status <world>` (show running agents, tethered work, session health)

**What it defers:** Sentinel, forge, mail, workflows, caravans, consul,
events, heartbeat files (stale/hung detection), daemon mode.

**Definition of done:**
1. `sol cast <item1> myworld && sol cast <item2> myworld` dispatches to two different outposts
2. No two outposts get the same work item (flock serialization)
3. Kill a outpost's tmux session → prefect detects and restarts within 3 minutes
4. Restarted outpost picks up tethered work (GUPP)
5. `sol status myworld` shows all running outposts with their states and tethered work
6. `sol prefect run` starts prefect; `sol prefect stop` gracefully stops all sessions
7. Kill 3+ outposts within 30 seconds → prefect enters degraded mode (no respawns)

**Key risks:**
- Concurrent worktree creation from shared bare repo may need locking
- Prefect restart logic may fight with agents that are legitimately stopping
- Mass-death detection threshold may need empirical tuning

### Loop 2: Merge Pipeline

**What it adds:** Completed work flows through a merge queue. A forge
agent validates and merges work into the target branch. Quality gates (tests)
run before merge.

**What it requires:**
- Merge request records in store (merge_requests table, including `blocked_by`)
- Forge as Claude session + Go CLI toolbox (see [ADR-0005](decisions/0005-forge-claude-session.md))
- Merge slot serialization (file lock)
- Quality gate execution (configurable test/build commands)
- `sol resolve` extended to submit merge request
- Conflict-resolution delegation (create work items for complex conflicts)

**What it defers:** Sentinel, mail system, nudge queue (introduced in Loop 3
when sentinels need notification), caravans, workflows, consul, events.

**Definition of done:**
1. Outpost calls `sol resolve` → merge request created in store with `phase=ready`
2. Forge Claude session patrols `merge_requests` table, claims the MR, rebases onto main, runs quality gates
3. Trivial conflicts → forge resolves directly during rebase
4. Complex conflicts → forge delegates to outpost via `create-resolution`, MR blocked until resolved
5. Tests pass → forge merges to main, updates MR and work item status in DB
6. Tests fail → forge attributes failure (branch vs pre-existing), retries or marks failed
7. Operator can `sol forge queue myworld` to see pending merges
8. Only one merge in progress at a time per world (slot lock)
9. Operator can `sol forge attach myworld` to watch the forge work

**Key risks:**
- Non-deterministic conflict resolution (mitigated by "when in doubt, delegate" rule)
- Flaky tests causing repeated merge failures
- API cost proportional to queue activity (mitigated: Go fallback available)

### Loop 3: Sentinel, Health Monitoring, and Observability

**What it adds:** Per-world sentinel process that monitors outposts, detects
stalled/zombie sessions, and uses AI-assisted assessment for stuck agent
detection. Mail system for inter-agent communication. Event feed and chronicle
for observability. This is where inter-agent communication first becomes
necessary — protocol messages (OUTPOST_DONE, MERGE_READY, RECOVERY_NEEDED)
flow through the mail system.

**What it requires:**
- Mail system (SQLite-backed `messages` table in sphere.db + protocol message
  helpers) — new infrastructure, first loop with inter-agent communication
- Event feed (append-only JSONL with cross-process flock)
- Chronicle process (dedup, aggregation, feed truncation)
- Sentinel process (Go process with AI call-outs, long-running, per-world)
- Outpost state detection (session liveness + tether state + tmux output hashing)
- Stalled/zombie detection and recovery
- AI-assisted assessment via `claude -p` when tmux output unchanged between
  patrols — targeted AI calls for judgment, not a full AI agent session
- Event instrumentation of existing operations (cast, done, forge,
  prefect)
- `sol mail`, `sol feed`, `sol chronicle`, `sol sentinel` commands

**What it defers:** Caravans, workflows, consul, nudge queue (real-time
delivery to agents at turn boundaries), escalation CRUD, conflict resolution.

**Definition of done:**
1. Sentinel patrols outposts every 3 minutes
2. Dead outpost with tethered work → sentinel triggers respawn (max 2 attempts)
3. After 2 failed respawns → work item returned to open status
4. Working outpost with unchanged tmux output → AI assessment via `claude -p`
5. AI assessment returns "stuck" with high confidence → nudge injected into session
6. AI assessment failure → patrol continues (non-blocking, best-effort)
7. Zombie outpost (live session + idle + no tether) → session cleaned up
8. `sol feed --follow` shows real-time activity stream
9. `sol feed --type=patrol` shows sentinel patrol activity
10. Chronicle deduplicates and aggregates events in curated feed
11. `sol mail send/inbox/read/ack/check` work for inter-agent messaging
12. Existing operations (cast, done, forge, prefect) emit events

**Key risks:**
- AI assessment cost at scale (mitigated: only fires when heuristic triggers)
- AI assessment quality (mitigated: low confidence → no action)
- Sentinel patrol timing may conflict with outpost work (nudging a working agent)
- Event feed volume may be high with 30 agents (mitigated: chronicle aggregation)

### Loop 4: Workflows and Caravans

**What it adds:** Multi-step workflows for structured agent work. Caravan
tracking for batches of related work items. Conflict resolution in merges.

**What it requires:**
- Workflow engine (formula directories, molecule instances, state tracking)
- `sol workflow` commands (instantiate, current, advance, status)
- Caravan tracking (caravan records in store, item dependencies)
- `sol caravan` commands (create, check, status, launch)
- Merge conflict resolution path (REWORK_REQUEST → re-assign outpost)
- `sol cast` extended for batch dispatch and formula instantiation
- `sol prime` extended for workflow context injection

**What it defers:** Consul, escalation system, handoff continuity.

**Definition of done:**
1. `sol cast default-work --on=<item> myworld` creates workflow instance with steps
2. Outpost follows propulsion loop: current → execute → advance → repeat
3. Workflow state survives session crash and restart
4. `sol caravan create "auth-feature" item1 item2 item3` tracks batch
5. As items merge, caravan auto-checks readiness
6. Complex merge conflict → forge delegates via `create-resolution` → outpost resolves → `done --force-with-lease` unblocks MR
7. `sol workflow status` shows progress through multi-step work

**Key risks:**
- Workflow state consistency across crash/restart cycles
- Caravan dependency resolution correctness (topological ordering)
- Conflict resolution loop may create infinite cycles (needs max retry)

### Loop 5: Consul and Full Orchestration

**What it adds:** Sphere-level AI agent (consul) for cross-world coordination,
stale tether recovery, stranded caravan feeding. Escalation system. Handoff
continuity for long-running agents.

**What it requires:**
- Consul agent with patrol cycle
- Stale tether detection and recovery
- Stranded caravan detection and dispatch
- Escalation system (severity-based routing)
- `sol handoff` for session continuity (save state, send handoff mail, respawn)
- `sol escalate` for manual escalation

**What it defers:** Nothing critical — this completes the core system.

**Definition of done:**
1. Consul runs continuous patrol, writes heartbeat
2. Prefect monitors consul via heartbeat, restarts if stale
3. Stale tethers (tethered work with no active session for >1 hour) auto-recovered
4. Stranded caravans (ready work not dispatched) auto-fed
5. `sol escalate --severity=high "tests are failing on myworld"` creates escalation
6. High-severity escalation emails human operator
7. Agents can `sol handoff` for clean session restart with context preservation
8. Full `sol prefect run` / `sol prefect stop` lifecycle works across all agent types

**Key risks:**
- Consul patrol may create too much activity (needs rate limiting)
- Escalation routing depends on external integrations (email, SMS) — may be flaky
- Handoff context quality depends on agent's ability to summarize its state

---

## 6. Technology Choices

### Language: Go

**All components are written in Go.** Justification:

- **Single-binary deployment:** Each component compiles to a static binary
  with no runtime dependencies. `cp sol /usr/local/bin/` is the install.
- **Concurrency model:** Goroutines and channels for the prefect heartbeat
  loop, concurrent session health checks, and the chronicle. Go's concurrency
  is well-suited to "check 30 things every 3 minutes."
- **SQLite bindings:** `modernc.org/sqlite` provides a pure-Go SQLite
  implementation (no CGo, no `libsqlite3` dependency). Alternatively,
  `mattn/go-sqlite3` with CGo for maximum compatibility.
- **Existing ecosystem:** The operator already has Go installed (Gastown is
  Go). Formula parsing, TOML config, JSON handling, file operations — all
  well-served by Go's standard library.
- **Prototyping velocity:** Go is fast enough to write, fast enough to run,
  and produces artifacts that are easy to deploy.

**Why not Rust:** Rust would provide stronger correctness guarantees for the
prefect and session manager, but the compile-time cost slows iteration.
The system's primary failure modes are operational (tmux quirks, SQLite
locking, agent behavior), not memory safety. Go is the pragmatic choice for
a system where the operator is the primary debugger.

**Why not Shell:** Shell scripts are appropriate for glue (the prefect
lifecycle orchestration could be shell), but the prefect, store, and
workflow engine need structured error handling, JSON parsing, and SQLite
access that shell does poorly. Shell is used where it's natural: Claude Code
tethers, simple wrappers.

**Binary structure:** Single binary with subcommands (`sol cast`, `sol store`,
`sol session`). The `sol` binary includes all plumbing and porcelain commands.
Long-running processes (prefect, chronicle) are started as subprocesses
(e.g., `sol prefect run`). Internal package boundaries are clean enough to
allow splitting into separate binaries later if needed, but a single binary
simplifies distribution (`cp sol /usr/local/bin/`), versioning (one version
number), and deployment (no coordination between binaries).

### Dependencies

**Load-bearing (must be reliable):**

| Dependency | Purpose | Risk Mitigation |
|-----------|---------|-----------------|
| SQLite | Storage | Pure-Go implementation (`modernc.org/sqlite`), no external library |
| tmux (>= 3.2) | Session management | System package, widely available, battle-tested |
| git | Worktree management, merge ops | System package, universally available |
| Claude Code | AI agent runtime | External dependency, monitored by health checks |

**Standard library only (no external Go deps):**

| Need | Solution |
|------|----------|
| JSON parsing | `encoding/json` |
| TOML parsing | `github.com/BurntSushi/toml` (small, stable, no transitive deps) |
| File locking | `syscall.Flock` (POSIX) |
| CLI framework | `github.com/spf13/cobra` (proven, stable) |
| Process management | `os/exec`, `syscall` |

**Explicitly NOT depended on:**

| Tool | Why Not |
|------|---------|
| Dolt | Replaced by SQLite (eliminates server SPOF) |
| DoltHub | Wasteland federation is out of scope |
| Charmbracelet stack | TUI is out of scope |
| OpenTelemetry | JSONL events are sufficient initially |
| Node.js / npm | No JavaScript in the stack |

### Platform

**Primary target: Linux (amd64, arm64).** This is where AI coding
agents run in practice — cloud VMs and developer workstations.

**macOS support: yes, with caveats.** Go cross-compiles trivially. SQLite
and tmux work on macOS. The prefect uses PID files and signals (POSIX),
not systemd. macOS-specific: `launchd` plist generation for prefect
auto-start (equivalent to `systemd enable`).

**Container support: partial.** The system can run inside a container, but
tmux requires a pseudo-terminal. Interactive attachment works via `docker exec
-it`. Headless container deployment (tmux-free) is out of scope for initial
loops. If needed later, the session interface can be abstracted behind a Go
interface with both a tmux implementation and a process-group implementation.

**Windows: not supported.** POSIX file locking, tmux, and process signals
are fundamental to the design. WSL2 works as a Linux environment.

### Testing Strategy

**Unit tests:** Standard Go tests for store operations, workflow state
transitions, session health classification, dispatch serialization, and
message routing. Each package has its own `_test.go` files.

**Integration tests:** Multi-process tests that exercise real components
end-to-end. Key scenarios:
- Dispatch → execute → resolve → merge (happy path)
- Session crash mid-work → prefect restart → agent recovery
- Concurrent dispatch (verify no double-assignment via flock)
- Merge conflict detection and rework flow (Loop 4+)

**CRASH acceptance tests:** For each build loop, a specific crash test
that exercises the recovery path:
- Loop 0: Kill tmux session mid-work → re-cast → agent resumes from tether
- Loop 1: Kill 3+ sessions within 30s → prefect enters degraded mode
- Loop 2: Kill forge mid-merge → merge request released after TTL
- Loop 3: Kill sentinel mid-patrol → prefect restarts, no data loss

**Approach:** Tests use real SQLite, real tmux, real git worktrees — no
mocks for load-bearing infrastructure. Tests create isolated environments
(temp directories, dedicated tmux server via `TMUX_TMPDIR`) so they don't
interfere with live operation. The test harness provides helpers for
creating disposable worlds, spawning agents, and waiting for state transitions.

---

*This document is the target architecture for a production-ready multi-agent
orchestration system. It is informed by the Gastown prototype (experimental,
proven the concept), constrained by production-readiness (stability over
features), and designed for incremental implementation (each loop produces a
working system).*

*All major design decisions are resolved. Deferred items are noted in their
respective loop descriptions.*
