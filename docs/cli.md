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
| `sol consul attach` | Attach to the consul tmux session |

### Token Broker

| Command | Description |
|---------|-------------|
| `sol token-broker start` | Start the token broker as a detached background process |
| `sol token-broker stop` | Stop the running token broker (SIGTERM) |

### Prefect

| Command | Description |
|---------|-------------|
| `sol prefect start` | Start the prefect as a detached background process |
| `sol prefect status` | Show prefect running state, PID, and uptime (`--json` for machine-readable output) |

### Chronicle

| Command | Description |
|---------|-------------|
| `sol chronicle status` | Show chronicle running state and checkpoint offset (`--json` for machine-readable output) |

### Ledger

| Command | Description |
|---------|-------------|
| `sol ledger status` | Show ledger running state and OTLP port (`--json` for machine-readable output) |
