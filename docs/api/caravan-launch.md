# caravan-launch

JSON Schema for the `caravan-launch` command.

**Schema file**: [caravan-launch.schema.json](caravan-launch.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `auto_closed` | boolean | **yes** | auto closed |
| `blocked` | integer | **yes** | blocked |
| `caravan_id` | string | **yes** | Reference to caravan |
| `dispatched` | object[] | **yes** | List of dispatched |
| `world` | string | **yes** | world |

### dispatched

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent_name` | string | **yes** | agent name |
| `session_name` | string | **yes** | session name |
| `writ_id` | string | **yes** | Reference to writ |
