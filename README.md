# sol

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Run 10, 20, 30+ AI coding agents on a repository at the same time.

Sol assigns work to agents, gives each one its own git worktree so they never step on each other, watches for stalls and crashes, merges completed work through quality gates, and recovers automatically when things break.

It's a single Go binary backed by SQLite. No servers, no containers — just tmux sessions and the filesystem.

## Why Sol

- **Crash recovery** — agents keep running their tethered work if supervision dies; completed branches wait safely if the merge queue is down
- **Quality gates** — every branch goes through automated validation before it lands
- **Inspectability** — writs are SQLite rows, tethers are plain files, sessions are tmux; no black boxes
- **Single binary** — no servers, no containers, no infrastructure beyond tmux and git
- **Parallel isolation** — each agent gets its own git worktree; no conflicts, no stepping on each other

## Prerequisites

- **Go 1.21+** (to build from source)
- **tmux** (agent process containers)
- **git** (worktrees, branching, merging)
- **claude** CLI — [Anthropic Claude Code](https://docs.anthropic.com/en/docs/claude-code/getting-started)

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
sol writ create --world=myworld --title="Implement feature X" --description="..."
sol writ create --world=myworld --title="Fix bug Y" --description="..."

# Dispatch work — sol creates an agent, sets up a worktree, and starts a session
sol cast <writ-id> --world=myworld

# Watch an agent work
sol session attach sol-myworld-Toast

# Check on things
sol status myworld
sol writ list --world=myworld

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

### Core

| Concept | What it is |
|---------|-----------|
| **World** | A repository under management. Has its own database, agents, and worktrees. |
| **Writ** | A unit of work assigned to an agent. Describes what needs to be done. |
| **Agent** | A named identity (with work history and state) that runs in a tmux session. |
| **Cast** | Dispatch a writ: create worktree, write tether, start session. |
| **Resolve** | Signal completion: push branch, update state, clear tether. |
| **Forge** | Merge pipeline. Merges completed work through quality gates. |
| **SOL_HOME** | Runtime root directory (env var, default `~/sol`). All state lives here. |

### Supervision

| Concept | What it is |
|---------|-----------|
| **Sentinel** | Per-world health monitor. Detects stalls and stuck agents. |
| **Consul** | Sphere-level patrol. Recovers from failures across all worlds. |
| **Prefect** | Top-level supervisor. Keeps sentinel, forge, and consul running. |

### Advanced

| Concept | What it is |
|---------|-----------|
| **Outpost** | An agent's workspace within a world (`$SOL_HOME/{world}/outposts/{agent}/`). |
| **Envoy** | A persistent human-directed agent with its own worktree and brief (memory). |
| **Governor** | A per-world coordinator that manages work distribution and strategy. |
| **Caravan** | A batch of related writs dispatched as a group. |
| **Brief** | An agent's persistent memory file (`.brief/memory.md`), maintained across sessions. |
| **Managed Repo** | Sol's clone of your repository (`$SOL_HOME/{world}/repo/`). All worktrees branch from here. |
| **Tether** | A directory with per-writ files that tells an agent what to work on. If the tether exists, the agent runs it. Survives crashes. |

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

- **Tethers** — `$SOL_HOME/{world}/outposts/{agent}/.tether/`
- **Managed Repo** — `$SOL_HOME/{world}/repo/`
- **Workflows** — `$SOL_HOME/{world}/outposts/{agent}/.workflow/`
- **Workflow Definitions** — `$SOL_HOME/workflows/{name}/`
- **Events** — `$SOL_HOME/.events.jsonl` (raw), `$SOL_HOME/.feed.jsonl` (curated)
- **World Config** — `$SOL_HOME/{world}/world.toml`
- **Global Config** — `$SOL_HOME/sol.toml`

See [docs/cli.md](docs/cli.md) for the full CLI reference.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, conventions, and architecture.

## Design Documents

- [Manifesto](docs/manifesto.md) — Design philosophy: what we learned from the Gastown prototype, what we're building, why stability is the feature.
- [Failure Modes](docs/failure-modes.md) — Per-component crash recovery, graceful degradation, and mass failure handling.
- [Naming Glossary](docs/naming.md) — Sol naming conventions and migration reference from Gastown.
- [Principles](docs/principles.md) — Core design principles guiding sol's development.
- [Workflows](docs/workflows.md) — Workflow definitions and usage patterns.
- [Integration API](docs/integration-api.md) — Design sketch for stable CLI output and event webhooks.
- [Configuration](docs/configuration.md) — world.toml and sol.toml reference.
- [Operations](docs/operations.md) — Day-to-day operation guide.
- [Troubleshooting](docs/troubleshooting.md) — Diagnosing and fixing common problems.
- [Architecture Decision Records](docs/decisions/) — Records of significant architectural choices and the reasoning behind them.

## License

MIT — see [LICENSE](LICENSE).
