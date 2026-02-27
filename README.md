# gt — Multi-Agent Orchestration System

A production-ready system for coordinating concurrent AI coding agents.

## What It Does

Software teams are deploying 10, 20, 30+ concurrent AI coding agents across repositories. `gt` is the infrastructure that makes this work: it assigns work to agents, isolates them in git worktrees so they never conflict, supervises their health, merges their output through quality gates, and recovers automatically when things break. The entire system is a single Go binary backed by SQLite — no servers, no containers, no dependencies beyond tmux.

## Key Concepts

| Concept | What it is |
|---------|-----------|
| **Rig** | A project/repository under management. Each rig has its own database, agents, and worktrees. |
| **Agent** | A persistent identity (name, work history, state) backed by an ephemeral AI session. |
| **Hook** | A file at `$GT_HOME/{rig}/polecats/{agent}/.hook` — the durability primitive. If work is on the hook, the agent runs it. Survives crashes. |
| **Sling** | Dispatch a work item to an agent: create worktree, write hook, start session. |
| **Prime** | Inject execution context (CLAUDE.md, hook content, workflow state) when a session starts. |
| **Done** | Signal completion: push branch, update state, clear hook, stop session. |
| **Polecat** | A worker agent. The base unit of execution. |
| **Witness** | Per-rig health monitor. Detects stalls, zombies, and stuck agents. |
| **Refinery** | Merge pipeline. Processes completed work through quality gates into the target branch. |
| **Deacon** | Town-level patrol. Recovers stale hooks, feeds convoys, handles lifecycle requests. |
| **Supervisor** | Top-level orchestrator. Manages witness, refinery, and deacon processes. |
| **Convoy** | A batch of work items dispatched and tracked as a group. |
| **Workflow** | A multi-step formula (directory of markdown instructions) executed by an agent. |
| **GT_HOME** | Runtime root directory (env var, default `~/gt`). All state lives here. |

## Quick Start

```bash
# Build and install
make build
make install  # copies bin/gt to /usr/local/bin

# Set up a rig and agents
export GT_HOME=~/gt
gt agent create Toast --rig=myrig
gt agent create Rye --rig=myrig
gt agent create Pumpernickel --rig=myrig

# Create work items
gt store create --db=myrig --title="Implement feature X" --description="..."
gt store create --db=myrig --title="Fix bug Y" --description="..."

# Dispatch work
gt sling <work-item-id> myrig                     # auto-selects idle agent
gt sling <work-item-id> myrig --agent=Toast       # target a specific agent

# Watch an agent work
gt session attach gt-myrig-Toast

# Check status
gt status myrig
gt store list --db=myrig
gt session list

# Run with full supervision
gt supervisor run                    # manages all rigs
gt supervisor run --deacon           # includes town-level patrol
```

## Architecture Overview

### Design Principles

- **ZFC** (Zero Filesystem Cache) — Never cache state in memory. Always read from the source of truth. With 30 concurrent agents mutating state, any cache is a lie.
- **GUPP** (Universal Propulsion Principle) — If you find work on your hook, you run it. No confirmation, no polling. The hook IS the instruction.
- **CRASH** (Crash Recovery As Standard Handling) — Every component has a defined crash recovery path. Tested, not assumed.
- **GLASS** (Inspectability) — The system must be inspectable with `sqlite3`, `cat`, `ls`, `jq`. No specialized tooling required.
- **DEGRADE** (Graceful Degradation) — Subsystems down means reduced capacity, not halt. If supervision dies, agents still run their hooked work. If the merge queue is down, completed work waits safely.
- **EVOLVE** (Schema Evolution) — All schemas versioned, migrations run on startup. The system evolves without breaking.

### Component Hierarchy

```
Supervisor
├── Witness (per-rig)     — health monitoring, stall detection, AI assessment
├── Refinery (per-rig)    — merge queue, quality gates, conflict resolution
├── Deacon (town-level)   — stale hook recovery, convoy feeding, lifecycle
└── Polecats (per-rig)    — worker agents executing hooked work items
```

