# Prompt 03: Schema, Validation, and Performance

You are fixing schema safety, adding validation, and addressing
performance concerns found during the second Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 review-2 prompt 02 is complete. All tests pass.

Read all files mentioned below before making changes.

---

## Task 1: Add `"formulas"` to reserved world names

**File:** `internal/config/config.go`

**Bug:** The reserved names list includes `"store"`, `"runtime"`, and
`"sol"`, but not `"formulas"`. A user running `sol world init formulas`
would create `$SOL_HOME/formulas/world.toml` and
`$SOL_HOME/formulas/outposts/`, colliding with the workflow formula
directory at `$SOL_HOME/formulas/`.

**Verify:** Read `internal/workflow/workflow.go` to confirm the formula
path — look for how `$SOL_HOME/formulas/` is constructed.

**Fix:** Add `"formulas"` to the `reservedWorldNames` map:

```go
var reservedWorldNames = map[string]bool{
    "store":    true,
    "runtime":  true,
    "sol":      true,
    "formulas": true,
}
```

Also update the test in `internal/config/world_config_test.go` —
`TestValidateWorldNameReserved` — to include `"formulas"` in the
reserved names test cases.

---

## Task 2: Add world name length limit

**File:** `internal/config/config.go`, `ValidateWorldName`

**Bug:** No upper bound on world name length. Extremely long names could
exceed filesystem path limits when combined with nested paths like
`$SOL_HOME/{world}/outposts/{agent}/worktree/.claude/CLAUDE.md` or
create absurdly long tmux session names (`sol-{world}-{agentName}`).

**Fix:** Add a length check after the regex check:

```go
const maxWorldNameLen = 64

func ValidateWorldName(name string) error {
    if name == "" {
        return fmt.Errorf("world name must not be empty")
    }
    if len(name) > maxWorldNameLen {
        return fmt.Errorf("world name %q is too long (%d chars, max %d)", name, len(name), maxWorldNameLen)
    }
    if !validWorldName.MatchString(name) {
        return fmt.Errorf("invalid world name %q: must match [a-zA-Z0-9][a-zA-Z0-9_-]*", name)
    }
    if reservedWorldNames[name] {
        return fmt.Errorf("world name %q is reserved", name)
    }
    return nil
}
```

Add a test case for the length limit in the config tests.

---

## Task 3: Fix hardcoded `.store` in dry-run message

**File:** `cmd/world.go`, `worldDeleteCmd.RunE`

**Bug:** The dry-run confirmation message uses a hardcoded `.store` path
segment instead of `config.StoreDir()`:

```go
fmt.Printf("  - World database: %s\n", filepath.Join(home, ".store", name+".db"))
```

If `StoreDir()` ever changes, the dry-run message would show the wrong
path while the actual deletion uses the correct one.

**Fix:** Use `config.StoreDir()`:

```go
fmt.Printf("  - World database: %s\n", filepath.Join(config.StoreDir(), name+".db"))
```

Also verify that the world directory path uses `config.WorldDir(name)`
or equivalent rather than manual path construction.

---

## Task 4: Fix N+1 query in `ListWorkItems`

**File:** `internal/store/workitems.go`

**Bug:** After fetching work items, labels are fetched one-by-one in a
loop (around line 264):

```go
for i := range items {
    labelRows, err := s.db.Query(
        `SELECT label FROM labels WHERE work_item_id = ? ORDER BY label`,
        items[i].ID,
    )
    // ...
}
```

With N work items, this results in N+1 queries.

**Fix:** Fetch all labels for all returned work items in a single query,
then distribute them:

```go
// After fetching items, batch-fetch all labels.
if len(items) > 0 {
    // Build ID list for IN clause.
    ids := make([]interface{}, len(items))
    placeholders := make([]string, len(items))
    for i, item := range items {
        ids[i] = item.ID
        placeholders[i] = "?"
    }

    labelQuery := fmt.Sprintf(
        `SELECT work_item_id, label FROM labels WHERE work_item_id IN (%s) ORDER BY work_item_id, label`,
        strings.Join(placeholders, ","),
    )
    labelRows, err := s.db.Query(labelQuery, ids...)
    if err != nil {
        return nil, fmt.Errorf("failed to query labels: %w", err)
    }
    defer labelRows.Close()

    // Build a map of item ID → labels.
    labelMap := make(map[string][]string)
    for labelRows.Next() {
        var itemID, label string
        if err := labelRows.Scan(&itemID, &label); err != nil {
            return nil, fmt.Errorf("failed to scan label: %w", err)
        }
        labelMap[itemID] = append(labelMap[itemID], label)
    }
    if err := labelRows.Err(); err != nil {
        return nil, fmt.Errorf("failed to iterate labels: %w", err)
    }

    // Assign labels to items.
    for i := range items {
        items[i].Labels = labelMap[items[i].ID]
    }
}
```

