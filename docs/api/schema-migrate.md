# schema-migrate

JSON Schema for the `schema-migrate` command.

**Schema file**: [schema-migrate.schema.json](schema-migrate.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `applied_migrations` | object[] | **yes** | List of applied migrations |

### applied_migrations

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `database` | string | **yes** | database |
| `from_version` | integer | **yes** | from version |
| `status` | string | **yes** | status |
| `to_version` | integer | **yes** | to version |
| `type` | string | **yes** | type |
