# CLI Reference (Deprecated)

This file is no longer maintained. Agent command references are now provided
via role-scoped skills at `.claude/skills/{name}/SKILL.md`.

See ADR-0026 for details.

---

## New Commands

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

### Senate

| Command | Description |
|---------|-------------|
| `sol senate restart` | Restart the senate (stop then start) |
| `sol senate status` | Show senate running state and brief age. Supports `--json`. |
