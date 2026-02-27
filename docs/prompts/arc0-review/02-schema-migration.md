# Arc 0 Review, Prompt 2: Schema Migration and Status Values

## Context

Arc 0 left database column names, table names, and status values using old naming because changing them requires schema migrations. This prompt adds a new schema version for both the world and sphere databases to rename columns, tables, and status values.

After prompt 1 of this review series, all event constants and production strings use new naming. This prompt tackles the data layer.

## What To Change

### 1. Sphere DB Schema — V4 (was V3)

**File:** `internal/store/schema.go`

Add a new schema version `sphereSchemaV4` (rename the constant prefix from `town` → `sphere` at the same time — see section 5). This migration renames:

1. **`hook_item` column → `tether_item`** in the `agents` table
2. **`rig` column → `world`** in the `agents` table
3. **`convoys` table → `caravans`**
4. **`convoy_items` table → `caravan_items`**
5. **`rig` column → `world`** in the `caravan_items` table (was `convoy_items`)
6. **`convoy_id` column → `caravan_id`** in the `caravan_items` table

SQLite supports `ALTER TABLE RENAME TABLE` (since 3.25.0) and `ALTER TABLE RENAME COLUMN` (since 3.25.0). The pure-Go SQLite we use (`modernc.org/sqlite`) supports both.

```sql
-- Rename columns in agents
ALTER TABLE agents RENAME COLUMN hook_item TO tether_item;
ALTER TABLE agents RENAME COLUMN rig TO world;

-- Rename convoy tables
ALTER TABLE convoys RENAME TO caravans;
ALTER TABLE convoy_items RENAME TO caravan_items;

-- Rename columns in caravan_items (after table rename)
ALTER TABLE caravan_items RENAME COLUMN convoy_id TO caravan_id;
ALTER TABLE caravan_items RENAME COLUMN rig TO world;
```

**Important:** SQLite automatically updates indexes when columns/tables are renamed via `ALTER TABLE RENAME`, so the existing indexes will continue to work. However, the index names will still reference the old names. This is cosmetic and harmless — SQLite indexes are internal and not user-facing. If you want to rename them too, you must `DROP INDEX` + `CREATE INDEX` with the new name.

Update `migrateSphere()` (renamed from `migrateTown()`) to apply V4 if `v < 4`, and update the version to 4.

### 2. World DB — No Schema Change Needed

The world database (`{world}.db`) doesn't have columns with old naming — its tables (`work_items`, `labels`, `merge_requests`, `dependencies`) use generic names. No migration needed.

However, the **work item status value `"hooked"` must change to `"tethered"`**. This is a data value, not a schema change. It affects:
- `internal/dispatch/dispatch.go` — the status string `"hooked"` when setting work item status during cast
- `internal/dispatch/dispatch.go` — the check `item.Status == "hooked"` for re-cast detection
- All test files that check or set `Status: "hooked"`

### 3. Update Go Code — Agent Struct and SQL

**File:** `internal/store/agents.go`

Rename the Go struct field and update all SQL to use new column names:
- `Agent.HookItem` → `Agent.TetherItem`
- All SQL: `hook_item` → `tether_item`, `rig` → `world`
- All local variables named `hookItem` → `tetherItem`
- Comment: `// GetAgent returns an agent by ID ("rig/name").` → `// GetAgent returns an agent by ID ("world/name").`
- Comment on `UpdateAgentState`: `hook_item` references → `tether_item`

Specifically update these SQL strings:
```go
// CreateAgent:
`INSERT INTO agents (id, name, rig, role, ...)` → `INSERT INTO agents (id, name, world, role, ...)`

// GetAgent:
`SELECT id, name, rig, role, state, hook_item, ...` → `SELECT id, name, world, role, state, tether_item, ...`

// UpdateAgentState:
`UPDATE agents SET state = ?, hook_item = NULL, ...` → `UPDATE agents SET state = ?, tether_item = NULL, ...`
`UPDATE agents SET state = ?, hook_item = ?, ...` → `UPDATE agents SET state = ?, tether_item = ?, ...`

// ListAgents:
`SELECT id, name, rig, role, state, hook_item, ...` → `SELECT id, name, world, role, state, tether_item, ...`
`AND rig = ?` → `AND world = ?`

// FindIdleAgent:
`SELECT id, name, rig, role, state, hook_item, ...` → `SELECT id, name, world, role, state, tether_item, ...`
`FROM agents WHERE rig = ?` → `FROM agents WHERE world = ?`
```

### 4. Update Go Code — Convoy SQL

**File:** `internal/store/convoys.go`

