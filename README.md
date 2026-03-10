# sol

Run 10, 20, 30+ AI coding agents on a repository at the same time.

Sol assigns work to agents, gives each one its own git worktree so they never step on each other, watches for stalls and crashes, merges completed work through quality gates, and recovers automatically when things break.

It's a single Go binary backed by SQLite. No servers, no containers — just tmux sessions and the filesystem.

## Prerequisites

- **Go 1.21+** (to build)
- **tmux** (agent process containers)
- **git** (worktrees, branching, merging)
- **claude** CLI (AI agent sessions)

Run `sol doctor` after install to verify everything is in place.

## Quick Start

```bash
# Build and install
make build
make install  # copies bin/sol to /usr/local/bin

# First-time setup — creates SOL_HOME and your first world
sol init --name=myworld --source-repo=git@github.com:org/your-repo.git

# Or point at a local repo
sol init --name=myworld --source-repo=/path/to/local/repo

# Create writs
sol store create --world=myworld --title="Implement feature X" --description="..."
sol store create --world=myworld --title="Fix bug Y" --description="..."

# Dispatch work — sol creates an agent, sets up a worktree, and starts a session
sol cast <writ-id> myworld

# Watch an agent work
sol session attach sol-myworld-Toast

# Check on things
sol status myworld
sol store list --world=myworld

# Run with full supervision (health monitoring, auto-merge, recovery)
sol prefect run --consul
```

## How It Works

A **world** is a repository under management. When you initialize a world, sol clones the repo and sets up a database to track work.

You create **writs** describing what needs to be done. When you **cast** a writ, sol picks an idle agent (or creates one), sets up an isolated git worktree, writes the assignment to a **tether** file, and starts a Claude session. The agent reads its tether and gets to work.

When an agent finishes, it calls `sol resolve` — this pushes its branch, updates the writ status, and clears the tether. The **forge** picks up completed branches and merges them through quality gates into the target branch.

Meanwhile, the **sentinel** monitors agent health per-world (detecting stalls and crashes), and the **consul** patrols across all worlds (recovering stale tethers, feeding caravans). The **prefect** supervises all of this — if any component crashes, it restarts it.

For ongoing human-directed work, **envoys** provide persistent agents with their own worktrees and memory that survives across sessions. A **governor** coordinates work within a world, and can be given strategic direction.

Everything is inspectable. Writs live in SQLite — query them with `sqlite3`. Tethers are plain files — read them with `cat`. Sessions are tmux — attach with `sol session attach`.

## Concepts

| Concept | What it is |
|---------|-----------|
| **World** | A repository under management. Has its own database, agents, and worktrees. |
| **Agent** | A named identity (with work history and state) that runs in a tmux session. |
| **Tether** | A file that tells an agent what to work on. If the tether exists, the agent runs it. Survives crashes. |
| **Cast** | Dispatch a writ: create worktree, write tether, start session. |
| **Resolve** | Signal completion: push branch, update state, clear tether. |
| **Outpost** | An agent's workspace within a world (`$SOL_HOME/{world}/outposts/{agent}/`). |
| **Envoy** | A persistent human-directed agent with its own worktree and brief (memory). |
| **Governor** | A per-world coordinator that manages work distribution and strategy. |
| **Sentinel** | Per-world health monitor. Detects stalls and stuck agents. |
| **Forge** | Merge pipeline. Merges completed work through quality gates. |
| **Consul** | Sphere-level patrol. Recovers from failures across all worlds. |
| **Prefect** | Top-level supervisor. Keeps sentinel, forge, and consul running. |
| **Caravan** | A batch of related writs dispatched as a group. |
| **Brief** | An agent's persistent memory file (`.brief/memory.md`), maintained across sessions. |
| **Managed Repo** | Sol's clone of your repository (`$SOL_HOME/{world}/repo/`). All worktrees branch from here. |
| **SOL_HOME** | Runtime root directory (env var, default `~/sol`). All state lives here. |

## Architecture

### How Components Fit Together

```
Prefect
├── Sentinel (per-world)     — health monitoring, stall detection
├── Forge (per-world)        — merge queue, quality gates
├── Consul (sphere-level)    — recovery patrol across all worlds
├── Outposts (per-world)     — worker agents on tethered tasks
├── Envoys (per-world)       — persistent human-directed agents
└── Governor (per-world)     — work coordination
```

