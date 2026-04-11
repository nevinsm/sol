# sentinel-status

JSON Schema for the `sentinel-status` command.

**Schema file**: [sentinel-status.schema.json](sentinel-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agents_checked` | integer | no | agents checked |
| `heartbeat_age` | string | no | heartbeat age |
| `patrol_count` | integer | no | patrol count |
| `pid` | integer | no | pid |
| `reaped_count` | integer | no | reaped count |
| `running` | boolean | **yes** | running |
| `stalled_count` | integer | no | stalled count |
| `status` | string | no | status |
| `world` | string | **yes** | world |
