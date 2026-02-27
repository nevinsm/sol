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
| **World** | A project or workspace. Contains agents, work items, and configuration. Each world has its own database and directory tree. | Rig |
| **Outpost** | A worker agent's station within a world. Directory at `$SOL_HOME/{world}/outposts/{agent}/`. | Polecat |
| **Sphere** | The global registry connecting all worlds. Stores agents, messages, escalations, convoys. Database: `sphere.db`. | Town |

## Actions

| Term | Definition | Replaces |
|---|---|---|
| **Cast** | Dispatch work to an agent. Creates a worktree, tethers work, starts a session. From "farcaster" — instantaneous transit. | Sling |
| **Prime** | Inject execution context into a session on startup. Unchanged — already perfect. | Prime |
| **Resolve** | Signal that work is complete. Push branch, clear tether, stop session. | Done |

## Primitives

| Term | Definition | Replaces |
|---|---|---|
| **Tether** | The durability primitive. A file at `$SOL_HOME/{world}/outposts/{agent}/.tether` that binds an agent to a work item. If the tether exists, the work is assigned. | Hook |
| **Charter** | *(Reserved for future use — per-world configuration file.)* | — |

## Processes

| Term | Definition | Replaces |
|---|---|---|
| **Governor** | Per-world supervisor. Manages agent health, respawns dead sessions, detects mass failures. | Supervisor |
| **Forge** | Merge pipeline. Processes merge requests through quality gates, resolves conflicts, integrates output. | Refinery |
| **Sentinel** | Per-world health monitor. Detects stalled/stuck/zombie agents, performs AI-assisted assessment, injects nudges. | Witness |
| **Chronicle** | Event log maintenance. Deduplication, aggregation, feed truncation. | Curator |
| **Consul** | System-level patrol. Stale tether recovery, stranded caravan feeding, lifecycle management, heartbeat monitoring. Operates across all worlds. | Deacon |

## Grouping

| Term | Definition | Replaces |
|---|---|---|
| **Caravan** | A batch of related work items dispatched together. Tracks readiness and dependencies across worlds. | Convoy |

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

For contributors familiar with the previous naming (gt/Gastown):

| Old | New |
|---|---|
| gt | sol |
| GT_HOME | SOL_HOME |
| rig | world |
| polecat | outpost |
| town | sphere |
| sling | cast |
| done | resolve |
| hook | tether |
| refinery | forge |
| witness | sentinel |
| curator | chronicle |
| deacon | consul |
| convoy | caravan |
| supervisor | governor |
| prime | prime |
