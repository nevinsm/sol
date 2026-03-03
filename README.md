# sol — Multi-Agent Orchestration System

A production-ready system for coordinating concurrent AI coding agents.

## What It Does

Software teams are deploying 10, 20, 30+ concurrent AI coding agents across repositories. `sol` is the infrastructure that makes this work: it assigns work to agents, isolates them in git worktrees so they never conflict, supervises their health, merges their output through quality gates, and recovers automatically when things break. The entire system is a single Go binary backed by SQLite — no servers, no containers, no dependencies beyond tmux.

## Key Concepts

| Concept | What it is |
|---------|-----------|
| **World** | A project/repository under management. Each world has its own database, agents, and worktrees. |
| **Agent** | A persistent identity (name, work history, state) backed by an ephemeral AI session. |
| **Tether** | A file at `$SOL_HOME/{world}/outposts/{agent}/.tether` — the durability primitive. If work is on the tether, the agent runs it. Survives crashes. |
| **Cast** | Dispatch a work item to an agent: create worktree, write tether, start session. |
| **Prime** | Inject execution context (CLAUDE.md, tether content, workflow state) when a session starts. |
| **Resolve** | Signal completion: push branch, update state, clear tether, stop session. |
| **Outpost** | A worker agent's station within a world. Directory at `$SOL_HOME/{world}/outposts/{agent}/`. |
| **Sentinel** | Per-world health monitor. Detects stalls, zombies, and stuck agents. |
| **Forge** | Merge pipeline. Processes completed work through quality gates into the target branch. |
| **Consul** | Sphere-level patrol. Recovers stale tethers, feeds caravans, handles lifecycle requests. |
| **Prefect** | Top-level orchestrator. Manages sentinel, forge, and consul processes. |
| **Caravan** | A batch of work items dispatched and tracked as a group. |
| **Workflow** | A multi-step formula (directory of markdown instructions) executed by an agent. |
| **SOL_HOME** | Runtime root directory (env var, default `~/sol`). All state lives here. |

## Quick Start

```bash
# Build and install
make build
make install  # copies bin/sol to /usr/local/bin

# Initialize a world
export SOL_HOME=~/sol
sol world init myworld --source-repo=git@github.com:org/your-repo.git
sol world init myworld --source-repo=/path/to/local/repo

# Create agents (or let cast auto-provision them)
sol agent create Toast --world=myworld
sol agent create Rye --world=myworld

# Create work items
sol store create --world=myworld --title="Implement feature X" --description="..."
sol store create --world=myworld --title="Fix bug Y" --description="..."

# Dispatch work
sol cast <work-item-id> myworld                     # auto-selects idle agent
sol cast <work-item-id> myworld --agent=Toast       # target a specific agent

# Watch an agent work
sol session attach sol-myworld-Toast

# Check status
sol status myworld
sol world status myworld          # includes world config
sol store list --world=myworld
sol session list

# Run with full supervision
sol prefect run                    # manages all worlds
sol prefect run --consul           # includes sphere-level patrol
```

## Architecture Overview

### Design Principles

- **ZFC** (Zero Filesystem Cache) — Never cache state in memory. Always read from the source of truth. With 30 concurrent agents mutating state, any cache is a lie.
- **GUPP** (Universal Propulsion Principle) — If you find work on your tether, you run it. No confirmation, no polling. The tether IS the instruction.
- **CRASH** (Crash Recovery As Standard Handling) — Every component has a defined crash recovery path. Tested, not assumed.
- **GLASS** (Inspectability) — The system must be inspectable with `sqlite3`, `cat`, `ls`, `jq`. No specialized tooling required.
- **DEGRADE** (Graceful Degradation) — Subsystems down means reduced capacity, not halt. If supervision dies, agents still run their tethered work. If the merge queue is down, completed work waits safely.
- **EVOLVE** (Schema Evolution) — All schemas versioned, migrations run on startup. The system evolves without breaking.

### Component Hierarchy

```
Prefect
├── Sentinel (per-world)     — health monitoring, stall detection, AI assessment
├── Forge (per-world)        — merge queue, quality gates, conflict resolution
├── Consul (sphere-level)    — stale tether recovery, caravan feeding, lifecycle
└── Outposts (per-world)     — worker agents executing tethered work items
```

The prefect manages the lifecycle of all other components. Sentinel monitors outpost health within a world. Forge processes completed work into the target branch. Consul patrols across all worlds for system-level recovery. Each component can fail independently without taking down the others.

### Storage Model

Two SQLite databases (WAL mode, `busy_timeout=5000`, `foreign_keys=ON`):