Make sure `strings` and `fmt` are imported. Remove the old per-item
label query loop.

---

## Task 5: Add missing indexes

**File:** `internal/store/schema.go`

**Bug:** Several columns used in WHERE clauses lack indexes, which will
hurt performance at scale.

**Fix:** Add new indexes in the appropriate migration. Since we cannot
modify existing migrations, add a new world DB migration (V5) and a new
sphere DB migration (V6).

### World DB — V5:

```go
var migrateWorldV5 = `CREATE INDEX IF NOT EXISTS idx_mr_blocked_by ON merge_requests(blocked_by);`
```

### Sphere DB — V6:

```go
var migrateSpherV6 = `
CREATE INDEX IF NOT EXISTS idx_agents_world_state ON agents(world, state);
CREATE INDEX IF NOT EXISTS idx_escalations_status ON escalations(status);
CREATE INDEX IF NOT EXISTS idx_caravan_items_world ON caravan_items(world);
`
```

Update the migration runner functions (`migrateWorld` and
`migrateSphere`) to apply these new migrations. Follow the existing
pattern: check `schema_version`, execute DDL, update version.

**Important:** Use `CREATE INDEX IF NOT EXISTS` to ensure idempotency.
Update the `const` or variable that tracks the current schema version.

Update any tests that assert on specific schema version numbers.

---

## Task 6: Make ALTER TABLE migrations crash-safe

**File:** `internal/store/schema.go`

**Bug:** V3 world (`ALTER TABLE merge_requests ADD COLUMN blocked_by`)
and V4 sphere (`ALTER TABLE` renames) are not idempotent. If the process
crashes between executing the ALTER and updating `schema_version`, the
next startup will fail trying to re-apply the ALTER.

**Fix:** Wrap each ALTER TABLE migration in a helper that catches
"already exists" errors. SQLite's error messages for duplicate columns
contain `"duplicate column name"`:

```go
// execIgnoreDuplicate runs a SQL statement and ignores "duplicate column"
// errors, making ALTER TABLE ADD COLUMN idempotent.
func execIgnoreDuplicate(db interface{ Exec(string, ...interface{}) (sql.Result, error) }, query string) error {
    _, err := db.Exec(query)
    if err != nil && strings.Contains(err.Error(), "duplicate column name") {
        return nil
    }
    return err
}
```

For the rename operations in V4, SQLite returns `"no such column"` if
the column was already renamed. Add similar handling:

```go
func execIgnoreRenameErrors(db interface{ Exec(string, ...interface{}) (sql.Result, error) }, query string) error {
    _, err := db.Exec(query)
    if err != nil {
        msg := err.Error()
        if strings.Contains(msg, "no such column") || strings.Contains(msg, "no such table") {
            return nil // Already renamed
        }
    }
    return err
}
```

Apply these wrappers to the V3 world and V4 sphere migration statements.

Make sure `strings` is imported in `schema.go`.

---

## Task 7: Verify

1. `make build` — compiles cleanly
2. `make test` — all tests pass
3. Verification:
   ```bash
   # "formulas" is reserved
   export SOL_HOME=/tmp/sol-test-review2
   mkdir -p /tmp/sol-test-review2/.store
   bin/sol world init formulas 2>&1
   # → error: reserved

   # Long name is rejected
   bin/sol world init $(python3 -c "print('a'*65)") 2>&1
   # → error: too long

   # Schema versions are correct
   sqlite3 /tmp/sol-test-review2/.store/sphere.db "SELECT version FROM schema_version"
   # → should show latest version

   rm -rf /tmp/sol-test-review2
   ```

---

## Guidelines

- The N+1 fix is the most complex change. Test thoroughly — the existing
  `TestListWorkItemsWithLabels` (or similar) in `store_test.go` should
  exercise the new batch query.
- For new migrations, follow the exact pattern of existing ones. Read
  `migrateWorld()` and `migrateSphere()` carefully before adding steps.
- The ALTER TABLE crash-safety fix is defensive — it should not change
  behavior on a healthy database, only on one that crashed mid-migration.
- All existing tests must continue to pass.
- Commit with message:
  `fix(store): arc 1 review-2 — indexes, N+1 query, migration safety`
