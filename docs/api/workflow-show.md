# workflow-show

JSON Schema for the `workflow-show` command.

**Schema file**: [workflow-show.schema.json](workflow-show.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | no | description |
| `error` | string | no | error |
| `manifest` | boolean | **yes** | manifest |
| `name` | string | **yes** | name |
| `path` | string | **yes** | path |
| `steps` | object[] | no | List of steps |
| `tier` | string | **yes** | tier |
| `type` | string | **yes** | type |
| `valid` | boolean | **yes** | valid |
| `variables` | object | no | variables |

### steps

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | **yes** | Unique identifier |
| `instructions` | string | **yes** | instructions |
| `needs` | string[] | no | List of needs |
| `title` | string | **yes** | title |
