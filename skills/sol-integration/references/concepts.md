# Sol Conceptual Model

This document explains the core concepts in sol for external collaborators.

## Sphere

The **sphere** is the top-level runtime. There is one sphere per machine. It contains worlds, sphere-level processes (prefect, consul, broker, ledger), and global configuration.

The sphere is started with `sol up` and stopped with `sol down`. When running, the prefect process monitors all worlds and respawns failed agent sessions.

Configuration lives at `$SOL_HOME/sol.toml`. The sphere database (`sphere.db`) stores agents, messages, escalations, and caravans.

## World

A **world** maps to a single git repository. Each world has:

- A **managed repo** — a clone of the source repository that sol uses as the basis for all worktrees
- A **world database** — stores writs and world-specific state
- A **forge** — the merge pipeline (see below)
- A **sentinel** — a health monitor that watches for issues
- **Agents** — AI coding sessions bound to the world
- **Configuration** — `world.toml` with settings like source repo, target branch, quality gates

Worlds are created with `sol world init` and listed via `sol status`.

## Writ

A **writ** is a unit of work. It has a lifecycle:

1. **open** — created but not assigned to any agent
2. **tethered** — bound to an agent (assigned via `sol cast`)
3. **working** — the agent is actively executing the writ
4. **done** — the agent has resolved; work is complete and awaiting merge
5. **closed** — the forge has merged the work (or the writ was manually closed)

Writs have two kinds:

- **code** — produces a git branch and merge request. The forge merges these.
- **analysis** — produces a report or investigation. No branch or merge.

Writs can have:

- **Priority** — 1 (critical), 2 (normal), 3 (low)
- **Labels** — arbitrary tags for organization
- **Dependencies** — writs that must close before this writ can be dispatched

As an external collaborator, you create writs and dispatch them. You do not resolve them — that is the agent's job.

## Agent

An **agent** is an AI coding session managed by sol. There are two types:

- **Envoy** — a persistent, human-directed agent. Envoys can work on multiple writs over time and maintain persistent memory. They are created explicitly and live until deleted.
- **Outpost** — a disposable, single-writ executor. When you dispatch a writ with `sol cast`, sol creates an outpost, gives it an isolated git worktree, and starts an AI session. When the outpost resolves its writ, it stops and its worktree is cleaned up.

Each agent has a tmux session where the AI process runs. You should never interact with these sessions directly — sol manages their lifecycle.

Agents can communicate via **mail** (asynchronous messages) and **nudges** (messages injected on session start).

## Forge

The **forge** is a per-world merge pipeline. When an agent resolves a code writ, the resulting branch enters the forge queue. The forge:

1. Takes the next branch from the queue
2. Rebases it onto the target branch
3. Runs quality gates (build, test, lint — configured in `world.toml`)
4. If all gates pass, merges to the target branch
5. If gates fail, the forge may retry or report the failure

You never interact with the forge directly. You can observe its state with `sol forge queue --world=<world>`.

## Caravan

A **caravan** is a batch of related writs with phase-based sequencing. Caravans enable multi-step projects:

- Items are assigned to **phases** (0, 1, 2, ...)
- Items in the same phase can run in **parallel**
- Phase N does not start until **all items in prior phases are closed** (merged)
- Caravans must be **commissioned** before they become dispatchable

Example workflow:

```
Phase 0: "Add database schema" (must merge first)
Phase 1: "Add API endpoints", "Add validation" (can run in parallel after phase 0)
Phase 2: "Add integration tests" (runs after phase 1 items merge)
```

Create a caravan, assign phases, then commission it. Sol handles the sequencing — dispatching items as their phase becomes eligible.

## Tether

A **tether** is the internal binding between an agent and a writ. When `sol cast` dispatches a writ, it creates a tether. When the agent resolves, the tether is cleared.

You do not create or manage tethers directly. They are an internal mechanism — mentioned here so you understand what "tethered" means in writ status output.

## Sentinel

The **sentinel** is a per-world health monitor. It watches for issues like stalled agents, failed forge runs, and unhealthy state. It runs as a background process and reports findings via `sol status <world>`.

## Consul

The **consul** is a sphere-level patrol process. It scans for stale tethers (agents that died without resolving), stranded caravans, and other sphere-wide health concerns.

## Key Directories

- `$SOL_HOME/` — runtime root (default: `~/sol`)
- `$SOL_HOME/<world>/repo/` — managed git clone
- `$SOL_HOME/<world>/outposts/<name>/worktree/` — agent worktrees
- `$SOL_HOME/<world>/envoys/<name>/worktree/` — envoy worktrees
- `$SOL_HOME/<world>/world.toml` — world configuration

Do not modify these directories directly. Use sol CLI commands.
