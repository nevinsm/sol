# Prompt 01: Store Hardening — Migrations, Consistency, Defensive Patterns

You are fixing store-layer issues found during the third Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** All Arc 1 review-2 prompts are complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/store/schema.go` — migration helpers and schema definitions
- `internal/store/caravans.go` — caravan queries and `CheckCaravanReadiness`
- `internal/store/worlds.go` — world CRUD
- `internal/store/agents.go` — `RowsAffected` usage pattern
- `internal/store/workitems.go` — `RowsAffected` usage pattern
- `internal/store/merge_requests.go` — `RowsAffected` usage pattern
- `internal/store/escalations.go` — `RowsAffected` usage pattern (different)
- `internal/store/messages.go` — `RowsAffected` usage pattern (different)
- `internal/store/worlds_test.go` — existing GetWorld tests

---

## Task 1: Replace fragile error-string matching in migration helpers

**File:** `internal/store/schema.go`

The `execIgnoreDuplicate` function (line 221) matches on
`strings.Contains(err.Error(), "duplicate column name")` and
`execIgnoreRenameErrors` (line 231) matches on `"no such column"` /
`"no such table"`. These are fragile — if the SQLite driver changes error
text, migrations break.

**Replace `execIgnoreDuplicate` with a check-then-exec pattern:**

```go
// columnExists checks whether a column exists on a table using PRAGMA table_info.
func columnExists(db interface {
	QueryRow(string, ...interface{}) *sql.Row
}, table, column string) (bool, error) {
	// PRAGMA table_info returns one row per column. We can't parameterize
	// PRAGMA arguments, but table/column names come from our own schema
	// constants, not user input.
	rows, err := db.(interface {
		Query(string, ...interface{}) (*sql.Rows, error)
	}).Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
```

Then update `migrateWorld` for the V3 migration (the only caller of
`execIgnoreDuplicate`). Instead of:

```go
if v < 3 {
    if err := execIgnoreDuplicate(s.db, worldSchemaV3); err != nil {
```

Use:

```go
if v < 3 {
    exists, err := columnExists(s.db, "merge_requests", "blocked_by")
    if err != nil {
        return fmt.Errorf("failed to check merge_requests schema: %w", err)
    }
    if !exists {
        if _, err := s.db.Exec(worldSchemaV3); err != nil {
            return fmt.Errorf("failed to apply world schema v3: %w", err)
        }
    }
}
```

Remove `execIgnoreDuplicate` after — it has no other callers.

**Replace `execIgnoreRenameErrors` with a table-exists check pattern:**

For the sphere V4 migration (renames), the existing per-statement approach
is actually correct for idempotency. But the error-string matching is
still fragile. Replace it with a helper that checks whether the
source column/table still exists before running the rename:

```go
// tableExists checks whether a table exists in the database.
func tableExists(db interface {
	QueryRow(string, ...interface{}) *sql.Row
}, name string) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
```

Then update the V4 sphere migration. Instead of blindly executing each
rename statement, check first:

```go
if v < 4 {
    // Rename agents.hook_item → tether_item (if not already renamed).
    if exists, _ := columnExists(s.db, "agents", "hook_item"); exists {
        if _, err := s.db.Exec(`ALTER TABLE agents RENAME COLUMN hook_item TO tether_item`); err != nil {
            return fmt.Errorf("failed to rename agents.hook_item: %w", err)
        }
    }
    // Rename agents.rig → world (if not already renamed).
    if exists, _ := columnExists(s.db, "agents", "rig"); exists {
        if _, err := s.db.Exec(`ALTER TABLE agents RENAME COLUMN rig TO world`); err != nil {
            return fmt.Errorf("failed to rename agents.rig: %w", err)
        }
    }
    // Rename convoys → caravans (if not already renamed).
    if exists, _ := tableExists(s.db, "convoys"); exists {
        if _, err := s.db.Exec(`ALTER TABLE convoys RENAME TO caravans`); err != nil {
            return fmt.Errorf("failed to rename convoys: %w", err)
        }
    }
    // Rename convoy_items → caravan_items (if not already renamed).
    if exists, _ := tableExists(s.db, "convoy_items"); exists {
        if _, err := s.db.Exec(`ALTER TABLE convoy_items RENAME TO caravan_items`); err != nil {
            return fmt.Errorf("failed to rename convoy_items: %w", err)
        }
    }
    // Rename caravan_items.convoy_id → caravan_id (if not already renamed).
    if exists, _ := columnExists(s.db, "caravan_items", "convoy_id"); exists {
        if _, err := s.db.Exec(`ALTER TABLE caravan_items RENAME COLUMN convoy_id TO caravan_id`); err != nil {
            return fmt.Errorf("failed to rename caravan_items.convoy_id: %w", err)
        }
    }
    // Rename caravan_items.rig → world (if not already renamed).
    if exists, _ := columnExists(s.db, "caravan_items", "rig"); exists {
        if _, err := s.db.Exec(`ALTER TABLE caravan_items RENAME COLUMN rig TO world`); err != nil {
            return fmt.Errorf("failed to rename caravan_items.rig: %w", err)
        }
    }
}
```

Remove `execIgnoreRenameErrors` after — it has no other callers.

The `sphereSchemaV4` constant is no longer needed as a multi-line SQL
string. You may remove it or keep it as a comment for documentation.

---

## Task 2: Standardize `RowsAffected` error handling

Across the store package, there are two patterns:

**Pattern A** (agents.go, workitems.go, merge_requests.go, caravans.go):
```go
n, _ := result.RowsAffected()
```

**Pattern B** (escalations.go, messages.go, worlds.go):
```go
n, err := result.RowsAffected()
if err != nil {
    return fmt.Errorf("failed to check update result for ...: %w", err)
}
```

Standardize on **Pattern A** everywhere. The `modernc.org/sqlite` driver
never returns an error from `RowsAffected()` — this is safe.

Add a one-line comment at the first occurrence in each file:

```go
// RowsAffected is always nil for modernc.org/sqlite.
n, _ := result.RowsAffected()
```

**Files to update (change from Pattern B to Pattern A):**

- `escalations.go` lines 151-153 and 172-174
- `messages.go` lines 87-89 and 137-139
- `worlds.go` lines 107-109

---

## Task 3: Make `CheckCaravanReadiness` defensive with defer

**File:** `internal/store/caravans.go`

The current code opens a world store per world and closes it manually at
line 282 (happy path) or line 274 (error path). This works today but is
fragile — any future modification that adds an early return will leak a
store connection.

Extract the per-world processing into a closure so `defer` works:

```go
for world, worldItems := range byWorld {
    worldResults, err := func() ([]CaravanItemStatus, error) {
        worldStore, err := worldOpener(world)
        if err != nil {
            return nil, fmt.Errorf("failed to open world %q: %w", world, err)
        }
        defer worldStore.Close()

        var out []CaravanItemStatus
        for _, ci := range worldItems {
            cis := CaravanItemStatus{CaravanItem: ci}

            item, err := worldStore.GetWorkItem(ci.WorkItemID)
            if err != nil {
                cis.WorkItemStatus = "unknown"
                out = append(out, cis)
                continue
            }

            cis.WorkItemStatus = item.Status

            ready, err := worldStore.IsReady(ci.WorkItemID)
            if err != nil {
                return nil, fmt.Errorf("failed to check readiness for %q: %w", ci.WorkItemID, err)
            }
            cis.Ready = ready

            out = append(out, cis)
        }
        return out, nil
    }()
    if err != nil {
        return nil, err
    }
    results = append(results, worldResults...)
}
```

---

## Task 4: Make `GetWorld` return an error on not-found

**File:** `internal/store/worlds.go`

Currently `GetWorld` returns `nil, nil` when the world doesn't exist
(line 42-43), unlike every other `Get*` function which returns an error.

Change:

```go
if err == sql.ErrNoRows {
    return nil, nil
}
```

To:

```go
if err == sql.ErrNoRows {
    return nil, fmt.Errorf("world %q not found", name)
}
```

Then update the tests in `worlds_test.go` that expect `nil, nil`.
The test `TestGetWorld_NotFound` (or similar) should now expect an error
containing `"not found"`.

Check for any production callers of `GetWorld` that rely on the `nil, nil`
return — grep the entire codebase for `GetWorld(`. As of this review there
are no production callers, only test callers.

---

## Task 5: Tests

Add a test for the `columnExists` helper:

```go
func TestColumnExists(t *testing.T) {
    s := newTestStore(t, "world")
    // work_items table has a "title" column.
    exists, err := columnExists(s.db, "work_items", "title")
    if err != nil {
        t.Fatalf("columnExists error: %v", err)
    }
    if !exists {
        t.Fatal("expected title column to exist")
    }
    // Nonexistent column.
    exists, err = columnExists(s.db, "work_items", "nonexistent")
    if err != nil {
        t.Fatalf("columnExists error: %v", err)
    }
    if exists {
        t.Fatal("expected nonexistent column to not exist")
    }
}
```

Add a test for `tableExists`:

```go
func TestTableExists(t *testing.T) {
    s := newTestStore(t, "world")
    exists, err := tableExists(s.db, "work_items")
    if err != nil {
        t.Fatalf("tableExists error: %v", err)
    }
    if !exists {
        t.Fatal("expected work_items table to exist")
    }
    exists, err = tableExists(s.db, "nonexistent")
    if err != nil {
        t.Fatalf("tableExists error: %v", err)
    }
    if exists {
        t.Fatal("expected nonexistent table to not exist")
    }
}
```

---

## Verification

- `make build && make test` passes
- `go vet ./...` clean
- Verify migration idempotency: the existing migration tests should still
  pass (schemas created from scratch still work; re-running migrations on
  an already-migrated DB still works)

## Commit

```
fix(store): arc 1 review-3 — migration safety, RowsAffected consistency, defensive patterns
```
