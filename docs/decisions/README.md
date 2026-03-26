# Architecture Decision Records

Index of architecture decision records. Update this file when adding new ADRs.

| # | Title | Status | Summary |
|---|-------|--------|---------|
| 0001 | Sentinel as Go Process with Targeted AI Call-outs | Accepted | Sentinel runs as a deterministic Go process; AI callouts fire only when output hash is unchanged between patrols |
| 0002 | Forge as Go Process | Superseded by ADR-0005 | Initial forge as a pure Go process with no AI judgment; superseded when conflict resolution required intelligence |
| 0003 | AI Assessment Gated by tmux Output Hashing | Accepted | Sentinel gates AI assessments behind tmux output hash comparison so AI calls scale with stuck agents, not total agents |
| 0004 | Chronicle as Separate Component from Event Feed | Accepted | Chronicle runs as an independent process handling event deduplication, aggregation, and feed truncation |
| 0005 | Forge as Claude Session + Go Toolbox | Superseded by ADR-0027 | Forge as Claude session backed by Go CLI subcommands for conflict resolution and test failure attribution |
| 0006 | Prefect Defers Outpost Management to Sentinel | Accepted | When a sentinel is active for a world, prefect defers all outpost supervision to it; sentinel handles respawns and recovery |
| 0007 | Consul as Go Process (Not Full Claude Session) | Accepted | Consul runs as a deterministic Go process following the sentinel pattern; AI callouts added only when heuristics detect trouble |
| 0008 | World Lifecycle with Dual-Store Design | Accepted | `sol world init` is required before any world operation; configuration uses `world.toml` as file-primary source of truth with sphere DB as cache |
| 0009 | Envoy as Context-Persistent Claude Session | Accepted | Envoy role provides persistent human-directed agents with durable context via agent-maintained brief files |
| 0010 | Governor as Per-World Work Coordinator | Accepted | Governor is a per-world Claude session backed by Go subcommands for natural language work dispatch and caravan coordination |
| 0011 | Senate as Sphere-Scoped Planning Session | Superseded by ADR-0029, ADR-0035 | Sphere-scoped Claude session for cross-world writ planning and caravan creation; renamed to Chancellor (ADR-0029), then removed (ADR-0035) |
| 0012 | Charmbracelet Libraries for Terminal UI | Accepted | Adopts lipgloss for terminal styling and huh for interactive prompts across autarch-facing commands |
| 0013 | Brief System for Context Persistence | Accepted | Persistent agents maintain self-authored brief files injected via Claude Code hooks to carry context across sessions and compactions |
| 0014 | Managed World Repository | Accepted | Each world maintains a managed git clone at `$SOL_HOME/{world}/repo/`; all worktrees branch from this clone |
| 0015 | Workflow Manifest and Workflow Types | Superseded by ADR-0032 | Adds inline, manifested, and convoy execution modes and three workflow types (workflow, expansion, convoy). Superseded: unified into single type with two modes. |
| 0016 | Ledger as Sphere-Scoped OTel Receiver for Agent Token Tracking | Accepted | Adds `agent_history` and `token_usage` tables modeled on OTel span/metric hierarchy for per-agent token tracking |
| 0017 | Workflow-Based Forge | Superseded by ADR-0027 | Replaced forge's free-form Claude patrol with a TOML workflow prescribing exact step sequences; superseded when Go process proved sufficient |
| 0018 | Agent Config Directory Isolation | Accepted | Sets `CLAUDE_CONFIG_DIR` per agent to a world-scoped path, isolating auto-memory and session transcripts between agents |
| 0019 | Account & Quota Management | Accepted | Multi-account support with sentinel-managed credential rotation and agent pause/resume on quota exhaustion |
| 0020 | Operational Tooling | Accepted | Adds world export/import/clone commands, schema migration visibility (`sol schema status`), and multi-world prefect filtering via `--worlds` |
| 0021 | Three-Tier Workflow Resolution | Accepted | Workflow resolution follows project tier (`.sol/workflows/`) > user tier (`$SOL_HOME/workflows/`) > embedded binary defaults |
| 0022 | Token Broker — Centralized OAuth Refresh | Accepted | Token broker centralizes OAuth refresh token handling; agents receive access-token-only credentials, eliminating refresh-token race conditions |
| 0023 | Unified Agent Startup and Role-Specific System Prompts | Accepted | Centralized `internal/startup` package with a 9-step launch sequence and role-specific system prompt strategy (replace for autonomous, append for interactive) |
| 0024 | Writ Kind — Code vs Analysis Resolve Paths | Accepted | Adds `kind` column to writs; code writs push branches through forge, non-code (analysis) writs close directly without MR creation |
| 0025 | Tether Evolution — Multi-Writ Persistent Agents | Accepted | Tether becomes a directory enabling multiple concurrent bound writs; active writ tracked in sphere DB; `sol tether`/`sol untether` for persistent agents |
| 0026 | Agent Skills — Progressive Disclosure for Tool Education | Accepted | Replaces monolithic CLI reference with role-scoped skill files loaded on demand via Claude Code's skills system |
| 0027 | Forge as Deterministic Go Process | Superseded by ADR-0028 | Forge as a deterministic Go process with targeted AI callouts at failure points only; superseded by event-driven orchestrator design |
| 0028 | Event-Driven Forge with Go Orchestration Shell | Accepted | Forge becomes a Go orchestration shell that starts ephemeral Claude sessions per merge task for inline conflict resolution |
| 0029 | Rename Senate to Chancellor | Superseded by ADR-0035 | Senate component renamed to Chancellor; entire role later removed in ADR-0035 |
| 0030 | Split Store into WorldStore and SphereStore | Accepted | Splits `*Store` into `*WorldStore` and `*SphereStore` as distinct types for compile-time database boundary enforcement |
| 0031 | Runtime Adapter Interface | Accepted | Defines `RuntimeAdapter` interface in `internal/adapter/` to abstract Claude-specific startup primitives and enable future runtime support |
| 0035 | Remove Chancellor Role | Accepted | Chancellor role removed entirely; planning is an envoy function via persona templates and cross-world CLI access |

## Superseded ADRs

- **ADR-0002** (forge as Go process) — superseded by ADR-0005 (Claude session), which was revised by ADR-0017 (workflow-based), then superseded by ADR-0027 (deterministic Go), then superseded by ADR-0028 (Go orchestration shell + ephemeral sessions)
- **ADR-0005** (forge as Claude session + Go toolbox) — superseded by ADR-0027
- **ADR-0017** (workflow-based forge) — superseded by ADR-0027
- **ADR-0027** (forge as deterministic Go process) — superseded by ADR-0028
- **ADR-0015** (workflow manifest and workflow types) — superseded by ADR-0032 (workflow type unification)
- **ADR-0011** (senate as sphere-scoped planner) — superseded by ADR-0029 (Rename Senate to Chancellor), then ADR-0035 (Remove Chancellor Role)
- **ADR-0029** (rename senate to chancellor) — superseded by ADR-0035 (Remove Chancellor Role)
