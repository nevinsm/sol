# consul-status

JSON Schema for the `consul-status` command.

**Schema file**: [consul-status.schema.json](consul-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `caravan_feeds` | integer | **yes** | caravan feeds |
| `checked_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `escalations` | integer | **yes** | escalations |
| `patrol_count` | integer | **yes** | patrol count |
| `pid_gone` | boolean | **yes** | pid gone |
| `stale` | boolean | **yes** | stale |
| `stale_tethers` | integer | **yes** | stale tethers |
| `status` | string | **yes** | status |
| `wedged` | boolean | **yes** | wedged |
