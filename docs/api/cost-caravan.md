# cost-caravan

JSON Schema for the `cost-caravan` command.

**Schema file**: [cost-caravan.schema.json](cost-caravan.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `caravan_id` | string | **yes** | Reference to caravan |
| `caravan_name` | string | **yes** | caravan name |
| `period` | string | **yes** | period |
| `total_cost` | number | **yes** | total cost |
| `writs` | object[] | **yes** | List of writs |

### writs

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cache_tokens` | integer | **yes** | cache tokens |
| `cost` | number | **yes** | cost |
| `input_tokens` | integer | **yes** | input tokens |
| `kind` | string | **yes** | kind |
| `output_tokens` | integer | **yes** | output tokens |
| `phase` | integer | **yes** | phase |
| `status` | string | **yes** | status |
| `world` | string | **yes** | world |
| `writ_id` | string | **yes** | Reference to writ |