The supervisor manages the lifecycle of all other components. Witness monitors polecat health within a rig. Refinery processes completed work into the target branch. Deacon patrols across all rigs for system-level recovery. Each component can fail independently without taking down the others.

### Storage Model

Two SQLite databases (WAL mode, `busy_timeout=5000`, `foreign_keys=ON`):

- **Town DB** (`$GT_HOME/.store/town.db`) — Agents, messages, escalations, convoys. Shared across all rigs.
- **Rig DB** (`$GT_HOME/.store/{rig}.db`) — Work items, labels, merge requests, dependencies. One per rig.

State on the filesystem:

- **Hooks** — `$GT_HOME/{rig}/polecats/{agent}/.hook`
- **Workflows** — `$GT_HOME/{rig}/polecats/{agent}/.workflow/`
- **Formulas** — `$GT_HOME/formulas/{name}/`
- **Events** — `$GT_HOME/.feed/events.jsonl`

## CLI Reference

### Dispatch

| Command | Description |
|---------|-------------|
| `gt sling <item-id> <rig>` | Assign work to an agent, create worktree, start session |
| `gt prime --rig=R --agent=A` | Assemble and print execution context for an agent |
| `gt done --rig=R --agent=A` | Signal completion: push branch, update state, clear hook |

`sling` accepts `--agent` (auto-selects idle if omitted), `--formula`, and `--var` flags.

### Agents

| Command | Description |
|---------|-------------|
| `gt agent create <name> --rig=R` | Create an agent (default role: polecat) |
| `gt agent list --rig=R` | List agents in a rig |

### Store (Work Items)

| Command | Description |
|---------|-------------|
| `gt store create --db=R --title=T` | Create a work item |
| `gt store get <id> --db=R` | Get a work item by ID |
| `gt store list --db=R` | List work items (filter by `--status`, `--label`, `--assignee`) |
| `gt store update <id> --db=R` | Update status, assignee, or priority |
| `gt store close <id> --db=R` | Close a work item |
| `gt store query --db=R --sql=Q` | Run a read-only SQL query |

### Dependencies

| Command | Description |
|---------|-------------|
| `gt store dep add <from> <to> --db=R` | Add a dependency (from depends on to) |
| `gt store dep remove <from> <to> --db=R` | Remove a dependency |
| `gt store dep list <id> --db=R` | List dependencies for a work item |

### Sessions

| Command | Description |
|---------|-------------|
| `gt session start <name>` | Start a tmux session |
| `gt session stop <name>` | Stop a tmux session |
| `gt session list` | List all sessions |
| `gt session health <name>` | Check session health |
| `gt session capture <name>` | Capture pane output |
| `gt session attach <name>` | Attach to a session |
| `gt session inject <name> --message=M` | Inject text into a session |

### Supervision

| Command | Description |
|---------|-------------|
| `gt supervisor run` | Run the supervisor (foreground). `--deacon` enables town-level patrol. |
| `gt supervisor stop` | Stop the running supervisor |
| `gt status <rig>` | Show rig status |

### Witness (Per-Rig Health Monitor)

| Command | Description |
|---------|-------------|
| `gt witness run <rig>` | Run the witness patrol loop (foreground) |
| `gt witness start <rig>` | Start witness as background tmux session |
| `gt witness stop <rig>` | Stop the witness |
| `gt witness attach <rig>` | Attach to the witness session |

### Refinery (Merge Pipeline)

| Command | Description |
|---------|-------------|
| `gt refinery start <rig>` | Start the refinery as a Claude session |
| `gt refinery stop <rig>` | Stop the refinery |
| `gt refinery attach <rig>` | Attach to the refinery session |
| `gt refinery queue <rig>` | Show the merge request queue |

Toolbox subcommands (used by the refinery Claude session):

