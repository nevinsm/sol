# sol

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Run 10, 20, 30+ AI coding agents on a repository at the same time.

---

You're using AI coding agents. They're good at individual tasks. But when you want five of them working in parallel — on different features, across branches — everything falls apart. Sessions crash and nobody notices. Agents edit the same files and create conflicts. Finished work sits in branches that nobody merges. You spend more time babysitting than building.

Sol fixes this. It gives each agent an isolated git worktree, watches for crashes and stalls, automatically restarts failed sessions, merges completed work through quality gates, and recovers gracefully when things break. It's a single Go binary backed by SQLite. No servers, no containers — just tmux sessions and the filesystem.

## What It Looks Like

```bash
# Set up sol and point it at your repo
sol init --name=myproject --source-repo=git@github.com:org/repo.git

# Create work items
sol writ create --world myproject --title "Add rate limiting to API" --description "Add rate limiting middleware to all public API endpoints"
sol writ create --world myproject --title "Fix timezone handling in scheduler"
sol writ create --world myproject --title "Refactor auth middleware"

# Dispatch all three — each gets its own agent, worktree, and session
sol cast sol-a1b2c3d4e5f6a7b8 --world myproject
sol cast sol-c3d4e5f6a7b8c9d0 --world myproject
sol cast sol-e5f6a7b8c9d0e1f2 --world myproject

# See what's happening
sol status myproject
#   Agents: 3 active (Toast, Sage, Nova)
#   Writs:  3 tethered, 0 pending
#   Forge:  idle, waiting for completed branches

# Watch a specific agent work
sol session attach sol-myproject-Toast

# When agents finish, they call `sol resolve` themselves —
# pushing their branch, clearing their tether, updating the writ.
# The forge picks up completed branches and merges them through
# quality gates into your target branch.

# Start full supervision: health monitoring, auto-merge, crash recovery
sol prefect run --consul
```

You create work, dispatch it, and check back later. Agents that crash get restarted. Agents that stall get detected. Completed work gets merged. You stay in control without staying in the loop.

## Why Sol

**"Why not just run multiple Claude sessions in separate terminals?"**

You can — until one crashes at 2 AM and you don't notice. Until two agents edit the same module and you spend an hour resolving conflicts. Until you have six finished branches and can't remember which ones passed tests. Until an agent's context fills up and it forgets what it was working on.

Sol exists because running multiple agents is easy. *Managing* multiple agents is hard.

- **Crash recovery.** If an agent's session dies, sol detects it and restarts it. The agent reads its tether — a durable file describing the assignment — and picks up where it left off. If sol itself crashes, agents keep running their tethered work independently.
- **Isolation.** Every agent gets its own git worktree branched from the target. No merge conflicts between agents. No stepping on each other's work.
- **Quality gates.** The forge merges completed branches through configurable validation before they land. No untested code gets merged automatically.
- **Inspectability.** Writs are SQLite rows — query them. Tethers are plain files — read them. Sessions are tmux — attach to them. No black boxes, no proprietary state.

## Quick Start

**Prerequisites:** Go 1.24+, tmux, git, [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code/getting-started)

```bash
# Build and install
make build && make install   # binary goes to ~/.local/bin/sol

# Verify prerequisites
sol doctor

# Initialize — creates SOL_HOME (~/sol) and your first world
sol init --name=myproject --source-repo=git@github.com:org/repo.git

# Create a writ and dispatch it
sol writ create --world myproject --title "Implement feature X" --description "Description of the work"
sol cast <writ-id> --world myproject

# Check status
sol status myproject
```

See [docs/cli.md](docs/cli.md) for the full command reference and [docs/operations.md](docs/operations.md) for day-to-day usage.

## How It Works

A **world** is a repository under management. When you create one, sol clones the repo and sets up a database to track work.

You describe work as **writs**. When you **cast** a writ, sol assigns it to an agent, creates an isolated git worktree, writes the assignment to a **tether** file, and starts a Claude session. The agent reads its tether and begins working.

When the agent finishes, it calls `sol resolve` — pushing its branch, updating the writ, and clearing its tether. The **forge** picks up the completed branch and merges it through quality gates.

Behind the scenes, a supervision hierarchy keeps everything running:

```
Prefect
├── Sentinel (per-world)     — health monitoring, stall detection
├── Forge (per-world)        — merge queue, quality gates
├── Consul (sphere-level)    — recovery patrol across all worlds
├── Outposts (per-world)     — worker agents on tethered tasks
└── Envoys (per-world)       — persistent human-directed agents
```

Each component can fail independently. If supervision dies, agents keep running. If the forge is down, completed branches wait safely. The **prefect** restarts any component that crashes.

For ongoing human-directed work, **envoys** provide persistent agents with their own worktrees and memory that survives across sessions.

All state is stored in SQLite databases (WAL mode) — one sphere-wide and one per world — plus plain files on disk. See [docs/configuration.md](docs/configuration.md) for storage layout and [docs/failure-modes.md](docs/failure-modes.md) for crash recovery details.

## Status

Sol is at **v0.1.0**. It works — agents dispatch, execute, and merge real code in production use. But the API surface is not yet stable, edge cases remain, and documentation is still catching up with the implementation.

What works well today: core dispatch loop, crash recovery, forge merge pipeline, multi-world management, envoys.

What's still rough: error messages in some paths, CLI ergonomics for less common operations, documentation gaps.

If you try it and hit problems, check [docs/troubleshooting.md](docs/troubleshooting.md) or open an issue.

## Design Documents

- [Manifesto](docs/manifesto.md) — Design philosophy and what we learned from prototyping
- [Failure Modes](docs/failure-modes.md) — Crash recovery and graceful degradation
- [Principles](docs/principles.md) — Core design principles
- [Configuration](docs/configuration.md) — world.toml and sol.toml reference
- [Operations](docs/operations.md) — Day-to-day operation guide
- [Workflows](docs/workflows.md) — Workflow definitions and patterns
- [CLI Reference](docs/cli.md) — Full command documentation
- [Troubleshooting](docs/troubleshooting.md) — Diagnosing common problems
- [Architecture Decisions](docs/decisions/) — ADRs with reasoning

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, conventions, and architecture.

## License

MIT — see [LICENSE](LICENSE).
