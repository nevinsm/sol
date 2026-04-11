# prefect-status

JSON Schema for the `prefect-status` command.

**Schema file**: [prefect-status.schema.json](prefect-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `pid` | integer | no | pid |
| `status` | string | **yes** | status |
| `uptime_seconds` | integer | no | uptime seconds |
