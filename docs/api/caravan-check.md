# caravan-check

JSON Schema for the `caravan-check` command.

**Schema file**: [caravan-check.schema.json](caravan-check.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `blocked_by_caravans` | string[] | no | List of blocked by caravans |
| `id` | string | **yes** | Unique identifier |
| `items` | object[] | **yes** | List of items |
| `name` | string | **yes** | name |
| `status` | string | **yes** | status |

### items

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `assignee` | string | no | assignee |
| `phase` | integer | **yes** | phase |
| `ready` | boolean | **yes** | ready |
| `world` | string | **yes** | world |
| `writ_id` | string | **yes** | Reference to writ |
| `writ_status` | string | **yes** | writ status |
