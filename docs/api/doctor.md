# doctor

JSON Schema for the `doctor` command.

**Schema file**: [doctor.schema.json](doctor.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `checks` | object[] | **yes** | List of checks |

### checks

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `fix` | string | **yes** | fix |
| `message` | string | **yes** | message |
| `name` | string | **yes** | name |
| `passed` | boolean | **yes** | passed |
| `warning` | boolean | no | warning |