- **Sphere DB** (`$SOL_HOME/.store/sphere.db`) — Agents, messages, escalations, caravans, world registry. Shared across all worlds.
- **World DB** (`$SOL_HOME/.store/{world}.db`) — Work items, labels, merge requests, dependencies. One per world.

State on the filesystem:

- **Tethers** — `$SOL_HOME/{world}/outposts/{agent}/.tether`
- **Workflows** — `$SOL_HOME/{world}/outposts/{agent}/.workflow/`
- **Formulas** — `$SOL_HOME/formulas/{name}/`
- **Events** — `$SOL_HOME/.events.jsonl` (raw), `$SOL_HOME/.feed.jsonl` (curated)
- **World Config** — `$SOL_HOME/{world}/world.toml`
- **Global Config** — `$SOL_HOME/sol.toml`

## CLI Reference

### World Management

| Command | Description |
|---------|-------------|
| `sol world init <name>` | Create a world (database, directory tree, config). `--source-repo` associates a git repository. |
| `sol world list` | List all registered worlds. `--json` for machine-readable output. |
| `sol world status <name>` | Show world status including config, agents, work items, and health. `--json` supported. |
| `sol world delete <name>` | Delete a world and all associated data. Requires `--confirm`. Refuses if sessions are active. |
| `sol world sync <name>` | Fetch and pull latest from the managed repo's origin. Clones if repo doesn't exist yet. |

### Dispatch

| Command | Description |
|---------|-------------|
| `sol cast <item-id> <world>` | Assign work to an agent, create worktree, start session |
| `sol prime --world=W --agent=A` | Assemble and print execution context for an agent |
| `sol resolve --world=W --agent=A` | Signal completion: push branch, update state, clear tether |

`cast` accepts `--agent` (auto-selects idle if omitted), `--formula`, and `--var` flags.

### Agents

| Command | Description |
|---------|-------------|
| `sol agent create <name> --world=W` | Create an agent (default role: agent) |
| `sol agent list --world=W` | List agents in a world |

### Store (Work Items)

| Command | Description |
|---------|-------------|
| `sol store create --world=W --title=T` | Create a work item |
| `sol store get <id> --world=W` | Get a work item by ID |
| `sol store list --world=W` | List work items (filter by `--status`, `--label`, `--assignee`) |
| `sol store update <id> --world=W` | Update status, assignee, or priority |
| `sol store close <id> --world=W` | Close a work item |
| `sol store query --world=W --sql=Q` | Run a read-only SQL query |

### Dependencies

| Command | Description |
|---------|-------------|
| `sol store dep add <from> <to> --world=W` | Add a dependency (from depends on to) |
| `sol store dep remove <from> <to> --world=W` | Remove a dependency |
| `sol store dep list <id> --world=W` | List dependencies for a work item |

### Sessions

| Command | Description |
|---------|-------------|
| `sol session start <name>` | Start a tmux session |
| `sol session stop <name>` | Stop a tmux session |
| `sol session list` | List all sessions |
| `sol session health <name>` | Check session health |
| `sol session capture <name>` | Capture pane output |
| `sol session attach <name>` | Attach to a session |
| `sol session inject <name> --message=M` | Inject text into a session |

### Supervision

| Command | Description |
|---------|-------------|
| `sol prefect run` | Run the prefect (foreground). `--consul` enables sphere-level patrol. |
| `sol prefect stop` | Stop the running prefect |
| `sol status <world>` | Show world status (exit code reflects health) |

### Sentinel (Per-World Health Monitor)

| Command | Description |
|---------|-------------|
| `sol sentinel run <world>` | Run the sentinel patrol loop (foreground) |
| `sol sentinel start <world>` | Start sentinel as background tmux session |
| `sol sentinel stop <world>` | Stop the sentinel |
| `sol sentinel attach <world>` | Attach to the sentinel session |

### Forge (Merge Pipeline)

| Command | Description |
|---------|-------------|
| `sol forge start <world>` | Start the forge as a Claude session |
| `sol forge stop <world>` | Stop the forge |
| `sol forge attach <world>` | Attach to the forge session |
| `sol forge queue <world>` | Show the merge request queue |

Toolbox subcommands (used by the forge Claude session):

