# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - Unreleased

Initial public release. Sol is a multi-agent orchestration system for coordinating concurrent AI coding agents on a shared repository.

### Added

#### Core

- **World management** — `sol world init`, `sol world delete`, `sol world export`, `sol world import` for managing repositories under orchestration
- **Writ lifecycle** — `sol writ create`, `sol writ list`, `sol writ close`, `sol writ activate` for tracking units of work with status, labels, and descriptions
- **Writ dependencies** — `sol writ dep add/remove/list` for declaring ordering constraints between writs
- **Agent management** — Named agent identities with persistent state, work history (`sol agent history`), cost tracking (`sol agent stats`), and postmortem analysis (`sol agent postmortem`)
- **Git worktree isolation** — Each agent gets its own worktree branched from the target, preventing conflicts between concurrent agents
- **Tether-based work assignment** — Durable file-based tethers (`$SOL_HOME/{world}/{role}s/{agent}/.tether/`) that survive session crashes and context loss
- **Dispatch** (`sol cast`) — Assigns a writ to an agent, creates an isolated worktree, and starts an AI session in one atomic operation
- **Resolve** (`sol resolve`) — Agents signal completion: pushes branch, clears tether, updates writ status
- **Escalation** (`sol escalate`) — Agents signal when stuck; escalations route to log, mail, or webhook
- **Session management** — `sol session attach`, `sol session stop` for inspecting and controlling tmux-based agent sessions
- **First-time setup** (`sol init`) — Interactive or flag-driven initialization of SOL_HOME and first world

#### Supervision

- **Prefect** (`sol prefect run`) — Sphere-wide supervisor that restarts crashed sessions, runs health checks, and manages all background components
- **Sentinel** (`sol sentinel run`) — Per-world health monitor with stall detection; deterministic Go process with targeted AI callouts
- **Consul** (`sol consul run`) — Sphere-level recovery patrol for stale tethers, stranded caravans, and escalation management
- **Forge** (`sol forge run`) — Per-world merge pipeline that processes completed branches through configurable quality gates before merging to the target branch
- **Forge controls** — `sol forge pause`, `sol forge resume`, `sol forge queue`, `sol forge blocked`, `sol forge log`, `sol forge mark-failed` for merge pipeline management
- **Chronicle** — Event log maintenance with heartbeat monitoring
- **Unified startup** (`sol up`) — Starts all supervision components for a world in one command

#### Agent Types

- **Outpost agents** — Autonomous workers dispatched to writs via `sol cast`; execute independently in isolated worktrees
- **Envoy agents** (`sol envoy start`) — Persistent human-directed agents with their own worktrees and brief (memory) system for exploratory or ongoing work

#### Organization

- **Caravans** (`sol caravan create`, `sol caravan list`, `sol caravan close`) — Batch related writs with phase-based sequencing across worlds
- **Caravan dependencies** (`sol caravan dep`) — Phase ordering within caravans
- **Labels and filtering** — Label writs for categorization; filter writ lists by label, status, or agent
- **Writ tracing** (`sol writ trace`) — Trace writ lineage and dependency chains

#### Workflows

- **Manifest-mode workflows** — Six built-in workflows: `rule-of-five`, `code-review`, `plan-review`, `guided-design`, `prd-review`, `security-audit`
- **Three-tier resolution** — Workflow lookup: project (`.sol/workflows/`) > user (`$SOL_HOME/workflows/`) > embedded (compiled into binary); first match wins
- **Workflow management** — `sol workflow list`, `sol workflow run`, `sol workflow eject`, `sol workflow init` for discovering, executing, customizing, and scaffolding workflows
- **Variable substitution** — Workflows support configurable variables and target writ binding

#### Multi-Runtime Support

- **Claude Code adapter** (primary) — Full integration with Claude Code CLI including persona injection, skill installation, hook support, and OTel telemetry
- **OpenAI Codex adapter** — Alternative runtime adapter for Codex-based agents
- **Runtime-scoped model configuration** — Per-world and global model settings via `world.toml` and `sol.toml`
- **Adapter registry** — Pluggable runtime architecture for adding new AI agent backends

#### Infrastructure

- **SQLite storage** (WAL mode) — `sphere.db` for sphere-wide state (agents, accounts); per-world `{world}.db` for writs, merge requests, and events
- **OTLP telemetry receiver** (Ledger) — Sphere-scoped OpenTelemetry OTLP receiver for tracking agent token usage and costs
- **Doctor** (`sol doctor`) — Prerequisite validation: checks tmux, git, claude CLI, jq, SOL_HOME, SQLite WAL support, env file permissions
- **Configuration** — Layered TOML configuration via `world.toml` (per-world) and `sol.toml` (global); quality gates, runtime selection, model defaults
- **Status display** (`sol status`) — Sphere overview and per-world detail with lipgloss-styled terminal rendering
- **Dashboard** (`sol dash`) — Interactive TUI for monitoring sphere and world state
- **Event feed** (`sol feed`) — Live event stream for observing system activity
- **Brief system** — Agent-maintained context files (`.brief/memory.md`) persisted across sessions via hook-based injection
- **Mail system** (`sol mail`, `sol inbox`) — Inter-agent and operator-to-agent messaging
- **Cost tracking** (`sol cost`) — Token usage and cost reporting across agents and worlds
- **Quota management** (`sol quota`) — Budget controls for agent spending
- **Account/broker system** — API key management with credential rotation and per-agent account binding
- **Guard system** (`sol guard`) — Configurable pre-dispatch checks
- **Handoff** (`sol handoff`) — Session cycling when agent context runs long; committed code survives automatically
- **Nudge** (`sol nudge`) — Prompt a running agent to refocus or change direction
- **Schema management** (`sol schema`) — Database migration tooling
- **CLI documentation generation** (`sol docs generate`, `sol docs validate`) — Auto-generate and validate CLI reference docs
- **Dry-run confirmation pattern** — Destructive commands preview changes without `--confirm`; `--force` for behavioral escalation

### Known Limitations

- **API surface not yet stable** — Command names, flags, and configuration keys may change in future releases
- **CLI ergonomics still evolving** — Error messages in some paths could be more helpful; less common operations may feel rough
- **Documentation gaps** — Some features are better documented than others; the codebase is ahead of the docs
- **Single-machine only** — Sol runs on one host; no distributed coordination across machines
- **tmux dependency** — Sessions require tmux, which does not run natively on Windows (Linux and macOS only)
- **SQLite on network filesystems** — WAL mode requires a local filesystem; NFS and SMB mounts are not supported (see `sol doctor` for detection)

[0.1.0]: https://github.com/nevinsm/sol/releases/tag/v0.1.0
