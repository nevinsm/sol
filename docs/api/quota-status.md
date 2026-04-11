# quota-status

JSON Schema for the `quota-status` command.

**Schema file**: [quota-status.schema.json](quota-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `accounts` | object[] | **yes** | List of accounts |

### accounts

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `handle` | string | **yes** | handle |
| `last_used_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `limited_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `remaining` | integer | no | remaining |
| `resets_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `status` | string | **yes** | status |
| `window` | string | no | window |
