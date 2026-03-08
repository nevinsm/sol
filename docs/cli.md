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

### Writ Trace

| Command | Description |
|---------|-------------|
| `sol writ trace <id>` | Show full trace: timeline, cost, and escalations |
| `sol writ trace <id> --json` | Machine-readable JSON output |
| `sol writ trace <id> --timeline` | Timeline only |
| `sol writ trace <id> --cost` | Cost only |
| `sol writ trace <id> --no-events` | Skip event log scan (faster) |
| `sol writ trace <id> --world <name>` | Skip world auto-resolution |

Data sources queried:
- **World DB**: writ record, agent history, token usage, merge requests, dependencies, labels
- **Sphere DB**: escalations, caravan items, active agents (degrades gracefully if unavailable)
- **Tether filesystem**: active tether files across outposts and envoys
- **Event log**: `.events.jsonl` feed events (skipped with `--no-events`)

Example:
```
$ sol writ trace sol-a1b2c3d4e5f6a7b8
Writ: sol-a1b2c3d4e5f6a7b8
Title: Fix authentication token refresh
Kind: code    Status: closed    Priority: 1
Created: 2026-03-07T10:15:00Z    Closed: 2026-03-07T12:45:00Z    World: haven
Labels: auth, critical

── Timeline ──────────────────────────────────────────────────────────
  10:15:00Z  created        by operator
  10:15:05Z  cast           to Toast
  11:30:00Z  resolved       by Toast
  12:45:00Z  merged         mr-1a2b3c4d5e6f7a8b

── Cost ──────────────────────────────────────────────────────────────
  Model             Input      Output   Cache Read   Cache Write     Cost
  claude-sonnet-4   125,400    48,200      890,000        12,500   $0.47
                                                          Total:   $0.47
  Cycle time: 2h 30m (cast → merge)

── Escalations ───────────────────────────────────────────────────────
  (none)
```

### World Sleep/Wake

| Command | Description |
|---------|-------------|
| `sol world sleep <name>` | Soft sleep: stop services, let agents finish naturally |
| `sol world sleep --force <name>` | Hard sleep: stop services + stop all outpost agents, return writs, warn envoys |
| `sol world wake <name>` | Wake world: start services with per-service verification reporting |
