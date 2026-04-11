# chronicle-status

JSON Schema for the `chronicle-status` command.

**Schema file**: [chronicle-status.schema.json](chronicle-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `checkpoint_offset` | integer | no | checkpoint offset |
| `events_processed` | integer | no | events processed |
| `heartbeat_age` | string | no | heartbeat age |
| `pid` | integer | no | pid |
| `status` | string | **yes** | status |
