# caravan-dep-list

JSON Schema for the `caravan-dep-list` command.

**Schema file**: [caravan-dep-list.schema.json](caravan-dep-list.schema.json)

## Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `depended_by` | object[] | **yes** | List of depended by |
| `depends_on` | object[] | **yes** | List of depends on |
| `id` | string | **yes** | Unique identifier |
| `name` | string | **yes** | name |

### depended_by

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | **yes** | Unique identifier |
| `name` | string | **yes** | name |
| `status` | string | **yes** | status |

### depends_on

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | **yes** | Unique identifier |
| `name` | string | **yes** | name |
| `status` | string | **yes** | status |
