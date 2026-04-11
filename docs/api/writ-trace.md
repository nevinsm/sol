# writ-trace

JSON Schema for the `writ-trace` command.

**Schema file**: [writ-trace.schema.json](writ-trace.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `active_agents` | object[] | **yes** | List of active agents |
| `caravan_items` | object[] | **yes** | List of caravan items |
| `caravans` | object | no | caravans |
| `cost` | object | no | cost details |
| `degradations` | string[] | no | List of degradations |
| `dependencies` | string[] | **yes** | List of dependencies |
| `dependents` | string[] | **yes** | List of dependents |
| `escalations` | object[] | **yes** | List of escalations |
| `history` | object[] | **yes** | List of history |
| `labels` | string[] | **yes** | List of labels |
| `merge_requests` | object[] | **yes** | List of merge requests |
| `tethers` | object[] | **yes** | List of tethers |
| `timeline` | object[] | **yes** | List of timeline |
| `tokens` | object[] | **yes** | List of tokens |
| `world` | string | **yes** | world |
| `writ` | object | **yes** | writ details |

### active_agents

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `active_writ_id` | string | no | Reference to active writ |
| `created_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `id` | string | **yes** | Unique identifier |
| `name` | string | **yes** | name |
| `role` | string | **yes** | role |
| `state` | string | **yes** | state |
| `updated_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `world` | string | **yes** | world |

### caravan_items

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `caravan_id` | string | **yes** | Reference to caravan |
| `phase` | integer | **yes** | phase |
| `world` | string | **yes** | world |
| `writ_id` | string | **yes** | Reference to writ |

### cost

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cycle_time` | string | no | cycle time |
| `models` | object[] | **yes** | List of models |
| `total` | number | **yes** | total |

#### cost.models

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cache_creation_tokens` | integer | **yes** | cache creation tokens |
| `cache_read_tokens` | integer | **yes** | cache read tokens |
| `cost` | number | **yes** | cost |
| `input_tokens` | integer | **yes** | input tokens |
| `model` | string | **yes** | model |
| `output_tokens` | integer | **yes** | output tokens |
| `reasoning_tokens` | integer | **yes** | reasoning tokens |

### escalations

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `acknowledged` | boolean | **yes** | acknowledged |
| `created_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `description` | string | **yes** | description |
| `id` | string | **yes** | Unique identifier |
| `last_notified_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `severity` | string | **yes** | severity |
| `source` | string | **yes** | source |
| `source_ref` | string | no | source ref |
| `status` | string | **yes** | status |
| `updated_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |

### history

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | **yes** | action |
| `agent_name` | string | **yes** | agent name |
| `ended_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `id` | string | **yes** | Unique identifier |
| `started_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `summary` | string | no | summary |
| `writ_id` | string | no | Reference to writ |

### merge_requests

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `attempts` | integer | **yes** | attempts |
| `blocked_by` | string | no | blocked by |
| `branch` | string | **yes** | branch |
| `claimed_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `claimed_by` | string | no | claimed by |
| `created_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `id` | string | **yes** | Unique identifier |
| `merged_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `phase` | string | **yes** | phase |
| `priority` | integer | **yes** | priority |
| `resolution_count` | integer | **yes** | resolution count |
| `updated_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `writ_id` | string | **yes** | Reference to writ |

### tethers

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent` | string | **yes** | agent |
| `role` | string | **yes** | role |

### timeline

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `action` | string | **yes** | action |
| `detail` | string | **yes** | detail |
| `occurred_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |

### tokens

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cache_creation_tokens` | integer | **yes** | cache creation tokens |
| `cache_read_tokens` | integer | **yes** | cache read tokens |
| `cost_usd` | number | no | cost usd |
| `duration_ms` | integer | no | duration ms |
| `input_tokens` | integer | **yes** | input tokens |
| `model` | string | **yes** | model |
| `output_tokens` | integer | **yes** | output tokens |
| `reasoning_tokens` | integer | **yes** | reasoning tokens |

### writ

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `assignee` | string | no | assignee |
| `close_reason` | string | no | close reason |
| `closed_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `created_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `created_by` | string | **yes** | created by |
| `description` | string | **yes** | description |
| `id` | string | **yes** | Unique identifier |
| `kind` | string | **yes** | kind |
| `labels` | string[] | **yes** | List of labels |
| `metadata` | object | no | metadata |
| `parent_id` | string | no | Reference to parent |
| `priority` | integer | **yes** | priority |
| `status` | string | **yes** | status |
| `title` | string | **yes** | title |
| `updated_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
