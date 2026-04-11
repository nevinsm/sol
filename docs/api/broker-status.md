# broker-status

JSON Schema for the `broker-status` command.

**Schema file**: [broker-status.schema.json](broker-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `checked_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `consecutive_failures` | integer | **yes** | consecutive failures |
| `last_healthy_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `last_probe_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `patrol_count` | integer | **yes** | patrol count |
| `provider_health` | string | **yes** | provider health |
| `providers` | object[] | no | List of providers |
| `stale` | boolean | **yes** | stale |
| `status` | string | **yes** | status |

### providers

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `consecutive_failures` | integer | **yes** | consecutive failures |
| `health` | string | **yes** | health |
| `last_healthy_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `last_probe_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `provider` | string | **yes** | provider |