| Command | Description |
|---------|-------------|
| `sol forge ready <world>` | List ready merge requests |
| `sol forge blocked <world>` | List blocked merge requests |
| `sol forge claim <world>` | Claim the next ready MR |
| `sol forge release <world> <mr-id>` | Release a claimed MR back to ready |
| `sol forge run-gates <world>` | Run quality gates |
| `sol forge push <world>` | Push to target branch |
| `sol forge mark-merged <world> <mr-id>` | Mark MR as merged |
| `sol forge mark-failed <world> <mr-id>` | Mark MR as failed |
| `sol forge create-resolution <world> <mr-id>` | Create conflict resolution task |
| `sol forge check-unblocked <world>` | Check for resolved blockers |

### Messaging

| Command | Description |
|---------|-------------|
| `sol mail send --to=R --subject=S` | Send a message |
| `sol mail inbox` | List pending messages |
| `sol mail read <msg-id>` | Read a message (marks as read) |
| `sol mail ack <msg-id>` | Acknowledge a message |
| `sol mail check` | Count unread messages (exit 1 if unread) |

### Escalations

| Command | Description |
|---------|-------------|
| `sol escalate <description>` | Create an escalation (`--severity`: low/medium/high/critical) |
| `sol escalation list` | List escalations (`--status`: open/acknowledged/resolved) |
| `sol escalation ack <id>` | Acknowledge an escalation |
| `sol escalation resolve <id>` | Resolve an escalation |

### Observability

| Command | Description |
|---------|-------------|
| `sol feed` | View event feed (`-f` follow, `-n` limit, `--since`, `--type`) |
| `sol log-event --type=T --actor=A` | Log a custom event (plumbing) |
| `sol chronicle run` | Run the event chronicle (foreground) |
| `sol chronicle start` | Start chronicle as background session |
| `sol chronicle stop` | Stop the chronicle |

### Workflows

| Command | Description |
|---------|-------------|
| `sol workflow instantiate <formula>` | Instantiate a workflow from a formula |
| `sol workflow current --world=W --agent=A` | Print current step instructions |
| `sol workflow advance --world=W --agent=A` | Advance to next step |
| `sol workflow status --world=W --agent=A` | Show workflow progress |

### Caravans

| Command | Description |
|---------|-------------|
| `sol caravan create <name> [items...]` | Create a caravan with optional items |
| `sol caravan add <caravan-id> <items...>` | Add items to a caravan |
| `sol caravan check <caravan-id>` | Check readiness of caravan items |
| `sol caravan status [caravan-id]` | Show caravan status |
| `sol caravan launch <caravan-id> --world=W` | Dispatch ready items in a caravan |

### Handoff (Session Continuity)

| Command | Description |
|---------|-------------|
| `sol handoff --world=W --agent=A` | Hand off to a fresh session with context preservation |

`--summary` provides a progress summary. Captures tmux output, git state, and workflow progress into `.handoff.json`, then restarts the session with that context.

### Consul (Sphere-Level Patrol)

| Command | Description |
|---------|-------------|
| `sol consul run` | Run the consul patrol loop (foreground) |
| `sol consul status` | Show consul status from heartbeat |

`consul run` accepts `--interval` (default 5m), `--stale-timeout` (default 1h), and `--webhook` for escalation notifications.

## Build Loops

The system was built in six incremental loops. Each loop produces a fully working system.

| Loop | What it added | Status |
|------|--------------|--------|
| **Loop 0** | Single agent dispatch — store, session, tether, cast, prime, resolve | Complete |
| **Loop 1** | Multi-agent supervision — prefect, agent respawn, health checks | Complete |
| **Loop 2** | Merge pipeline — forge, quality gates, conflict resolution | Complete |
| **Loop 3** | Observability — sentinel, events, chronicle, mail system | Complete |
| **Loop 4** | Workflows and caravans — formulas, step-based execution, batch dispatch | Complete |
| **Loop 5** | Full orchestration — consul, escalations, handoff, lifecycle management | Complete |

Post-build arcs refine and operationalize:

| Arc | What it does | Status |
|-----|-------------|--------|
| **Arc 0** | Rename (gt → sol) — full codebase rename from Gastown prototype | Complete |
| **Arc 1** | World lifecycle — `sol world init/list/status/delete`, config files, hard gate | Complete |
| **Arc 2** | Operator onboarding — `sol doctor`, `sol init`, `sol status`, lipgloss styling | Complete |
| **Arc 3** | Envoy + Governor — brief system, persistent agents, per-world coordination | Complete |
| **Arc 3.5** | Managed world repository — URL init, managed clone, `sol world sync` | Complete |

## Project Structure

