# forge-await

JSON Schema for the `forge-await` command.

**Schema file**: [forge-await.schema.json](forge-await.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `messages` | object[] | **yes** | List of messages |
| `waited_seconds` | number | **yes** | waited seconds |
| `woke` | boolean | **yes** | woke |

### messages

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `body` | string | **yes** | body |
| `created_at` | string (date-time) | **yes** | Timestamp (RFC 3339, UTC) |
| `priority` | string | **yes** | priority |
| `sender` | string | **yes** | sender |
| `subject` | string | **yes** | subject |
| `ttl` | string | **yes** | ttl |
| `type` | string | **yes** | type |