| Command | Description |
|---------|-------------|
| `gt refinery ready <rig>` | List ready merge requests |
| `gt refinery blocked <rig>` | List blocked merge requests |
| `gt refinery claim <rig>` | Claim the next ready MR |
| `gt refinery release <rig> <mr-id>` | Release a claimed MR back to ready |
| `gt refinery run-gates <rig>` | Run quality gates |
| `gt refinery push <rig>` | Push to target branch |
| `gt refinery mark-merged <rig> <mr-id>` | Mark MR as merged |
| `gt refinery mark-failed <rig> <mr-id>` | Mark MR as failed |
| `gt refinery create-resolution <rig> <mr-id>` | Create conflict resolution task |
| `gt refinery check-unblocked <rig>` | Check for resolved blockers |

### Messaging

| Command | Description |
|---------|-------------|
| `gt mail send --to=R --subject=S` | Send a message |
| `gt mail inbox` | List pending messages |
| `gt mail read <msg-id>` | Read a message (marks as read) |
| `gt mail ack <msg-id>` | Acknowledge a message |
| `gt mail check` | Count unread messages |

### Escalations

| Command | Description |
|---------|-------------|
| `gt escalate <description>` | Create an escalation (`--severity`: low/medium/high/critical) |
| `gt escalation list` | List escalations (`--status`: open/acknowledged/resolved) |
| `gt escalation ack <id>` | Acknowledge an escalation |
| `gt escalation resolve <id>` | Resolve an escalation |

### Observability

| Command | Description |
|---------|-------------|
| `gt feed` | View event feed (`-f` follow, `-n` limit, `--since`, `--type`) |
| `gt log-event --type=T --actor=A` | Log a custom event (plumbing) |
| `gt curator run` | Run the event curator (foreground) |
| `gt curator start` | Start curator as background session |
| `gt curator stop` | Stop the curator |

### Workflows

| Command | Description |
|---------|-------------|
| `gt workflow instantiate <formula>` | Instantiate a workflow from a formula |
| `gt workflow current --rig=R --agent=A` | Print current step instructions |
| `gt workflow advance --rig=R --agent=A` | Advance to next step |
| `gt workflow status --rig=R --agent=A` | Show workflow progress |

### Convoys

| Command | Description |
|---------|-------------|
| `gt convoy create <name> [items...]` | Create a convoy with optional items |
| `gt convoy add <convoy-id> <items...>` | Add items to a convoy |
| `gt convoy check <convoy-id>` | Check readiness of convoy items |
| `gt convoy status [convoy-id]` | Show convoy status |
| `gt convoy launch <convoy-id> --rig=R` | Dispatch ready items in a convoy |

### Handoff (Session Continuity)

| Command | Description |
|---------|-------------|
| `gt handoff --rig=R --agent=A` | Hand off to a fresh session with context preservation |

`--summary` provides a progress summary. Captures tmux output, git state, and workflow progress into `.handoff.json`, then restarts the session with that context.

### Deacon (Town-Level Patrol)

| Command | Description |
|---------|-------------|
| `gt deacon run` | Run the deacon patrol loop (foreground) |
| `gt deacon status` | Show deacon status from heartbeat |

`deacon run` accepts `--interval` (default 5m), `--stale-timeout` (default 1h), and `--webhook` for escalation notifications.

## Build Loops

The system was built in six incremental loops. Each loop produces a fully working system.

| Loop | What it added | Status |
|------|--------------|--------|
| **Loop 0** | Single agent dispatch — store, session, hook, sling, prime, done | Complete |
| **Loop 1** | Multi-agent supervision — supervisor, agent respawn, health checks | Complete |
| **Loop 2** | Merge pipeline — refinery, quality gates, conflict resolution | Complete |
| **Loop 3** | Observability — witness, events, curator, mail system | Complete |
| **Loop 4** | Workflows and convoys — formulas, step-based execution, batch dispatch | Complete |
| **Loop 5** | Full orchestration — deacon, escalations, handoff, lifecycle management | Complete |

## Project Structure

