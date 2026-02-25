# Target Architecture: Multi-Agent Orchestration System

> Informed by Gastown as a requirements document, Unix philosophy as a
> stability guide, and production-readiness as the primary constraint.

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

> "If you find something on your hook, YOU RUN IT."

When an agent starts a session and discovers work on its hook, it must begin
execution immediately. No confirmation, no waiting. The hook IS the assignment.

This principle is essential for throughput. A system with 30 agents where each
waits for a supervisor acknowledgment before starting work creates a
bottleneck at the supervisor. GUPP eliminates that bottleneck. The hook
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
| Session Mgr (3.2) | `.runtime/sessions/*.json` | tmux server memory | Supervisor restarts sessions | <3 min |
| Mail (3.3) | `messages` table | In-flight INSERT | Re-derive from DB | <1s |
| Nudge Queue (3.4) | Pending nudge files | Claimed (in-delivery) nudges | Re-derive from pending messages | <1s |
| Workflow Engine (3.5) | `state.json`, step files | In-memory step state | Re-read state.json on restart | <1s |
| Supervisor (3.6) | PID file, session registry | Heartbeat loop state | Restart supervisor (systemd/launchd) | <10s |
| Deacon (3.7) | Heartbeat file | Patrol cycle state | Supervisor restarts, re-patrols | <3 min |
| Witness (3.8) | Patrol state file | Current patrol cycle | Supervisor restarts, re-patrols | <3 min |
| Refinery (3.9) | `merge_requests` table, slot lock | In-progress merge | TTL expiry releases claimed MR | <30 min |
| Polecat (3.10) | Hook file, worktree, identity | Session memory | `gt prime` re-injects context (GUPP) | <30s |
| Event Feed (3.11) | JSONL files | Curator buffer | Curator restarts, tails from last position | <10s |

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
| SQLite store | Agents with hooked work continue executing (hook is a local file). New dispatch fails. Pending messages unavailable. |
| Supervisor | Running agents continue. No crash recovery or new spawns. |
| Witness | Polecats work normally. Completed work waits in queue. |
| Refinery | Work accumulates in merge queue. No merges land. |
| Network/git remote | Agents work locally. `gt done` push phase retries. |

The key insight: **an agent with work on its hook and a local worktree needs
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
- Transactional status transitions (hooked → working → done must not lose state)
- Label/priority-based queries for dispatch decisions
- Dependency graph traversal for convoy readiness

**Existing tools that solve this well:** SQLite (concurrent reads via WAL,
serialized writes, SQL queries, single-file deployment). The query patterns
from Gastown's behavioral spec — label intersection, priority sorting, assignee
filtering, dependency graph — are all natural SQL.

### 2.2 Work Dispatch (Essential)

**Problem:** Assigning work to agents — selecting the right agent, preparing
the execution context, and starting the session.

**Hard constraints:**
- Must be serialized per-bead (prevent double-assignment)
- Must be atomic: if session start fails, the bead returns to open status
- Must support batch dispatch (10+ beads at once)
- Latency budget: <5s per single dispatch, <30s for batch of 10

**Existing tools:** Per-bead flock (advisory file lock) for serialization.
The dispatch operation is inherently sequential per bead — validate, assign,
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
- Must support 50+ identities per rig (name pool)
- Queries: find idle agents, find agent by name, get agent history

**Existing tools:** SQLite table for identity records. Agent directories on
the filesystem for local state.

### 2.5 Work Execution Context (Essential)

**Problem:** When an agent starts (or restarts), it needs to know what to do —
what's on its hook, what step it's on, what the instructions say.

**Hard constraints:**
- Must work after session crash (derive from durable state, not memory)
- Must be fast (<2s to assemble context)
- Must include: hooked work, workflow step, pending messages, checkpoint state

**Existing tools:** A `prime` command that reads hook file, workflow state,
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
POLECAT_DONE), nudges, and freeform mail.

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
- Serialized merges (one at a time per rig to prevent conflict explosion)
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

