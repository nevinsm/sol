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
| **Autarch** | The human principal of the sol system. Creates writs, commissions caravans, reviews work, makes design decisions. The system and all its agents serve the autarch. From Greek *autarchēs* (self-ruler); cf. Gene Wolfe's *Book of the New Sun*. Identity label: `autarch`. | *(was: operator)* |
| **World** | A project or workspace. Contains agents, writs, and configuration. Each world has its own database and directory tree. | Rig |
| **Outpost** | A worker agent's station within a world. Directory at `$SOL_HOME/{world}/outposts/{agent}/`. | Polecat |
| **Envoy** | A persistent, human-directed agent. Maintains context across sessions via Claude Code's auto-memory (`<envoyDir>/memory/MEMORY.md`). Directory at `$SOL_HOME/{world}/envoys/{name}/`. | Crew |
| **Writ** | A unit of work with a kind (code or analysis). Created in the store, assigned to agents via cast, tracked through tether/resolve lifecycle. Kind determines the resolve path — code writs flow through forge, non-code writs close directly. Stored in per-world database. | *(was: work item)* |
| **Sphere** | The global registry connecting all worlds. Stores agents, messages, escalations, caravans. Database: `sphere.db`. | Town |

## Actions

| Term | Definition | Replaces (Gastown) |
|---|---|---|
| **Cast** | Dispatch work to an agent. Creates a worktree, tethers work, starts a session. From "farcaster" — instantaneous transit. | Sling |
| **Prime** | Inject execution context into a session on startup. Unchanged — already perfect. | Prime |
| **Resolve** | Signal work completion. For code writs: push branch, create MR, clear tether. For non-code writs: close writ, clear tether. | Done |
| **Nudge** | A short message injected into a running agent's session to redirect or unstick it. Queued by the sentinel (or autarch) and drained into the agent's tmux session. CLI: `sol nudge`. | *(new)* |
| **Handoff** | Stop an agent's current session and start a fresh one for the same writ, preserving context via committed code and the tether. Used to recover from context exhaustion or runtime hangs. CLI: `sol handoff`. | *(new)* |

## Primitives

| Term | Definition | Replaces (Gastown) |
|---|---|---|
| **Kind** | A writ's processing classification. Stored as a dedicated column (`TEXT NOT NULL DEFAULT 'code'`). Determines the resolve path, persona generation, and forge involvement. Code writs flow through forge; non-code writs (analysis, review, etc.) close directly. See ADR-0024. | *(new)* |
| **Tether** | The durability primitive. A directory at `$SOL_HOME/{world}/{role}s/{agent}/.tether/` containing one file per bound writ. For outposts, contains a single file; for persistent agents (envoys), may contain multiple files representing concurrent writ bindings. If any tether file exists, work is assigned. See ADR-0025. | Hook |
| **Active Writ** | The writ a persistent agent is currently focused on. Tracked in the sphere DB `agents.active_writ` column. Set by `sol cast`, `sol tether` (first tether only), and `sol writ activate`. Only one writ can be active because Claude Code caches the system prompt at session start. | *(new)* |
| **Charter** | Per-world configuration file (`world.toml`). Defines source repo, agent capacity, model tier, and forge settings. Layered with global `sol.toml`. | *(new in Arc 1)* |
| **Memory** | An envoy's persistent accumulated context. Claude Code auto-memory, loaded from `<envoyDir>/memory/MEMORY.md` at session start via the adapter's `autoMemoryDirectory` setting. Agent-maintained via the `/memory` REPL command or natural-language save. Lives outside the worktree so it survives worktree rebuilds. | *(was: Brief, retired)* |
| **Writ Output Directory** | The delivery surface for non-code writs. Path: `$SOL_HOME/{world}/writ-outputs/{writID}/`. Created at cast time; contents are readable with standard shell tools. See also: `config.WritOutputDir()`. | *(new)* |
| **Account** | A registered AI provider credential (Claude OAuth or API key). Worlds and agents reference accounts by name; the broker probes them for availability and the quota subsystem tracks rate limit state per account. CLI: `sol account`. | *(new)* |
| **Quota** | Per-account rate limit state. Tracks which accounts are throttled by the upstream provider and rotates rate-limited agents to available accounts. CLI: `sol quota`. | *(new)* |
| **Guidelines** | A named template of execution instructions injected into an agent's persona at cast time. Auto-selected by writ kind (e.g. code → default code guidelines) or overridden via `--guidelines`. Stored under `.claude/rules/`. | *(new)* |
| **Persona** | The composite per-session instruction file written to `CLAUDE.local.md` at the worktree root. Combines the agent identity, writ assignment, guidelines, and protocol notes. Read by Claude Code via its upward directory walk. | *(new)* |
| **Persona Template** | A reusable behavioral posture selected at envoy creation via `--persona=<name>`. Resolved through the three-tier lookup in `internal/persona/resolve.go` (project `.sol/personas/{name}.md` → user `$SOL_HOME/personas/{name}.md` → embedded `internal/persona/defaults/`). The resolved template is written to the envoy's `persona.md` file and injected on session start. Distinct from the per-session **Persona** file above: the template is the *source*, the per-session file is the *materialized instance*. See `docs/personas.md`. | *(new)* |
| **Skills** | Reusable capability bundles available to agents at session start. Stored under `.claude/skills/` (excluded from git via the managed-repo exclude list). | *(new)* |
| **Heartbeat** | A periodically-touched file on disk used by background processes (broker, sentinel, prefect, consul) to advertise liveness. Other components read the heartbeat to detect dead processes. | *(new)* |
| **Guard** | A safety check that blocks a destructive or behavior-changing operation unless an explicit confirmation flag is provided. Guards return exit code 2 when they block. | *(new)* |
| **Dash** | Live TUI dashboard for the sphere. Displays real-time agent status, writ progress, and system health. Package: `internal/dash/`. CLI: `sol dash`. | *(new)* |
| **Inbox** | Unified TUI for viewing and acting on escalations and unread mail. Package: `internal/inbox/`. CLI: `sol inbox`. | *(new)* |
| **Mail** | Asynchronous inter-agent and autarch messaging with priority and notification. Enables agents and the autarch to exchange messages outside of session context. CLI: `sol mail send/list/read`. | *(new)* |
| **Escalation** | Agent-initiated request-for-help surfaced in `sol inbox` for the autarch. Created when an agent is stuck and cannot complete its work. ID format: `esc-` + 16 hex chars. CLI: `sol escalate`. | *(new)* |
| **Doctor** | Prerequisite validator. Checks that required dependencies (tmux, git, claude, SOL_HOME, SQLite WAL) are available and correctly configured. Package: `internal/doctor/`. CLI: `sol doctor`. | *(new)* |
| **Budget** | Per-account daily spend tracking and enforcement. Gates dispatch when an account exceeds its configured daily limit. Package: `internal/budget/`. | *(new)* |