```
gt-src/
├── main.go                        Entry point
├── Makefile                        build, test, test-e2e, install, clean
├── cmd/                            Cobra command definitions
│   ├── root.go                     Root command, version
│   ├── sling.go, prime.go, done.go Dispatch pipeline
│   ├── agent.go                    Agent management
│   ├── store.go, store_dep.go      Work items and dependencies
│   ├── session.go                  tmux session management
│   ├── supervisor.go               Top-level orchestrator
│   ├── refinery.go                 Merge pipeline + toolbox
│   ├── status.go                   Rig status
│   ├── witness.go                  Per-rig health monitor
│   ├── feed.go, log_event.go       Event feed
│   ├── curator.go                  Event curator
│   ├── mail.go                     Inter-agent messaging
│   ├── workflow.go                 Workflow engine
│   ├── convoy.go                   Batch dispatch
│   ├── escalate.go, escalation.go  Escalation management
│   ├── handoff.go                  Session continuity
│   └── deacon.go                   Town-level patrol
├── internal/
│   ├── config/                     GT_HOME resolution
│   ├── store/                      SQLite: work items, agents, messages, escalations
│   ├── session/                    tmux: start, stop, health, capture, inject
│   ├── hook/                       Hook file read/write/clear
│   ├── protocol/                   CLAUDE.md + hook script generation
│   ├── namepool/                   Name generation
│   ├── dispatch/                   Sling/prime/done core logic
│   ├── supervisor/                 Agent respawn, health checks
│   ├── refinery/                   Merge queue, quality gates
│   ├── witness/                    Stall detection, AI assessment
│   ├── status/                     Rig status gathering
│   ├── events/                     JSONL event feed + curator
│   ├── workflow/                   Directory-based state machine, formulas
│   ├── escalation/                 Notifier interface, log/mail/webhook
│   ├── handoff/                    Session continuity, capture/exec
│   └── deacon/                     Town-level patrol, heartbeat
├── test/integration/               End-to-end tests
└── docs/
    ├── manifesto.md                Design philosophy
    ├── target-architecture.md      Full system specification
    ├── decisions/                   Architecture Decision Records
    └── prompts/                    Build loop prompts (loop0–loop5)
```

## Development

```bash
make build       # Build binary to bin/gt
make test        # Run all unit tests
make test-e2e    # Run end-to-end integration tests
make install     # Install to /usr/local/bin
make clean       # Remove build artifacts
```

### Conventions

- **Commits**: [Conventional Commits](https://www.conventionalcommits.org/) — `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`. Use scope when helpful: `feat(store): add label filtering`.
- **Work item IDs**: `gt-` + 8 hex chars (e.g., `gt-a1b2c3d4`)
- **Session names**: `gt-{rig}-{agentName}` (e.g., `gt-myrig-Toast`)
- **Timestamps**: RFC 3339 in UTC
- **Error messages**: Include context — `"failed to open rig database %q: %w"`
- **SQLite connections**: Always set `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`

## Design Documents

- [Manifesto](docs/manifesto.md) — Design philosophy: what we learned from the Gastown prototype, what we're building, why stability is the feature.
- [Target Architecture](docs/target-architecture.md) — Full system specification: components, schemas, interfaces, failure modes, build loops.
- [Architecture Decision Records](docs/decisions/) — Decisions that diverge from the target architecture:
  - [ADR-0001](docs/decisions/0001-witness-as-go-process.md) — Witness as Go process with targeted AI call-outs
  - [ADR-0003](docs/decisions/0003-ai-assessment-gated-by-output-hashing.md) — AI assessment gated by tmux output hashing
  - [ADR-0004](docs/decisions/0004-curator-as-separate-component.md) — Curator as separate component
  - [ADR-0005](docs/decisions/0005-refinery-claude-session.md) — Refinery as Claude session + Go toolbox (supersedes ADR-0002)
  - [ADR-0006](docs/decisions/0006-supervisor-defers-to-witness.md) — Supervisor defers polecat management to witness
  - [ADR-0007](docs/decisions/0007-deacon-as-go-process.md) — Deacon as Go process