**Existing tools:** A supervisor process that checks PID/session liveness on
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
Curator process for dedup/aggregation. Standard Unix tools (`tail`, `jq`,
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
                    │   (gt CLI, tmux attach, sqlite3)     │
                    └────────────┬────────────────────────┘
                                 │
                    ┌────────────┴────────────────────────┐
                    │            gt CLI                     │
                    │   (porcelain: sling, done, up,       │
                    │    down, status, mail, convoy)        │
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
    │  Supervisor   │  │   Deacon     │
    │  (Go process) │  │  (AI agent)  │
    └──────┬───────┘  └──────┬───────┘
           │                 │
     ┌─────┼─────┐     ┌────┼────┐
     │     │     │     │    │    │
     ▼     ▼     ▼     ▼    ▼    ▼
   Wit.  Ref.  Wit.  Dogs  Stale  Stranded
   (rig1)(rig1)(rig2)       hooks  convoys
     │
     ├── Polecat sessions (×M per rig)
     └── Crew sessions (×K per rig, human-managed)
```

### 3.1 Store (SQLite)

**What it does:** Provides the single source of truth for all coordination
state — work items, agent identities, merge requests, convoys, and
escalations. Replaces Dolt/beads with SQLite in WAL mode.

**Dependencies:** None (SQLite is a library, not a server).

**What depends on it:** Everything that reads or writes coordination state —
the CLI, supervisor, all agent types.

**Failure mode:** If the database file is corrupted or locked, operations
that require coordination state fail. Agents with hooked work continue
executing (DEGRADE principle). The store is a file, so corruption recovery
is `cp backup.db store.db`.

**State:** One SQLite database per rig, plus one town-level database.
- Town DB: `~/gt/.store/town.db` — agent identities, mail, convoys, escalations
- Rig DBs: `~/gt/.store/{rig}.db` — work items, merge requests

**Schema:**

```sql
-- town.db
CREATE TABLE agents (
    id          TEXT PRIMARY KEY,    -- e.g., "gastown/Toast"
    name        TEXT NOT NULL,       -- e.g., "Toast"
    rig         TEXT NOT NULL,       -- e.g., "gastown"
    role        TEXT NOT NULL,       -- polecat|witness|refinery|crew
    state       TEXT NOT NULL DEFAULT 'idle',  -- idle|working|stuck|zombie
    hook_bead   TEXT,                -- currently hooked work item ID
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE agent_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id    TEXT NOT NULL REFERENCES agents(id),
    bead_id     TEXT NOT NULL,
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

CREATE TABLE convoys (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open',
    owner       TEXT,
    created_at  TEXT NOT NULL,
    closed_at   TEXT
);

CREATE TABLE convoy_items (
    convoy_id   TEXT NOT NULL REFERENCES convoys(id),
    bead_id     TEXT NOT NULL,
    rig         TEXT NOT NULL,
    PRIMARY KEY (convoy_id, bead_id)
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

-- {rig}.db
CREATE TABLE work_items (
    id          TEXT PRIMARY KEY,    -- e.g., "gt-abc"
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

**Interface:** A `gt store` CLI (plumbing) wrapping SQLite operations:

```
gt store create --db=<rig> --title="..." --priority=2 --label=gt:task
gt store get <id>
gt store list --db=<rig> --status=open --label=gt:task --assignee=<agent> --json
gt store update <id> --status=hooked --assignee=<agent>
gt store close <id>
gt store query --db=<rig> --sql="SELECT ..." (escape hatch for complex queries)
```

**Concurrency handling:** SQLite WAL mode supports concurrent readers. Writers
are serialized by SQLite's built-in locking with `PRAGMA busy_timeout=5000`
(5s retry). For the actual workload (30 agents, mostly non-overlapping writes
to different work items), this is sufficient. Write contention happens only
when multiple agents try to claim the same work item — handled by per-bead
flock before the SQLite write.

**Why not Dolt:** Dolt is a server process (single point of failure), requires
MySQL protocol overhead for simple reads, and its git-native versioning
(while elegant) adds complexity that isn't justified for the production use
case. SQLite is a library — no server, no port, no connection management.
The database is a single file copyable with `cp`.

**Why not plain files:** 30 concurrent agents writing labeled, prioritized,
dependency-linked work items is a database workload. Plain files with symlink
indexes and advisory locks would be reinventing SQLite badly. Label
intersection queries (`status=open AND label=gt:task AND priority < 2`) are
one SQL statement but would require directory traversal and set intersection
with files.

### 3.2 Session Manager (tmux)

**What it does:** Provides process containers for AI agents — creating
sessions, injecting text, capturing output, checking health, and enabling
interactive attachment.

**Dependencies:** tmux (>= 3.2).

**What depends on it:** All agent types (polecats, witnesses, refineries,
crew). The supervisor for liveness checks. The nudge system for message
injection.

**Failure mode:** If tmux server crashes, all sessions die. The supervisor
detects this (all PID checks fail simultaneously) and enters degraded mode.
Recovery: supervisor restarts tmux server, then restarts agents. Hooked work
is durable — agents recover via GUPP on restart.

**State:** Tmux server state (in-memory, volatile). Session metadata stored
as tmux environment variables and in `.runtime/sessions/{name}.json` files.

**Interface:**

```
gt session start <name> --workdir=<dir> --cmd=<command> --env=KEY=VAL
gt session stop <name> [--force]
gt session list [--json]
gt session health <name> [--max-inactivity=30m]
    → exit 0: healthy, 1: dead, 2: agent-dead, 3: hung
gt session capture <name> --lines=N
gt session attach <name>
gt session inject <name> --message=<text>  (low-level nudge)
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

**Dependencies:** Store (SQLite `messages` table in `town.db`), nudge queue
(for delivery notifications).

**What depends on it:** All agent communication — protocol messages
(MERGE_READY, POLECAT_DONE), nudges, escalations, human-agent messages.

**Failure mode:** Same as store — if SQLite is unavailable, message writes
fail. Agents with hooked work continue executing (DEGRADE principle). Pending
messages are also unavailable when the store is down.

**State:** The `messages` table in `town.db` (schema in Section 3.1). This is
the same WAL-mode SQLite database used for all other coordination state. One
source of truth, one consistency model, one backup strategy.

**Interface:**

```
gt mail send --to=<recipient> --subject=<text> --body=<text> [--priority=N]
gt mail inbox [--identity=<addr>] [--json]
gt mail read <message-id>
gt mail ack <message-id>
gt mail check [--identity=<addr>]   (count unread)
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

**Protocol messages:** MERGE_READY, POLECAT_DONE, MERGED, MERGE_FAILED,
REWORK_REQUEST, CONVOY_NEEDS_FEEDING, RECOVERED_BEAD, RECOVERY_NEEDED,
HANDOFF, HELP. These use the same `messages` table with the `type` field
set to `'protocol'` and structured JSON in the body. Receivers query by
`type='protocol'` and parse the subject prefix.

### 3.4 Nudge Queue

**What it does:** Non-destructive message delivery to AI agents at turn
boundaries. When an agent is busy (mid-tool-call), nudges queue until the
agent reaches a natural pause point.

**Dependencies:** Filesystem, tmux (for injection).

**What depends on it:** Mail system (delivery notifications), supervisor
(health pings), human operator (direct nudges).

**Failure mode:** If nudge delivery fails, the nudge file remains in the
queue and is retried next turn boundary. Expired nudges are silently
discarded.

**State:** File-based FIFO queues (carried forward from Gastown — this design
is sound):

```
~/gt/.runtime/nudge_queue/{session}/
├── {nanosecond-timestamp}-{hex}.json     Pending nudge
├── {nanosecond-timestamp}-{hex}.claimed  Being delivered
```

**Interface:**

```
gt nudge <target> [message]
gt nudge --session=<name> --message=<text> [--priority=normal|urgent]
```

**Delivery sequence:** Unchanged from Gastown — acquire per-session lock,
find agent pane, send text in literal mode, wait 500ms, ESC, Enter with
retry, SIGWINCH for detached sessions. The Claude Code `UserPromptSubmit`
hook drains the queue at turn boundaries.

**Invariants:** Max queue depth 50 per session. FIFO within priority. TTL:
normal=30min, urgent=2hr. Atomic claim via file rename.

### 3.5 Workflow Engine (Directory-based)

**What it does:** Manages multi-step workflows — templates (formulas),
instances (molecules), step tracking, and progress state.

**Dependencies:** Filesystem, store (for linking workflows to work items).

**What depends on it:** Work dispatch (instantiating workflows on assignment),
agents (reading current step), `gt done` (advancing/completing workflows).

**Failure mode:** If a workflow state file is corrupted, the agent loses its
place. Recovery: the operator or supervisor re-reads the step directory to
reconstruct state. Each step's completion is idempotent — re-running a
completed step is safe.

**State:**

```
~/gt/formulas/                          Templates (checked into repo)
├── polecat-work/
│   ├── manifest.toml                   Metadata: name, type, variables
│   └── steps/
│       ├── 01-load-context.md          Step instructions
│       ├── 02-implement.md
│       └── 03-verify.md
└── witness-patrol/
    ├── manifest.toml
    └── steps/
        └── 01-patrol.md

~/gt/{rig}/polecats/{name}/.workflow/   Active workflow instance
├── manifest.json                       {formula, work_item_id, vars}
├── state.json                          {current_step, completed: [], status}
└── steps/
    ├── 01-load-context.json            {id, title, status, started_at}
    ├── 02-implement.json
    └── 03-verify.json
```

**Manifest TOML format:**

```toml
name = "polecat-work"
type = "workflow"            # workflow|convoy|expansion
description = "Standard polecat work execution"

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
gt workflow instantiate <formula-dir> --bead=<id> --var=key=val
    → creates .workflow/ directory with state.json and step files
gt workflow current [--agent=<name>]
    → outputs current step instructions to stdout
gt workflow advance [--agent=<name>]
    → marks current step complete, finds next ready step, updates state.json
gt workflow status [--agent=<name>]
    → outputs progress summary
```

**Propulsion loop (what agents execute):**
```
1. gt workflow current     → read step instructions
2. Execute step
3. gt workflow advance     → close step, advance to next
4. If status != "done": GOTO 1
```

**Ephemeral workflows:** For patrol loops (witness, deacon), the workflow
directory is created in a temporary location and deleted when the patrol
cycle completes. State is not preserved across cycles (same as Gastown's
wisps, but using filesystem semantics).

### 3.6 Supervisor

**What it does:** A Go process that monitors agent session liveness, restarts
crashed sessions, and provides the heartbeat loop. Replaces the three-layer
daemon → boot → deacon chain.

**Dependencies:** Tmux (for session checks), filesystem (PID files, heartbeat
files).

**What depends on it:** All agent sessions (for crash recovery). The operator
(for system startup/shutdown).

**Failure mode:** If the supervisor crashes, running agents continue
unaffected. No new agents are spawned, and crashed agents are not recovered.
The operator must restart the supervisor. A system-level supervisor (systemd
or launchd) can restart the supervisor process itself.

**State:**

```
~/gt/.runtime/
├── supervisor.pid              Supervisor process PID
├── supervisor.log              Structured JSON log
├── sessions/
│   └── {session-name}.json     {pid, started_at, role, rig}
└── heartbeats/
    └── {session-name}.json     {timestamp, cycle, status}
```

**Design — why two layers instead of three:**

Gastown's three-layer chain (daemon → boot → deacon) was designed to solve the
problem that a Go process can't reason about agent behavior and an AI agent
can't detect its own hang. In practice, this produced session-confusion bugs
and over-engineering.

The new design uses two layers:

1. **Supervisor (Go process):** Handles what code can handle — process
   liveness, session existence, heartbeat freshness, mass-death detection,
   respawn with backoff. This replaces both daemon and boot.

2. **Deacon (AI agent):** Handles what requires judgment — stale hook
   recovery, stranded convoy feeding, quality assessment of stuck agents.
   Monitored by the supervisor via heartbeat, just like any other agent.

The boot agent is eliminated. Its triage logic ("is the deacon's heartbeat
stale? should I nudge or wake?") becomes a simple function in the supervisor:

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
1. Check supervisor PID file (single-instance guard)
2. For each registered session:
   a. Check tmux session exists
   b. If missing: respawn (with backoff — 30s, 1m, 2m, 5m)
   c. If exists: check heartbeat freshness
   d. If stale (>15 min): restart session
3. Mass-death detection: 3+ deaths in 30s → enter degraded mode (log only,
   no respawns, notify operator)
4. Deacon triage (see above)

**Interface:**

```
gt supervisor start     (background, writes PID file)
gt supervisor stop
gt supervisor status    (show all monitored sessions)
gt supervisor logs [-f]
```

### 3.7 Deacon (AI Agent)

**What it does:** Town-level AI agent that handles tasks requiring judgment —
stale hook recovery, stranded convoy feeding, cross-rig coordination. Narrower
scope than Gastown's deacon because process liveness is handled by the
supervisor.

**Dependencies:** Store, mail, session manager.

**What depends on it:** Convoy dispatch, stale hook recovery, dog management.

**Failure mode:** If the deacon crashes, the supervisor restarts it. While
down: stale hooks accumulate (resolved on restart), convoys with ready work
wait (dispatched on restart). No data loss.

**State:** Heartbeat file (`~/gt/deacon/heartbeat.json`), patrol state.

**Patrol cycle:**
1. Write heartbeat
2. Check for stale hooks (work items with `status=hooked` and no active session for the assignee). Unhook and return to open.
3. Check for stranded convoys (convoys with closed items that have unstarted dependent work). Dispatch ready items.
4. Process lifecycle requests (cycle, restart, shutdown from operator mail).
5. Run health checks on agents with low activity (complement supervisor's process-level checks with behavior-level assessment).

### 3.8 Witness (AI Agent, Per-Rig)

**What it does:** Per-rig health monitor that patrols polecats, verifies work
completion, and routes completed work to the refinery.

**Dependencies:** Store, mail, session manager, tmux.

**What depends on it:** Polecats (for crash recovery, stuck detection).
Refinery (receives MERGE_READY messages).

**Failure mode:** If the witness crashes, the supervisor restarts it. While
down: completed polecat work waits for verification, crashed polecats are not
respawned (supervisor handles session restarts, but witness handles
work-level recovery). No data loss.

**State:** Patrol state file (`~/gt/{rig}/witness/.patrol-state.json`).

**Patrol cycle:**
1. Scan polecat directories
2. For each polecat:
   a. Check tmux session health
   b. If session dead + work hooked → mark stalled, attempt respawn (max 2)
   c. If session alive + no progress for threshold → nudge
   d. If explicitly stuck → escalate
3. Process POLECAT_DONE messages: verify clean git state, send MERGE_READY
4. Process MERGED/MERGE_FAILED messages: update work item status

### 3.9 Refinery (AI Agent, Per-Rig)

**What it does:** Merge queue processor that claims, validates, and merges
completed work into the target branch.

**Dependencies:** Store, mail, git, session manager.

**What depends on it:** Nothing directly — it is a terminal consumer of
the merge pipeline.

**Failure mode:** If the refinery crashes, the supervisor restarts it. Claimed
merge requests with expired TTL (30 min) are automatically released for
re-claim. No merges land while down; the queue accumulates.

**State:** Merge queue in rig SQLite DB (`merge_requests` table). Merge slot
lock file (`~/gt/{rig}/refinery/.merge-slot.lock`).

**Merge pipeline:**
1. Poll for `phase=ready` merge requests, sorted by priority + age
2. Claim (set `phase=claimed`, `claimed_by`, `claimed_at`)
3. Rebase onto latest target branch
4. Run quality gates (test, build, lint — configurable per rig)
5. If conflict → send REWORK_REQUEST to witness
6. If gates fail → send MERGE_FAILED to witness
7. Merge to target branch → send MERGED to witness
8. If convoy-eligible → send CONVOY_NEEDS_FEEDING to deacon

### 3.10 Polecat (Worker Agent)

**What it does:** Executes work items. Each polecat has persistent identity,
a git worktree sandbox, and ephemeral tmux sessions.

**Dependencies:** Store (for identity), git (for worktree), tmux (for
session), workflow engine (for step tracking).

**What depends on it:** Witnesses (monitor polecats). Refineries (receive
completed work).

**Failure mode:** Session crash → work remains hooked, worktree preserved.
On restart, `gt prime` reads hook, workflow state, and pending messages to
reconstruct context. Agent resumes from last durable state (GUPP + CRASH).

**Three-layer architecture (kept from Gastown):**

| Layer | What | Lifecycle | Storage |
|-------|------|-----------|---------|
| Identity | Agent record, work history | Permanent | SQLite `agents` + `agent_history` |
| Sandbox | Git worktree, branch | Persistent across assignments | `~/gt/{rig}/polecats/{name}/rig/` |
| Session | Claude in tmux | Ephemeral per step/handoff | tmux server memory |

**States:**

| State | Description | Session | Sandbox | Hook |
|-------|-------------|---------|---------|------|
| `idle` | Awaiting assignment | Dead | Preserved | Clear |
| `working` | Executing work | Alive | Exists | Set |
| `stalled` | Session crashed mid-work | Dead | Exists | Set |
| `stuck` | Explicitly requested help | Alive | Exists | Set |
| `zombie` | tmux exists but no worktree | Alive | Missing | — |

**Hook:** A file at `~/gt/{rig}/polecats/{name}/.hook` containing the work
item ID. Existence of this file means work is assigned. The hook is the
durability primitive — it survives everything except explicit detachment.
On session start, `gt prime` reads `.hook`, looks up the work item in the
store, loads workflow state, and injects execution context.

### 3.11 Event Feed

**What it does:** Captures system activity as an append-only event log, with
a curator that produces a filtered/aggregated feed for agent consumption.

**Dependencies:** Filesystem (flock for concurrent writes).

**What depends on it:** Operator monitoring (`gt feed`). Agents (for
situational awareness, optional).

**Failure mode:** Event logging is best-effort — failures are silently
ignored. If the curator crashes, the raw log continues growing and the
curated feed is stale. The supervisor restarts the curator. No primary
operations are affected.

**State:**
- Raw events: `~/gt/.events.jsonl` (append-only, flock-serialized)
- Curated feed: `~/gt/.feed.jsonl` (append-only, truncated at 10MB)

**Event format (unchanged from Gastown):**
```json
{"ts":"...","source":"gt","type":"sling","actor":"overseer","visibility":"both","payload":{...}}
```

**Curator:** A standalone process (`gt curator`) that tails the raw event
file, filters by visibility, deduplicates (10s window for done events),
aggregates (30s window for sling bursts), and appends to the curated feed.

**Interface:**
```
gt feed [--follow] [--limit=N] [--since=<time>] [--type=<type>]
gt log-event --type=<type> --actor=<actor> [--visibility=feed|audit|both]
gt curator start    (background process)
```

### 3.12 Agent Protocol Integration

**What it does:** Bridges the Go orchestration system and the AI agents
running inside tmux sessions. Defines how agents learn their commands,
receive their work context, and recover after crashes.

**Dependencies:** Store (for work item lookup), workflow engine (for step
state), session manager (for hook installation).

**What depends on it:** All agent types — every agent session needs protocol
integration to function.

#### CLAUDE.md Contract

Every polecat worktree gets a `.claude/CLAUDE.md` file generated by `gt sling`
that teaches the agent what it is and what it can do. This file is the
agent's entire understanding of the orchestration system.

**Minimal example:**

```markdown
# Polecat Agent: Toast (rig: myrig)

You are a polecat agent in a multi-agent orchestration system.

## Your Assignment
- Work item: gt-abc
- Title: Add input validation to login form
- Description: The login form accepts any input. Add email format
  validation and password length checks.

## Commands
- `gt done` — Signal work complete (pushes branch, clears hook)
- `gt workflow current` — Show current step instructions
- `gt workflow advance` — Mark step complete, advance to next
- `gt mail inbox` — Check for messages
- `gt escalate` — Request help if stuck

## Protocol
Execute your assignment. When finished, run `gt done`.
If stuck, run `gt escalate` with a description of the problem.
```

#### Claude Code Hooks

`gt sling` installs Claude Code hook scripts per-agent:

- **`SessionStart`** → runs `gt prime` to inject execution context. `gt prime`
  reads the durable `.hook` file, looks up the work item in the store, loads
  workflow state, and outputs structured context. This is how GUPP works —
  the agent starts, the hook fires, context appears.

- **`UserPromptSubmit`** → drains the nudge queue (unchanged from Gastown).
  At each turn boundary, pending nudges are delivered to the agent's input.

Hook scripts are shell scripts installed in the agent's worktree at
`.claude/hooks/`. They call `gt` subcommands — no agent-specific logic
lives in the hooks themselves.

#### Context Recovery on Restart

When a session crashes and the supervisor restarts it, the same hooks fire.
`gt prime` reads the durable `.hook` file, looks up the work item, loads
workflow state, and re-injects context. The agent doesn't know it crashed —
it just starts with instructions.

This is why the hook file is the durability primitive: it survives everything
except explicit detachment (`gt done` or operator `gt unhook`). As long as
`.hook` exists, `gt prime` can reconstruct full execution context from
durable state in SQLite and the filesystem.

---

## 4. What We're NOT Building

### Wasteland Federation

**Why not:** DoltHub-based cross-town sharing was experimental and tightly
coupled to Dolt as the storage backend. With SQLite, there is no natural
federation transport. Cross-instance work sharing is a fundamentally different
product. If needed later, a REST API or git-based sync protocol would be
designed from scratch.

### TUI Dashboard (Charmbracelet Stack)

**Why not:** The TUI added significant dependency weight (bubbletea, bubbles,
lipgloss, glamour) for a feature that competes with the operator's primary
interface: the terminal itself. `gt status --json | jq` and `tmux attach` are
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
system is designed around Claude Code's specific capabilities (hooks,
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
- `gt store` CLI for work item CRUD (SQLite)
- `gt session` CLI for tmux session management
- Hook mechanism (`.hook` file)
- `gt sling` (minimal: create worktree, hook work, start session)
- `gt prime` (minimal: read hook, output context)
- `gt done` (minimal: push branch, clear hook, kill session)

**What it defers:** Multi-agent, witnesses, refinery, mail, workflows,
convoys, supervisor, deacon, events.

**Definition of done:**
1. `gt store create --title="Add tests for login" --db=myrig` creates a work item
2. `gt sling <item-id> myrig` spawns a polecat in a fresh worktree with the work item context
3. The polecat session starts with work instructions injected (GUPP)
4. `gt done` (called by polecat) pushes the branch and returns the polecat to idle
5. Operator can `tmux attach` to observe the agent working
6. Operator can `sqlite3 ~/gt/.store/myrig.db "SELECT * FROM work_items"` to inspect state
7. Crash recovery: kill the tmux session, re-run `gt sling` → agent picks up hooked work

**Key risks:**
- SQLite concurrency model may need tuning (busy_timeout, journal_mode)
- Claude Code hook integration (SessionStart → `gt prime`) may have race conditions
- Worktree creation from bare repo may have git version-specific issues

### Loop 1: Multi-Agent with Supervision

**What it adds:** Multiple concurrent polecats per rig. A supervisor process
detects crashes and restarts sessions. Basic health monitoring.

**What it requires:**
- Themed name pool (a text file of 50 names + an allocation index in the
  `agents` table). When scanning 30 tmux sessions, "Toast" vs "Jasper" is
  immediately distinguishable; "agent-07" vs "agent-12" is not. This serves
  the GLASS principle. Configurable by replacing the names file.
- Per-bead flock for dispatch serialization
- Supervisor process (heartbeat loop, session liveness checks, respawn)
- `gt up` / `gt down` (ordered service start/stop)
- `gt status` (show running agents, hooked work, session health)

**What it defers:** Witness, refinery, mail, workflows, convoys, deacon, events.

**Definition of done:**
1. `gt sling <item1> myrig && gt sling <item2> myrig` dispatches to two different polecats
2. No two polecats get the same work item (flock serialization)
3. Kill a polecat's tmux session → supervisor detects and restarts within 3 minutes
4. Restarted polecat picks up hooked work (GUPP)
5. `gt status` shows all running polecats with their states and hooked work
6. `gt up` starts supervisor; `gt down` gracefully stops all sessions
7. Kill 5 polecats within 10 seconds → supervisor enters degraded mode (no respawns)

**Key risks:**
- Concurrent worktree creation from shared bare repo may need locking
- Supervisor restart logic may fight with agents that are legitimately stopping
- Mass-death detection threshold may need empirical tuning

### Loop 2: Merge Pipeline

**What it adds:** Completed work flows through a merge queue. A refinery
agent validates and merges work into the target branch. Quality gates (tests)
run before merge.

**What it requires:**
- Merge request records in store (merge_requests table)
- Refinery agent (long-running, polls merge queue)
- Merge slot serialization (file lock)
- Quality gate execution (configurable test/build commands)
- `gt done` extended to submit merge request

**What it defers:** Witness, mail system, nudge queue (introduced in Loop 3
when witnesses need notification), convoys, workflows, deacon, events,
conflict resolution.

**Definition of done:**
1. Polecat calls `gt done` → merge request created in store with `phase=ready`
2. Refinery polls `merge_requests` table, claims the MR, rebases onto main, runs `go test ./...`
3. Tests pass → refinery merges to main, updates MR and work item status in DB
4. Tests fail → refinery updates MR status, MR stays in queue for retry
5. Operator can `gt refinery queue myrig` to see pending merges
6. Only one merge in progress at a time per rig (slot lock)
7. Operator can `gt refinery attach myrig` to watch the refinery work

**Key risks:**
- Rebase conflicts during merge (deferred: conflict resolution in Loop 4)
- Flaky tests causing repeated merge failures

### Loop 3: Witness and Health Monitoring

**What it adds:** Per-rig witness agents that monitor polecats, detect
stalled/zombie sessions, and route completed work to the refinery. The event
feed for observability. This is where inter-agent communication first becomes
necessary — the witness needs to send MERGE_READY to the refinery, and the
refinery needs to send MERGED/MERGE_FAILED back.

**What it requires:**
- Witness agent (long-running, per-rig)
- Polecat state detection (session liveness + hook state + activity)
- Stalled/zombie detection and recovery
- Mail system (SQLite-backed messages + nudge queue for delivery) — new
  infrastructure, first loop that requires inter-agent communication
- POLECAT_DONE → verify → MERGE_READY pipeline
- Event feed (raw JSONL + curator)
- `gt feed` command

**What it defers:** Convoys, workflows, deacon, escalations, conflict resolution.

**Definition of done:**
1. Witness patrols polecats every 3 minutes
2. Dead polecat with hooked work → witness triggers respawn (max 2 attempts)
3. After 2 failed respawns → work item returned to open status
4. Polecat calls `gt done` → witness verifies clean git state → sends MERGE_READY
5. `gt feed --follow` shows real-time activity stream
6. `gt feed --type=patrol` shows witness patrol activity
7. Zombie polecat (tmux session with no worktree) → witness cleans up

**Key risks:**
- Witness patrol timing may conflict with polecat work (nudging a working agent)
- Stalled detection heuristics need tuning (what counts as "no progress"?)
- Event feed volume may be high with 30 agents

### Loop 4: Workflows and Convoys

**What it adds:** Multi-step workflows for structured agent work. Convoy
tracking for batches of related work items. Conflict resolution in merges.

**What it requires:**
- Workflow engine (formula directories, molecule instances, state tracking)
- `gt workflow` commands (instantiate, current, advance, status)
- Convoy tracking (convoy records in store, item dependencies)
- `gt convoy` commands (create, check, status, launch)
- Merge conflict resolution path (REWORK_REQUEST → re-assign polecat)
- `gt sling` extended for batch dispatch and formula instantiation
- `gt prime` extended for workflow context injection

**What it defers:** Deacon, escalation system, handoff continuity.

**Definition of done:**
1. `gt sling polecat-work --on=<item> myrig` creates workflow instance with steps
2. Polecat follows propulsion loop: current → execute → advance → repeat
3. Workflow state survives session crash and restart
4. `gt convoy create "auth-feature" item1 item2 item3` tracks batch
5. As items merge, convoy auto-checks readiness
6. Merge conflict → refinery sends REWORK_REQUEST → witness assigns polecat
7. `gt workflow status` shows progress through multi-step work

**Key risks:**
- Workflow state consistency across crash/restart cycles
- Convoy dependency resolution correctness (topological ordering)
- Conflict resolution loop may create infinite cycles (needs max retry)

### Loop 5: Deacon and Full Orchestration

**What it adds:** Town-level AI agent (deacon) for cross-rig coordination,
stale hook recovery, stranded convoy feeding. Escalation system. Handoff
continuity for long-running agents.

**What it requires:**
- Deacon agent with patrol cycle
- Stale hook detection and recovery
- Stranded convoy detection and dispatch
- Escalation system (severity-based routing)
- `gt handoff` for session continuity (save state, send handoff mail, respawn)
- `gt escalate` for manual escalation

**What it defers:** Nothing critical — this completes the core system.

**Definition of done:**
1. Deacon runs continuous patrol, writes heartbeat
2. Supervisor monitors deacon via heartbeat, restarts if stale
3. Stale hooks (hooked work with no active session for >1 hour) auto-recovered
4. Stranded convoys (ready work not dispatched) auto-fed
5. `gt escalate --severity=high "tests are failing on myrig"` creates escalation
6. High-severity escalation emails human operator
7. Agents can `gt handoff` for clean session restart with context preservation
8. Full `gt up` / `gt down` lifecycle works across all agent types

**Key risks:**
- Deacon patrol may create too much activity (needs rate limiting)
- Escalation routing depends on external integrations (email, SMS) — may be flaky
- Handoff context quality depends on agent's ability to summarize its state

---

## 6. Technology Choices

### Language: Go

**All components are written in Go.** Justification:

- **Single-binary deployment:** Each component compiles to a static binary
  with no runtime dependencies. `cp gt /usr/local/bin/` is the install.
- **Concurrency model:** Goroutines and channels for the supervisor heartbeat
  loop, concurrent session health checks, and the curator. Go's concurrency
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
supervisor and session manager, but the compile-time cost slows iteration.
The system's primary failure modes are operational (tmux quirks, SQLite
locking, agent behavior), not memory safety. Go is the pragmatic choice for
a system where the operator is the primary debugger.

**Why not Shell:** Shell scripts are appropriate for glue (the `gt up` /
`gt down` orchestration could be shell), but the supervisor, store, and
workflow engine need structured error handling, JSON parsing, and SQLite
access that shell does poorly. Shell is used where it's natural: Claude Code
hooks, simple wrappers.

**Binary structure:** Single binary with subcommands (`gt sling`, `gt store`,
`gt session`). The `gt` binary includes all plumbing and porcelain commands.
Long-running processes (supervisor, curator) are started as subprocesses
(e.g., `gt supervisor run`). Internal package boundaries are clean enough to
allow splitting into separate binaries later if needed, but a single binary
simplifies distribution (`cp gt /usr/local/bin/`), versioning (one version
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
and tmux work on macOS. The supervisor uses PID files and signals (POSIX),
not systemd. macOS-specific: `launchd` plist generation for supervisor
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
- Dispatch → execute → done → merge (happy path)
- Session crash mid-work → supervisor restart → agent recovery
- Concurrent dispatch (verify no double-assignment via flock)
- Merge conflict detection and rework flow (Loop 4+)

**CRASH acceptance tests:** For each build loop, a specific crash test
that exercises the recovery path:
- Loop 0: Kill tmux session mid-work → re-sling → agent resumes from hook
- Loop 1: Kill 5 sessions simultaneously → supervisor enters degraded mode
- Loop 2: Kill refinery mid-merge → merge request released after TTL
- Loop 3: Kill witness mid-patrol → supervisor restarts, no data loss

**Approach:** Tests use real SQLite, real tmux, real git worktrees — no
mocks for load-bearing infrastructure. Tests create isolated environments
(temp directories, dedicated tmux server via `TMUX_TMPDIR`) so they don't
interfere with live operation. The test harness provides helpers for
creating disposable rigs, spawning agents, and waiting for state transitions.

---

*This document is the target architecture for a production-ready multi-agent
orchestration system. It is informed by the Gastown prototype (experimental,
proven the concept), constrained by production-readiness (stability over
features), and designed for incremental implementation (each loop produces a
working system).*

*All major design decisions are resolved. Deferred items are noted in their
respective loop descriptions.*
