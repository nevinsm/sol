# ledger-status

JSON Schema for the `ledger-status` command.

**Schema file**: [ledger-status.schema.json](ledger-status.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `heartbeat_age` | string | no | heartbeat age |
| `pid` | integer | no | pid |
| `port` | integer | no | port |
| `requests_total` | integer | no | requests total |
| `status` | string | **yes** | status |
| `tokens_processed` | integer | no | tokens processed |
| `worlds_written` | integer | no | worlds written |