Each component can fail independently without taking down the others. If supervision dies, agents keep running their tethered work. If the merge queue is down, completed work waits safely.

### Storage

Two SQLite databases (WAL mode):

- **Sphere DB** (`$SOL_HOME/.store/sphere.db`) — agents, messages, escalations, caravans, world registry
- **World DB** (`$SOL_HOME/.store/{world}.db`) — writs, labels, merge requests, dependencies

State on the filesystem:

- **Tethers** — `$SOL_HOME/{world}/outposts/{agent}/.tether`
- **Managed Repo** — `$SOL_HOME/{world}/repo/`
- **Workflows** — `$SOL_HOME/{world}/outposts/{agent}/.workflow/`
- **Workflow Definitions** — `$SOL_HOME/workflows/{name}/`
- **Events** — `$SOL_HOME/.events.jsonl` (raw), `$SOL_HOME/.feed.jsonl` (curated)
- **World Config** — `$SOL_HOME/{world}/world.toml`
- **Global Config** — `$SOL_HOME/sol.toml`

See [docs/cli.md](docs/cli.md) for the full CLI reference.

## Project Structure

```
sol/
├── main.go                        Entry point
├── Makefile                        build, test, install, clean
├── cmd/                            Cobra command definitions
│   ├── root.go                     Root command, version
│   ├── init.go                     First-time setup (flag/interactive/guided)
│   ├── doctor.go                   Prerequisite checks
│   ├── world.go                    World lifecycle + sync
│   ├── cast.go, prime.go, resolve.go  Dispatch pipeline
│   ├── agent.go                    Agent management
│   ├── store.go, store_dep.go      Writs and dependencies
│   ├── session.go                  tmux session management
│   ├── prefect.go                  Top-level orchestrator
│   ├── forge.go                    Merge pipeline + toolbox
│   ├── status.go                   World status
│   ├── sentinel.go                 Per-world health monitor
│   ├── envoy.go                    Persistent human-directed agents
│   ├── governor.go                 Per-world coordinator
│   ├── brief.go                    Brief injection hooks
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
│   ├── store/                      SQLite: writs, agents, messages, escalations
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
│   ├── workflow/                   Directory-based state machine, workflows
│   ├── escalation/                 Notifier interface, log/mail/webhook
│   ├── handoff/                    Session continuity, capture/exec
│   ├── consul/                     Sphere-level patrol, heartbeat
│   ├── doctor/                     Prerequisite check engine
│   ├── setup/                      Init flow, managed repo cloning
│   ├── brief/                      Brief file management, size enforcement
│   ├── envoy/                      Envoy lifecycle, worktree, hooks
│   ├── governor/                   Governor lifecycle, hooks, world sync
│   └── world/                      World operations (sync, managed repo)
├── test/integration/               End-to-end tests
└── docs/
    ├── manifesto.md                Design philosophy
    ├── failure-modes.md            Crash recovery and degradation
    ├── naming.md                   Naming glossary and migration reference
    └── decisions/                  Architecture Decision Records
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
- **Writ IDs**: `sol-` + 16 hex chars (e.g., `sol-a1b2c3d4e5f6a7b8`)
- **Session names**: `sol-{world}-{agentName}` (e.g., `sol-myworld-Toast`)
- **Timestamps**: RFC 3339 in UTC
- **Error messages**: Include context — `"failed to open world database %q: %w"`
- **SQLite connections**: Always set `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`
- **World config**: `$SOL_HOME/{world}/world.toml` (per-world), `$SOL_HOME/sol.toml` (global)

## Design Documents

- [Manifesto](docs/manifesto.md) — Design philosophy: what we learned from the Gastown prototype, what we're building, why stability is the feature.
- [Failure Modes](docs/failure-modes.md) — Per-component crash recovery, graceful degradation, and mass failure handling.
- [Naming Glossary](docs/naming.md) — Sol naming conventions and migration reference from Gastown.
- [Architecture Decision Records](docs/decisions/) — Records of significant architectural choices and the reasoning behind them.
