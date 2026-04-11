# cost-agent

JSON Schema for the `cost-agent` command.

**Schema file**: [cost-agent.schema.json](cost-agent.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent` | string | **yes** | agent |
| `period` | string | **yes** | period |
| `total_cost` | number | **yes** | total cost |
| `world` | string | **yes** | world |
| `writs` | object[] | **yes** | List of writs |

### writs

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cache_tokens` | integer | **yes** | cache tokens |
| `cost` | number | **yes** | cost |
| `input_tokens` | integer | **yes** | input tokens |
| `kind` | string | **yes** | kind |
| `output_tokens` | integer | **yes** | output tokens |
| `status` | string | **yes** | status |
| `writ_id` | string | **yes** | Reference to writ |
