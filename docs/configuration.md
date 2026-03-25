# Configuration Reference

Sol uses layered TOML configuration files. Two files are involved:

- **`$SOL_HOME/sol.toml`** — sphere-level (global) configuration
- **`$SOL_HOME/{world}/world.toml`** — per-world configuration

## Layering Semantics

Configuration is resolved in three layers, each overriding the previous:

1. **Hardcoded defaults** — built-in values defined in `DefaultWorldConfig()`
2. **`sol.toml`** — sphere-level overrides, applied next
3. **`world.toml`** — per-world overrides, applied last

Missing files are not errors — they are silently skipped. This means you can use only `sol.toml`, only `world.toml`, both, or neither, and sol will always resolve a complete configuration from the hardcoded defaults.

---

## Configuration Sections

### `[world]`

World identity and source control settings. Configured in `world.toml`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `source_repo` | string | `""` | URL or local path of the source repository to clone for this world. Required when using `sol world init` with an explicit repo. |
| `branch` | string | `"main"` | The integration branch. Required (and must be non-empty) when `source_repo` is set. |
| `protected_branches` | string array | `[]` | Branch names that agents must not push to directly. |
| `sleeping` | bool | `false` | When `true`, the world is in sleep mode — no new work is dispatched. |
| `default_account` | string | `""` | Default billing account identifier for cost attribution. |

---

### `[agents]`

Agent pool and model settings.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `max_active` | int | `0` | Maximum number of concurrent active agents per world. `0` means unlimited. Must be `>= 0`. |
| `name_pool_path` | string | `""` | Path to a custom name pool file for agent names. Empty uses the embedded default pool. |
| `model_tier` | string | `"sonnet"` | Default model tier for all agents. Valid values: `sonnet`, `opus`, `haiku`. |
| `default_runtime` | string | `""` | Default runtime adapter for all agents. Valid values: `claude`. Empty falls back to `"claude"`. |

> **Migration note:** The `agents.capacity` field was removed. Use `agents.max_active` instead. Existing configs with `capacity` will silently ignore the field.

---

### `[agents.models]`

Per-role model overrides. Each key overrides `agents.model_tier` for that specific role. Empty means no override (falls back to `model_tier`).

Valid values for all fields: `sonnet`, `opus`, `haiku`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `outpost` | string | `""` | Model tier for outpost (coding) agents. |
| `envoy` | string | `""` | Model tier for envoy (human-interface) agents. |
| `governor` | string | `""` | Model tier for governor (work coordinator) agents. |
| `forge` | string | `""` | Model tier for forge (merge pipeline) agents. |

---

### `[agents.runtimes]`

Per-role runtime overrides. Each key overrides `agents.default_runtime` for that specific role. Empty means no override (falls back to `default_runtime`, then `"claude"`).

Valid values for all fields: `claude`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `outpost` | string | `""` | Runtime for outpost agents. |
| `envoy` | string | `""` | Runtime for envoy agents. |
| `governor` | string | `""` | Runtime for governor agents. |
| `forge` | string | `""` | Runtime for forge agents. |
| `chancellor` | string | `""` | Runtime for chancellor (cross-world planner) agents. |

---

### `[sphere]`

Sphere-level concurrency settings. Configured only in `sol.toml` (not `world.toml`).

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `max_sessions` | int | `0` | Maximum number of concurrent sessions across all worlds. `0` means unlimited. Must be `>= 0`. |

---

### `[forge]`

Merge pipeline quality gate settings.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `quality_gates` | string array | `[]` | Shell commands to run as quality gates before merging. All commands must exit 0 for the merge to proceed. |
| `gate_timeout` | string | `"5m"` | Maximum time allowed for each quality gate command. Must be a valid Go duration string (e.g., `"5m"`, `"2m30s"`). |

---

### `[ledger]`

Ledger telemetry receiver settings. Sphere-scoped; typically configured in `sol.toml`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `port` | int | `4318` | TCP port for the OTLP receiver that collects agent token usage. `0` disables the ledger. |

---

### `[writ-clean]`

Writ output directory retention settings.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `retention_days` | int | `15` | Number of days to retain writ output directories before cleanup. `0` uses the default of `15`. |

---

### `[escalation]`

Escalation aging and alerting settings. Sphere-scoped; configured in `sol.toml` under `[escalation]`.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `aging_critical` | string | `"30m"` | Re-notification interval for critical-severity escalations. Must be a valid Go duration string. |
| `aging_high` | string | `"2h"` | Re-notification interval for high-severity escalations. Must be a valid Go duration string. |
| `aging_medium` | string | `"8h"` | Re-notification interval for medium-severity escalations. Must be a valid Go duration string. |
| `escalation_threshold` | int | `5` | Number of unresolved escalations that triggers a buildup alert. |

