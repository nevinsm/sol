# forge-status

JSON Schema for the `forge-status` command.

**Schema file**: [forge-status.schema.json](forge-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `blocked` | integer | **yes** | blocked |
| `claimed_mr` | object | no | claimed mr details |
| `failed` | integer | **yes** | failed |
| `in_progress` | integer | **yes** | in progress |
| `last_failure` | object | no | last failure details |
| `last_merge` | object | no | last merge details |
| `merged` | integer | **yes** | merged |
| `merging` | boolean | no | merging |
| `paused` | boolean | **yes** | paused |
| `pid` | integer | no | pid |
| `ready` | integer | **yes** | ready |
| `running` | boolean | **yes** | running |
| `total` | integer | **yes** | total |
| `world` | string | **yes** | world |

### claimed_mr

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `age` | string | **yes** | age |
| `branch` | string | **yes** | branch |
| `id` | string | **yes** | Unique identifier |
| `title` | string | **yes** | title |
| `writ_id` | string | **yes** | Reference to writ |

### last_failure

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `branch` | string | **yes** | branch |
| `mr_id` | string | **yes** | Reference to mr |
| `occurred_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `title` | string | no | title |

### last_merge

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `branch` | string | **yes** | branch |
| `mr_id` | string | **yes** | Reference to mr |
| `occurred_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `title` | string | no | title |
