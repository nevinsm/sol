# cost-writ

JSON Schema for the `cost-writ` command.

**Schema file**: [cost-writ.schema.json](cost-writ.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `kind` | string | no | kind |
| `models` | object[] | **yes** | List of models |
| `period` | string | **yes** | period |
| `status` | string | no | status |
| `title` | string | no | title |
| `total_cost` | number | **yes** | total cost |
| `writ_id` | string | **yes** | Reference to writ |

### models

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `cache_creation_tokens` | integer | **yes** | cache creation tokens |
| `cache_read_tokens` | integer | **yes** | cache read tokens |
| `cost` | number | **yes** | cost |
| `input_tokens` | integer | **yes** | input tokens |
| `model` | string | **yes** | model |
| `output_tokens` | integer | **yes** | output tokens |