Low-severity escalations are never re-notified regardless of this configuration.

---

### `[sitrep]`

Sitrep AI assessment settings.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `model` | string | `"claude-sonnet-4-6"` | Claude model ID to use for sitrep assessments. |
| `assess_command` | string | `"claude"` | Base CLI command used to invoke the sitrep assessment. |
| `timeout` | string | `"60s"` | Maximum time allowed for a sitrep assessment. Must be a valid Go duration string. |

---

### `[pricing]`

Token pricing configuration. Configured in `sol.toml` only (not inherited by per-world config). Maps model names (as they appear in token usage records) to per-token costs in dollars per million tokens.

Each entry is a TOML table keyed by model name:

```toml
[pricing."<model-name>"]
input         = <float>  # dollars per million input tokens
output        = <float>  # dollars per million output tokens
cache_read    = <float>  # dollars per million cache read tokens
cache_creation = <float> # dollars per million cache creation tokens
```

Models with no pricing entry are excluded from cost calculations and reported as "unpriced".

---

## Annotated Examples

### `world.toml` — complete example

```toml
# $SOL_HOME/{world}/world.toml
# Per-world configuration. Overrides sol.toml and hardcoded defaults.

[world]
# URL or path of the source repository for this world.
source_repo = "https://github.com/example/myproject"

# Integration branch. Must be non-empty when source_repo is set.
branch = "main"

# Branches that agents may not push to directly.
protected_branches = ["main", "release"]

# Set to true to pause work dispatch for this world.
sleeping = false

# Default billing account for cost attribution.
default_account = "team-backend"

[agents]
# Maximum concurrent active agents (0 = unlimited).
max_active = 4

# Path to a custom name pool file. Empty = use built-in pool.
name_pool_path = ""

# Default model tier: sonnet, opus, or haiku.
model_tier = "sonnet"

# Default runtime adapter for all agents.
default_runtime = "claude"

[agents.models]
# Per-role model overrides. Empty = use agents.model_tier.
outpost   = "opus"    # coding agents get a more capable model
envoy     = "sonnet"
governor  = "sonnet"
forge     = "sonnet"

[agents.runtimes]
# Per-role runtime overrides. Empty = use agents.default_runtime.
outpost    = "claude"
envoy      = "claude"
governor   = "claude"
forge      = "claude"
chancellor = "claude"

[forge]
# Shell commands run as quality gates before merging.
# All must exit 0 for the merge to proceed.
quality_gates = [
  "make build",
  "make test",
]

# Maximum time allowed per quality gate command.
gate_timeout = "5m"

[ledger]
# OTLP receiver port for agent token tracking. 0 = disabled.
# Sphere-scoped; prefer setting this in sol.toml.
port = 4318

[writ-clean]
# Days to retain writ output directories. 0 = use default (15).
retention_days = 15

[sitrep]
# Claude model ID for sitrep assessments.
model = "claude-sonnet-4-6"

# CLI command used to invoke sitrep assessments.
assess_command = "claude"

# Maximum time for a sitrep assessment.
timeout = "60s"
```

---

### `sol.toml` — complete example

```toml
# $SOL_HOME/sol.toml
# Sphere-level (global) configuration. Applies to all worlds unless
# overridden by a per-world world.toml.

[sphere]
# Maximum concurrent sessions across all worlds (0 = unlimited).
max_sessions = 0

[agents]
# Default model tier for all agents across all worlds.
model_tier = "sonnet"

# Default runtime adapter.
default_runtime = "claude"

[ledger]
# OTLP receiver port. Set to 0 to disable the ledger globally.
port = 4318

[escalation]
# Re-notification intervals for unresolved escalations.
aging_critical = "30m"
aging_high     = "2h"
aging_medium   = "8h"

# Number of unresolved escalations that triggers a buildup alert.
escalation_threshold = 5

[sitrep]
model          = "claude-sonnet-4-6"
assess_command = "claude"
timeout        = "60s"

# Token pricing for cost tracking.
# Keys are model names as they appear in token usage records.
# Values are costs in dollars per million tokens.
[pricing."claude-sonnet-4-5"]
input          = 3.00
output         = 15.00
cache_read     = 0.30
cache_creation = 3.75

[pricing."claude-opus-4-5"]
input          = 15.00
output         = 75.00
cache_read     = 1.50
cache_creation = 18.75

[pricing."claude-haiku-4-5"]
input          = 0.80
output         = 4.00
cache_read     = 0.08
cache_creation = 1.00
```