## Processes

| Term | Definition | Replaces (Gastown) |
|---|---|---|
| **Prefect** | Sphere-wide orchestrator. Manages agent health across all worlds, respawns dead sessions, detects mass failures. | Supervisor |
| **Forge** | Merge pipeline. Processes merge requests through quality gates, resolves conflicts, integrates output. | Refinery |
| **Sentinel** | Per-world health monitor. Detects stalled/stuck/zombie agents, performs AI-assisted assessment, injects nudges. | Witness |
| **Chronicle** | Event log maintenance. Deduplication, aggregation, feed truncation. | Curator |
| **Ledger** | Sphere-scoped OTel OTLP receiver for agent token tracking. Accepts token usage events from Claude Code agent sessions, writes per-model token_usage records to world databases linked to agent_history entries. | *(new)* |
| **Consul** | System-level patrol. Stale tether recovery, stranded caravan feeding, lifecycle management, heartbeat monitoring. Operates across all worlds. | Deacon |
| **Broker** | Sphere-level health probe for AI provider runtimes (Claude, Codex). Discovers configured runtimes across worlds, probes them on an interval, and surfaces availability status. Runs as a background process with a heartbeat file. | *(new)* |

## Grouping

| Term | Definition | Replaces (Gastown) |
|---|---|---|
| **Caravan** | A batch of related writs dispatched together. Tracks readiness and dependencies across worlds. Items are assigned a **phase** for cross-world sequencing — phase 0 dispatches first, phase N waits for earlier phases to complete. Lifecycle: **drydock** (draft, created via `create`) → **open** (dispatchable, via `commission`) → **closed** (archived, via `close --confirm`). Reverse transitions: `reopen` (closed → drydock), `drydock` (open → drydock). `delete` is terminal from drydock or closed. See the [Caravan Lifecycle diagram in cli.md](cli.md#caravan-lifecycle) for details. | Convoy |

## Sessions

tmux sessions follow the pattern: `sol-{world}-{agentName}`

Example: `sol-myproject-Toast`

## IDs

| Entity | Format | Example |
|---|---|---|
| Writ | `sol-` + 16 hex chars | `sol-a1b2c3d4e5f6a7b8` |
| Merge request | `mr-` + 16 hex chars | `mr-a1b2c3d4e5f6a7b8` |
| Message | `msg-` + 16 hex chars | `msg-a1b2c3d4e5f6a7b8` |
| Escalation | `esc-` + 16 hex chars | `esc-a1b2c3d4e5f6a7b8` |
| Caravan | `car-` + 16 hex chars | `car-a1b2c3d4e5f6a7b8` |
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
| work item | writ |
| crew | envoy |
| mayor | consul (coordination) + sol init (onboarding) |
