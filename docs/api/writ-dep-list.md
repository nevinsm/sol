# writ-dep-list

JSON Schema for the `writ-dep-list` command.

**Schema file**: [writ-dep-list.schema.json](writ-dep-list.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `depended_by` | string[] | **yes** | List of depended by |
| `depends_on` | string[] | **yes** | List of depends on |
| `writ_id` | string | **yes** | Reference to writ |
