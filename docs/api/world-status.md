# world-status

JSON Schema for the `world-status` command.

**Schema file**: [world-status.schema.json](world-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agents` | object[] | **yes** | List of agents |
| `broker` | object | **yes** | broker details |
| `caravans` | object[] | no | List of caravans |
| `chronicle` | object | **yes** | chronicle details |
| `config` | object | **yes** | config details |
| `envoys` | object[] | **yes** | List of envoys |
| `forge` | object | **yes** | forge details |
| `ledger` | object | **yes** | ledger details |
| `max_active` | integer | **yes** | max active |
| `merge_queue` | object | **yes** | merge queue details |
| `merge_requests` | object[] | no | List of merge requests |
| `prefect` | object | **yes** | prefect details |
| `sentinel` | object | **yes** | sentinel details |
| `summary` | object | **yes** | summary details |
| `tokens` | object | **yes** | tokens details |
| `world` | string | **yes** | world |

### agents

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `active_writ` | string | no | active writ |
| `name` | string | **yes** | name |
| `nudge_count` | integer | no | nudge count |
| `session_alive` | boolean | **yes** | session alive |
| `state` | string | **yes** | state |
| `work_title` | string | no | work title |

### broker

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `heartbeat_age` | string | no | heartbeat age |
| `patrol_count` | integer | no | patrol count |
| `provider_health` | string | no | provider health |
| `providers` | object[] | no | List of providers |
| `running` | boolean | **yes** | running |
| `stale` | boolean | **yes** | stale |
| `token_health` | object[] | no | List of token health |

#### broker.providers

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `consecutive_failures` | integer | **yes** | consecutive failures |
| `health` | string | **yes** | health |
| `last_healthy` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `last_probe` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `provider` | string | **yes** | provider |

#### broker.token_health

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `expires_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `handle` | string | **yes** | handle |
| `status` | string | **yes** | status |
| `type` | string | **yes** | type |

### caravans

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `closed_items` | integer | **yes** | closed items |
| `dispatched_items` | integer | **yes** | dispatched items |
| `done_items` | integer | **yes** | done items |
| `id` | string | **yes** | Unique identifier |
| `items` | object[] | no | List of items |
| `name` | string | **yes** | name |
| `phases` | object[] | no | List of phases |
| `ready_items` | integer | **yes** | ready items |
| `status` | string | **yes** | status |
| `total_items` | integer | **yes** | total items |

#### caravans.items

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `assignee` | string | no | assignee |
| `phase` | integer | **yes** | phase |
| `ready` | boolean | **yes** | ready |
| `status` | string | **yes** | status |
| `title` | string | **yes** | title |
| `world` | string | **yes** | world |
| `writ_id` | string | **yes** | Reference to writ |

#### caravans.phases

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `closed` | integer | **yes** | closed |
| `dispatched` | integer | **yes** | dispatched |
| `done` | integer | **yes** | done |
| `phase` | integer | **yes** | phase |
| `ready` | integer | **yes** | ready |
| `total` | integer | **yes** | total |

### chronicle

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `events_processed` | integer | no | events processed |
| `heartbeat_age` | string | no | heartbeat age |
| `pid` | integer | no | pid |
| `running` | boolean | **yes** | running |
| `stale` | boolean | no | stale |

### config

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agents` | object | **yes** | agents details |
| `budget` | object | **yes** | budget details |
| `escalation` | object | **yes** | escalation details |
| `forge` | object | **yes** | forge details |
| `guidelines` | object | no | guidelines |
| `ledger` | object | **yes** | ledger details |
| `sphere` | object | **yes** | sphere details |
| `world` | object | **yes** | world details |
| `writ-clean` | object | **yes** | writ-clean details |

#### config.agents

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `default_runtime` | string | no | default runtime |
| `max_active` | integer | **yes** | max active |
| `model` | string | **yes** | model |
| `models` | object | no | models |
| `name_pool_path` | string | **yes** | name pool path |
| `runtimes` | object | no | runtimes details |

#### config.budget

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `accounts` | object | **yes** | accounts |

#### config.escalation

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `aging_critical` | string | **yes** | aging critical |
| `aging_high` | string | **yes** | aging high |
| `aging_medium` | string | **yes** | aging medium |
| `escalation_threshold` | integer | **yes** | escalation threshold |

#### config.forge

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `gate_timeout` | string | **yes** | gate timeout |
| `quality_gates` | string[] | **yes** | List of quality gates |

#### config.ledger

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `port` | integer | **yes** | port |

#### config.sphere

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `max_sessions` | integer | **yes** | max sessions |

#### config.world

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `branch` | string | **yes** | branch |
| `default_account` | string | no | default account |
| `protected_branches` | string[] | **yes** | List of protected branches |
| `sleeping` | boolean | no | sleeping |
| `source_repo` | string | **yes** | source repo |

#### config.writ-clean

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `retention_days` | integer | **yes** | retention days |

### envoys

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `active_writ` | string | no | active writ |
| `name` | string | **yes** | name |
| `nudge_count` | integer | no | nudge count |
| `session_alive` | boolean | **yes** | session alive |
| `state` | string | **yes** | state |
| `tethered_count` | integer | no | tethered count |
| `work_title` | string | no | work title |

### forge

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `current_mr` | string | no | current mr |
| `current_writ` | string | no | current writ |
| `heartbeat_age` | string | no | heartbeat age |
| `last_error` | string | no | last error |
| `last_merge` | string | no | last merge |
| `merges_total` | integer | no | merges total |
| `merging` | boolean | no | merging |
| `patrol_count` | integer | no | patrol count |
| `paused` | boolean | no | paused |
| `pid` | integer | no | pid |
| `queue_depth` | integer | no | queue depth |
| `running` | boolean | **yes** | running |
| `stale` | boolean | no | stale |
| `status` | string | no | status |

### ledger

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `heartbeat_age` | string | no | heartbeat age |
| `pid` | integer | no | pid |
| `port` | integer | no | port |
| `running` | boolean | **yes** | running |
| `stale` | boolean | no | stale |

### merge_queue

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `claimed` | integer | **yes** | claimed |
| `failed` | integer | **yes** | failed |
| `merged` | integer | **yes** | merged |
| `ready` | integer | **yes** | ready |
| `total` | integer | **yes** | total |

### merge_requests

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | **yes** | Unique identifier |
| `phase` | string | **yes** | phase |
| `title` | string | **yes** | title |
| `writ_id` | string | **yes** | Reference to writ |

### prefect

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `pid` | integer | no | pid |
| `running` | boolean | **yes** | running |

### sentinel

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agents_checked` | integer | no | agents checked |
| `heartbeat_age` | string | no | heartbeat age |
| `patrol_count` | integer | no | patrol count |
| `pid` | integer | no | pid |
| `reaped_count` | integer | no | reaped count |
| `running` | boolean | **yes** | running |
| `stale` | boolean | no | stale |
| `stalled_count` | integer | no | stalled count |
| `status` | string | no | status |

### summary

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `dead` | integer | **yes** | dead |
| `idle` | integer | **yes** | idle |
| `stalled` | integer | **yes** | stalled |
| `total` | integer | **yes** | total |
| `working` | integer | **yes** | working |

### tokens

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_count` | integer | **yes** | agent count |
| `cache_tokens` | integer | **yes** | cache tokens |
| `cost_usd` | number | no | cost usd |
| `input_tokens` | integer | **yes** | input tokens |
| `output_tokens` | integer | **yes** | output tokens |
| `runtime_breakdown` | object[] | no | List of runtime breakdown |

#### tokens.runtime_breakdown

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cache_tokens` | integer | **yes** | cache tokens |
| `cost_usd` | number | no | cost usd |
| `input_tokens` | integer | **yes** | input tokens |
| `output_tokens` | integer | **yes** | output tokens |
| `runtime` | string | **yes** | runtime |
