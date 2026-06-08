# writ-clean

JSON Schema for the `writ-clean` command.

**Schema file**: [writ-clean.schema.json](writ-clean.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bytes_freed` | integer | **yes** | bytes freed |
| `candidates` | object[] | no | List of candidates |
| `dirs_removed` | integer | **yes** | dirs removed |
| `retention_days` | integer | **yes** | retention days |
| `writs_cleaned` | integer | **yes** | writs cleaned |

### candidates

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `closed_at` | string | no | Timestamp (RFC 3339, UTC) |
| `size` | integer | **yes** | size |
| `writ_id` | string | **yes** | Reference to writ |
