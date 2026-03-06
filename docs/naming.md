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

| Term | Definition | Replaces (Gastown) |
|---|---|---|
| **World** | A project or workspace. Contains agents, work items, and configuration. Each world has its own database and directory tree. | Rig |
| **Outpost** | A worker agent's station within a world. Directory at `$SOL_HOME/{world}/outposts/{agent}/`. | Polecat |
| **Envoy** | A persistent, human-directed agent. Maintains context across sessions via a brief. Directory at `$SOL_HOME/{world}/envoys/{name}/`. | Crew |
| **Governor** | Per-world work coordinator. Singleton Claude session that handles natural language dispatch, caravan creation, and cast coordination. Directory at `$SOL_HOME/{world}/governor/`. | Mayor (partial) |
| **Senate** | Sphere-scoped work planner. Claude session for cross-world planning — creates work items, caravans, and dependencies across worlds. Queries governors for world context. Directory at `$SOL_HOME/senate/`. | *(new in Arc 4)* |
| **Sphere** | The global registry connecting all worlds. Stores agents, messages, escalations, caravans. Database: `sphere.db`. | Town |

## Actions

| Term | Definition | Replaces (Gastown) |
|---|---|---|
| **Cast** | Dispatch work to an agent. Creates a worktree, tethers work, starts a session. From "farcaster" — instantaneous transit. | Sling |
| **Prime** | Inject execution context into a session on startup. Unchanged — already perfect. | Prime |
| **Resolve** | Signal that work is complete. Push branch, clear tether, stop session. | Done |
| **Debrief** | Clear an envoy's or governor's brief, giving a fresh start. CLI: `sol envoy debrief`. | *(new in Arc 3)* |

## Primitives

| Term | Definition | Replaces (Gastown) |
|---|---|---|
| **Tether** | The durability primitive. A file at `$SOL_HOME/{world}/outposts/{agent}/.tether` that binds an agent to a work item. If the tether exists, the work is assigned. | Hook |
| **Charter** | Per-world configuration file (`world.toml`). Defines source repo, agent capacity, model tier, and forge settings. Layered with global `sol.toml`. | *(new in Arc 1)* |
| **Brief** | An envoy's, governor's, or senate's accumulated context. Agent-maintained file at `.brief/memory.md`. Injected on session start and after compaction, save-checked on stop. GLASS-inspectable. | *(new in Arc 3)* |
| **World Summary** | Governor-maintained external-facing summary of a world. Structured file at `.brief/world-summary.md` with prescribed sections (Project, Architecture, Priorities, Constraints). Read by Senate and operators via `sol world summary`. | *(new in Arc 3)* |

## Processes

| Term | Definition | Replaces (Gastown) |
|---|---|---|
| **Prefect** | Sphere-wide orchestrator. Manages agent health across all worlds, respawns dead sessions, detects mass failures. | Supervisor |
| **Forge** | Merge pipeline. Processes merge requests through quality gates, resolves conflicts, integrates output. | Refinery |
| **Sentinel** | Per-world health monitor. Detects stalled/stuck/zombie agents, performs AI-assisted assessment, injects nudges. | Witness |
| **Chronicle** | Event log maintenance. Deduplication, aggregation, feed truncation. | Curator |
| **Ledger** | Sphere-scoped OTel OTLP receiver for agent token tracking. Accepts token usage events from Claude Code agent sessions, writes per-model token_usage records to world databases linked to agent_history entries. | *(new)* |
| **Consul** | System-level patrol. Stale tether recovery, stranded caravan feeding, lifecycle management, heartbeat monitoring. Operates across all worlds. | Deacon |

## Grouping

| Term | Definition | Replaces (Gastown) |
|---|---|---|
| **Caravan** | A batch of related work items dispatched together. Tracks readiness and dependencies across worlds. Items are assigned a **phase** for cross-world sequencing — phase 0 dispatches first, phase N waits for earlier phases to complete. | Convoy |

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

For contributors familiar with the Gastown prototype naming:

| Gastown (old) | Sol (current) |
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
| supervisor | prefect |
| prime | prime (unchanged) |
| crew | envoy |
| mayor | governor (dispatch) + senate (cross-world planning) + consul (coordination) + sol init (onboarding) |
