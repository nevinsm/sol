# Sol — Naming Glossary

## Theme

Space-faring civilization. A commonwealth of worlds connected through a
datasphere. Draws from Pandora's Star (Commonwealth, worlds, governance) and
Hyperion (datasphere, farcasters, consuls). The governance layer names the
structure; the action layer names the mechanisms.

## System

| Term | Definition |
|---|---|
| **Sol** | The system itself. Origin star, center of the commonwealth. Single binary, single source of truth. CLI: `sol`. |
| **SOL_HOME** | Runtime root directory. Default `~/sol`. All state lives under this tree. |

## Structure

| Term | Definition | Replaces |
|---|---|---|
| **World** | A project or workspace. Contains agents, work items, and configuration. Each world has its own database and directory tree. | World |
| **Outpost** | A worker agent's station within a world. Directory at `$SOL_HOME/{world}/outposts/{agent}/`. | Outpost |
| **Sphere** | The global registry connecting all worlds. Stores agents, messages, escalations, caravans. Database: `sphere.db`. | Sphere |

## Actions

| Term | Definition | Replaces |
|---|---|---|
| **Cast** | Dispatch work to an agent. Creates a worktree, tethers work, starts a session. From "farcaster" — instantaneous transit. | Cast |
| **Prime** | Inject execution context into a session on startup. Unchanged — already perfect. | Prime |
| **Resolve** | Signal that work is complete. Push branch, clear tether, stop session. | Done |

## Primitives

| Term | Definition | Replaces |
|---|---|---|
| **Tether** | The durability primitive. A file at `$SOL_HOME/{world}/outposts/{agent}/.tether` that binds an agent to a work item. If the tether exists, the work is assigned. | Tether |
| **Charter** | Per-world configuration file (`world.toml`). Defines source repo, agent capacity, model tier, and forge settings. Layered with global `sol.toml`. | `world.toml` |

## Processes

| Term | Definition | Replaces |
|---|---|---|
| **Prefect** | Sphere-wide prefect. Manages agent health across all worlds, respawns dead sessions, detects mass failures. | Prefect |
| **Forge** | Merge pipeline. Processes merge requests through quality gates, resolves conflicts, integrates output. | Forge |
| **Sentinel** | Per-world health monitor. Detects stalled/stuck/zombie agents, performs AI-assisted assessment, injects nudges. | Sentinel |
| **Chronicle** | Event log maintenance. Deduplication, aggregation, feed truncation. | Chronicle |
| **Consul** | System-level patrol. Stale tether recovery, stranded caravan feeding, lifecycle management, heartbeat monitoring. Operates across all worlds. | Consul |

## Grouping

| Term | Definition | Replaces |
|---|---|---|
| **Caravan** | A batch of related work items dispatched together. Tracks readiness and dependencies across worlds. | Caravan |

## Sessions

tmux sessions follow the pattern: `sol-{world}-{agentName}`

Example: `sol-myproject-Toast`

## IDs

| Entity | Format | Example |
|---|---|---|
| Work item | `sol-` + 8 hex chars | `sol-a1b2c3d4` |
| Merge request | `mr-` + 8 hex chars | `mr-a1b2c3d4` |
| Message | `msg-` + 8 hex chars | `msg-a1b2c3d4` |
| Escalation | `esc-` + 8 hex chars | `esc-a1b2c3d4` |
| Caravan | `car-` + 8 hex chars | `car-a1b2c3d4` |
| Agent | `{world}/{name}` | `myproject/Toast` |

## Migration Reference

For contributors familiar with the previous naming (sol/Gastown):

| Old | New |
|---|---|
| sol | sol |
| SOL_HOME | SOL_HOME |
| world | world |
| outpost | outpost |
| sphere | sphere |
| cast | cast |
| done | resolve |
| tether | tether |
| forge | forge |
| sentinel | sentinel |
| chronicle | chronicle |
| consul | consul |
| caravan | caravan |
| prefect | prefect |
| prime | prime |