```
gt-src/
├── main.go                        Entry point
├── Makefile                        build, test, install, clean
├── cmd/                            Cobra command definitions
│   ├── root.go                     Root command, version
│   ├── world.go                    World lifecycle management
│   ├── cast.go, prime.go, resolve.go  Dispatch pipeline
│   ├── agent.go                    Agent management
│   ├── store.go, store_dep.go      Work items and dependencies
│   ├── session.go                  tmux session management
│   ├── prefect.go                  Top-level orchestrator
│   ├── forge.go                    Merge pipeline + toolbox
│   ├── status.go                   World status
│   ├── sentinel.go                 Per-world health monitor
│   ├── feed.go, log_event.go       Event feed
│   ├── chronicle.go                Event chronicle
│   ├── mail.go                     Inter-agent messaging
│   ├── workflow.go                 Workflow engine
│   ├── caravan.go                  Batch dispatch
│   ├── escalate.go, escalation.go  Escalation management
│   ├── handoff.go                  Session continuity
│   └── consul.go                   Sphere-level patrol
├── internal/
│   ├── config/                     SOL_HOME resolution, world config
│   ├── store/                      SQLite: work items, agents, messages, escalations
│   ├── session/                    tmux: start, stop, health, capture, inject
│   ├── tether/                     Tether file read/write/clear
│   ├── protocol/                   CLAUDE.md + tether script generation
│   ├── namepool/                   Name generation
│   ├── dispatch/                   Cast/prime/resolve core logic
│   ├── prefect/                    Agent respawn, health checks
│   ├── forge/                      Merge queue, quality gates
│   ├── sentinel/                   Stall detection, AI assessment
│   ├── status/                     World status gathering
│   ├── events/                     JSONL event feed + chronicle
│   ├── workflow/                   Directory-based state machine, formulas
│   ├── escalation/                 Notifier interface, log/mail/webhook
│   ├── handoff/                    Session continuity, capture/exec
│   ├── world/                      World config types (if separate from config)
│   └── consul/                     Sphere-level patrol, heartbeat
├── test/integration/               End-to-end tests
└── docs/
    ├── manifesto.md                Design philosophy
    ├── target-architecture.md      Full system specification
    ├── naming.md                   Naming glossary and migration reference
    ├── arc-roadmap.md              Post-build arc roadmap
    ├── decisions/                  Architecture Decision Records
    ├── reviews/                    Post-arc review reports
    └── prompts/                    Build loop and arc prompts
```

## Development

```bash
make build       # Build binary to bin/sol
make test        # Run all unit tests
make install     # Install to /usr/local/bin
make clean       # Remove build artifacts
```

### Conventions

- **Commits**: [Conventional Commits](https://www.conventionalcommits.org/) — `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`. Use scope when helpful: `feat(store): add label filtering`.
- **Work item IDs**: `sol-` + 8 hex chars (e.g., `sol-a1b2c3d4`)
- **Session names**: `sol-{world}-{agentName}` (e.g., `sol-myworld-Toast`)
- **Timestamps**: RFC 3339 in UTC
- **Error messages**: Include context — `"failed to open world database %q: %w"`
- **SQLite connections**: Always set `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`
- **World config**: `$SOL_HOME/{world}/world.toml` (per-world), `$SOL_HOME/sol.toml` (global)

## Design Documents

- [Manifesto](docs/manifesto.md) — Design philosophy: what we learned from the Gastown prototype, what we're building, why stability is the feature.
- [Target Architecture](docs/target-architecture.md) — Full system specification: components, schemas, interfaces, failure modes, build loops.
- [Naming Glossary](docs/naming.md) — Sol naming conventions and migration reference from Gastown.
- [Arc Roadmap](docs/arc-roadmap.md) — Post-build arc roadmap (Arc 0–4).
- [Architecture Decision Records](docs/decisions/) — Decisions that diverge from the target architecture:
  - [ADR-0001](docs/decisions/0001-sentinel-as-go-process.md) — Sentinel as Go process with targeted AI call-outs
  - [ADR-0003](docs/decisions/0003-ai-assessment-gated-by-output-hashing.md) — AI assessment gated by tmux output hashing
  - [ADR-0004](docs/decisions/0004-chronicle-as-separate-component.md) — Chronicle as separate component
  - [ADR-0005](docs/decisions/0005-forge-claude-session.md) — Forge as Claude session + Go toolbox (supersedes ADR-0002)
  - [ADR-0006](docs/decisions/0006-prefect-defers-to-sentinel.md) — Prefect defers outpost management to sentinel
  - [ADR-0007](docs/decisions/0007-consul-as-go-process.md) — Consul as Go process
  - [ADR-0008](docs/decisions/0008-world-lifecycle.md) — World lifecycle with dual-store design
