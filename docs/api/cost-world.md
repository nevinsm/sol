# cost-world

JSON Schema for the `cost-world` command.

**Schema file**: [cost-world.schema.json](cost-world.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agents` | object[] | **yes** | List of agents |
| `period` | string | **yes** | period |
| `total_cost` | number | **yes** | total cost |
| `world` | string | **yes** | world |

### agents

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent` | string | **yes** | agent |
| `cache_tokens` | integer | **yes** | cache tokens |
| `cost` | number | **yes** | cost |
| `input_tokens` | integer | **yes** | input tokens |
| `output_tokens` | integer | **yes** | output tokens |
| `writs` | integer | **yes** | writs |
