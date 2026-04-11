# quota-rotate

JSON Schema for the `quota-rotate` command.

**Schema file**: [quota-rotate.schema.json](quota-rotate.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `actions` | object[] | **yes** | List of actions |
| `dry_run` | boolean | **yes** | dry run |
| `expired` | string[] | **yes** | List of expired |

### actions

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent` | string | **yes** | agent |
| `from_account` | string | **yes** | from account |
| `paused` | boolean | no | paused |
| `to_account` | string | no | to account |