Update all SQL strings to use new table and column names:
```go
// CreateCaravan:
`INSERT INTO convoys (...)` → `INSERT INTO caravans (...)`

// GetCaravan:
`... FROM convoys WHERE ...` → `... FROM caravans WHERE ...`

// ListCaravans:
`... FROM convoys` → `... FROM caravans`

// UpdateCaravanStatus:
`UPDATE convoys SET ...` → `UPDATE caravans SET ...`

// AddCaravanItem:
`INSERT OR IGNORE INTO convoy_items (convoy_id, work_item_id, rig)` →
`INSERT OR IGNORE INTO caravan_items (caravan_id, work_item_id, world)`

// RemoveCaravanItem:
`DELETE FROM convoy_items WHERE convoy_id = ?` →
`DELETE FROM caravan_items WHERE caravan_id = ?`

// ListCaravanItems:
`SELECT convoy_id, work_item_id, rig FROM convoy_items WHERE convoy_id = ?` →
`SELECT caravan_id, work_item_id, world FROM caravan_items WHERE caravan_id = ?`
```

Also update the comment: `// Group items by world (rig column in DB).` → `// Group items by world.` (the mismatch note is no longer needed).

### 5. Rename Schema Constants and Migration Functions

**File:** `internal/store/schema.go`

Rename all internal constants and functions:
```
rigSchemaV1  → worldSchemaV1
rigSchemaV2  → worldSchemaV2
rigSchemaV3  → worldSchemaV3
rigSchemaV4  → worldSchemaV4
townSchemaV1 → sphereSchemaV1
townSchemaV2 → sphereSchemaV2
townSchemaV3 → sphereSchemaV3
townSchemaV4 → sphereSchemaV4  (new — the migration SQL from section 1)

migrateRig()  → migrateWorld()
migrateTown() → migrateSphere()
```

Update all error messages:
```
"failed to create rig schema v1" → "failed to create world schema v1"
(etc. for all schema error messages)
"failed to create town schema v1" → "failed to create sphere schema v1"
(etc.)
```

**File:** `internal/store/store.go`
- `s.migrateRig()` → `s.migrateWorld()`
- `s.migrateTown()` → `s.migrateSphere()` (if it exists — check `OpenSphere`)

### 6. Work Item Status: "hooked" → "tethered"

**File:** `internal/dispatch/dispatch.go`

Two places:
- Line 126: `item.Status == "hooked"` → `item.Status == "tethered"`
- Line 186: `Status: "hooked"` → `Status: "tethered"`
- Line 184 comment: `// 5. Update work item: status → hooked` → `// 5. Update work item: status → tethered`

### 7. All Consumers of Agent.HookItem

After renaming `Agent.HookItem` → `Agent.TetherItem`, update every file that references the field:

- `internal/prefect/supervisor.go` — `agent.HookItem` → `agent.TetherItem`
- `internal/sentinel/witness.go` — `agent.HookItem` → `agent.TetherItem`
- `internal/consul/deacon.go` — `agent.HookItem` → `agent.TetherItem`
- `internal/status/status.go` — `agent.HookItem` → `agent.TetherItem`
- `cmd/agent.go` — `a.HookItem` → `a.TetherItem` (in the table row formatting)
- Any test files that reference `.HookItem`

### 8. Consul Patrol Event Payload Keys

**File:** `internal/consul/deacon.go`

The consul emits events with payload maps. Update the keys:
- `"stale_hooks"` → `"stale_tethers"` (in the patrol event payload)
- `"convoy_feeds"` → `"caravan_feeds"` (in the patrol event payload)
- `"convoy_id"` → `"caravan_id"` (in caravan feed event payload)

Search for all map literals that construct event payloads and update keys.

## What NOT To Change (Yet)

- Test comments, test fixture strings, test variable names — prompt 3
- Test data using `"gt-"` prefixed work item IDs — prompt 3
- Test data using `Source: "gt"` — prompt 3
- Test data using `"myrig/witness"`, `"myrig/refinery"`, `"polecat"` — prompt 3

## Acceptance Criteria

```bash
make build && make test     # passes

# No old column names in Go code:
grep -rn 'hook_item' --include='*.go' .              # no hits
grep -rn '\.HookItem' --include='*.go' .             # no hits
grep -rn 'FROM convoys\|INTO convoys\|UPDATE convoys' --include='*.go' . # no hits
grep -rn 'convoy_items' --include='*.go' .           # no hits
grep -rn '"hooked"' --include='*.go' .               # no hits
grep -rn 'migrateRig\|migrateTown' --include='*.go' .  # no hits
grep -rn 'rigSchema\|townSchema' --include='*.go' .    # no hits

# Verify new schema works:
# The migration should handle both fresh databases AND upgrades from V3
```
