# broker-status

JSON Schema for the `broker-status` command.

**Schema file**: [broker-status.schema.json](broker-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `checked_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `patrol_count` | integer | **yes** | patrol count |
| `runtimes` | object[] | no | List of runtimes |
| `stale` | boolean | **yes** | stale |
| `status` | string | **yes** | status |

### runtimes

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `last_probe_at` | string (date-time) | no | Timestamp (RFC 3339, UTC) |
| `ok` | boolean | **yes** | ok |
| `runtime` | string | **yes** | runtime |
