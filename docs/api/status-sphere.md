# status-sphere

JSON Schema for the `status-sphere` command.

**Schema file**: [status-sphere.schema.json](status-sphere.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `broker` | object | **yes** | broker details |
| `caravans` | object[] | no | List of caravans |
| `chronicle` | object | **yes** | chronicle details |
| `consul` | object | **yes** | consul details |
| `escalations` | object | no | escalations details |
| `health` | string | **yes** | health |
| `ledger` | object | **yes** | ledger details |
| `mail_count` | integer | no | mail count |
| `prefect` | object | **yes** | prefect details |
| `sol_home` | string | **yes** | sol home |
| `tokens` | object | **yes** | tokens details |
| `worlds` | object[] | **yes** | List of worlds |

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
| `last_healthy_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `last_probe_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
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

### consul

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `heartbeat_age` | string | no | heartbeat age |
| `patrol_count` | integer | no | patrol count |
| `running` | boolean | **yes** | running |
| `stale` | boolean | **yes** | stale |

### escalations

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `by_severity` | object | **yes** | by severity |
| `total` | integer | **yes** | total |

### ledger

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `heartbeat_age` | string | no | heartbeat age |
| `pid` | integer | no | pid |
| `port` | integer | no | port |
| `running` | boolean | **yes** | running |
| `stale` | boolean | no | stale |

### prefect

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `pid` | integer | no | pid |
| `running` | boolean | **yes** | running |

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

### worlds

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agents` | integer | **yes** | agents |
| `dead` | integer | **yes** | dead |
| `envoys` | integer | **yes** | envoys |
| `forge` | boolean | **yes** | forge |
| `health` | string | **yes** | health |
| `idle` | integer | **yes** | idle |
| `max_active` | integer | **yes** | max active |
| `mr_failed` | integer | **yes** | mr failed |
| `mr_ready` | integer | **yes** | mr ready |
| `name` | string | **yes** | name |
| `sentinel` | boolean | **yes** | sentinel |
| `sleeping` | boolean | no | sleeping |
| `source_repo` | string | no | source repo |
| `stalled` | integer | **yes** | stalled |
| `working` | integer | **yes** | working |
