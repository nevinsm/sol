# CLI Reference (Deprecated)

This file is no longer maintained. Agent command references are now provided
via role-scoped skills at `.claude/skills/{name}/SKILL.md`.

See ADR-0026 for details.

---

## New Commands

### Agent Stats

| Command | Description |
|---------|-------------|
| `sol agent stats [name] --world=<world>` | Show agent performance metrics. When a name is given, displays detailed stats including casts, cycle time, merge rate, token usage, and estimated cost. Without a name, shows a leaderboard across all agents. Supports `--json`. |

Cost line behavior:
- When `[pricing]` is configured in `sol.toml`, shows `Estimated cost: $X.XX (N models, pricing from sol.toml)`.
- When some models lack pricing entries: `Estimated cost: $X.XX (N unpriced model(s))`.
- When no pricing is configured: `Estimated cost: (no pricing configured)`.

### Consul

| Command | Description |
|---------|-------------|
| `sol consul start` | Start the consul as a background tmux session (`sol-sphere-consul`) |
| `sol consul stop` | Stop the consul background session |
| `sol consul restart` | Restart the consul (stop then start) |
| `sol consul attach` | Attach to the consul tmux session |

### Token Broker

| Command | Description |
|---------|-------------|
| `sol token-broker start` | Start the token broker as a detached background process |
| `sol token-broker stop` | Stop the running token broker (SIGTERM) |
| `sol token-broker restart` | Restart the token broker (stop then start) |

### Prefect

| Command | Description |
|---------|-------------|
| `sol prefect start` | Start the prefect as a detached background process |
| `sol prefect restart` | Restart the prefect (stop then start) |
| `sol prefect status` | Show prefect running state, PID, and uptime (`--json` for machine-readable output) |

### Chronicle

| Command | Description |
|---------|-------------|
| `sol chronicle restart` | Restart the chronicle (stop then start) |
| `sol chronicle status` | Show chronicle running state and checkpoint offset (`--json` for machine-readable output) |

### Ledger

| Command | Description |
|---------|-------------|
| `sol ledger restart` | Restart the ledger (stop then start) |
| `sol ledger status` | Show ledger running state and OTLP port (`--json` for machine-readable output) |

### Sentinel

| Command | Description |
|---------|-------------|
| `sol sentinel restart --world=<world>` | Restart the sentinel (stop then start) |
| `sol sentinel status --world=<world>` | Show sentinel running state. Supports `--json`. |

### Governor

| Command | Description |
|---------|-------------|
| `sol governor restart --world=<world>` | Restart the governor (stop then start) |
| `sol governor status --world=<world>` | Show governor running state, agent state, active writs, tethers, and brief age. Supports `--json`. |

### Envoy

| Command | Description |
|---------|-------------|
| `sol envoy restart <name> --world=<world>` | Restart an envoy session (stop then start) |
| `sol envoy status <name> --world=<world>` | Show envoy running state, agent state, active writ, and brief age. Supports `--json`. |

### Config

| Command | Description |
|---------|-------------|
| `sol config claude` | Launch Claude Code pointed at sphere-level defaults (`$SOL_HOME/.claude-defaults/`). Seeds defaults on first run. Changes propagate to all agents on next start. |

### Senate

| Command | Description |
|---------|-------------|
| `sol senate restart` | Restart the senate (stop then start) |
| `sol senate status` | Show senate running state and brief age. Supports `--json`. |

### Cost

| Command | Description |
|---------|-------------|
| `sol cost` | Show sphere-wide per-world cost totals |
| `sol cost --world <world>` | Show per-agent breakdown within a world |
| `sol cost --agent <name> --world <world>` | Show per-writ breakdown for an agent |
| `sol cost --caravan <id-or-name>` | Show per-writ breakdown across worlds for a caravan |
| `sol cost --since <duration-or-date>` | Filter by time window (e.g., `24h`, `7d`, `2026-03-01`) |
| `sol cost --json` | Machine-readable JSON output |

All `--since`, `--json` flags can be combined with any view flag. Missing pricing in `sol.toml` degrades gracefully to token-count-only display.
