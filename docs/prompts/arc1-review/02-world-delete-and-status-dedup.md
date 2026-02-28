# Prompt 02: Arc 1 Review — World Delete Hardening and Status Deduplication

You are fixing design issues in `world delete` and eliminating code
duplication between `world status` and `sol status`.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 review prompt 01 is complete. All tests pass.

Read all files mentioned below before making changes.

---

## Task 1: Fix `world delete` — caravan item cleanup

**File:** `cmd/world.go`, `worldDeleteCmd.RunE`

**Problem:** Delete cleans up agents and the world record but leaves
orphaned `caravan_items` rows referencing the deleted world. After
deletion, `CheckCaravanReadiness` will fail trying to open the deleted
world's database.

**Fix:** Add caravan item cleanup before agent deletion. Read
`internal/store/caravans.go` to understand the caravan store interface.
Add a new method to the sphere store:

**File:** `internal/store/caravans.go`

```go
// DeleteCaravanItemsForWorld removes all caravan items for a given world.
func (s *Store) DeleteCaravanItemsForWorld(world string) error {
    _, err := s.db.Exec(`DELETE FROM caravan_items WHERE world = ?`, world)
    if err != nil {
        return fmt.Errorf("failed to delete caravan items for world %q: %w", world, err)
    }
    return nil
}
```

Then in `worldDeleteCmd.RunE`, call it before `DeleteAgentsForWorld`.

---

## Task 2: Fix `world delete` — wrap sphere cleanup in a transaction

**File:** `cmd/world.go`, `worldDeleteCmd.RunE`

**Problem:** `DeleteAgentsForWorld` and `RemoveWorld` (and now
`DeleteCaravanItemsForWorld`) are separate SQL operations. A crash
between them leaves inconsistent state.

**Fix:** Read `internal/store/store.go` to see if the `Store` type
exposes `db` or has transaction helpers. If the store exposes the
underlying `*sql.DB` or has a `Begin()` method, use a transaction.
If not, add a `DeleteWorldData` method to the store that wraps all
three operations in a single transaction:

**File:** `internal/store/worlds.go`

```go
// DeleteWorldData removes all sphere-level data for a world in a single
// transaction: caravan items, agents, and the world record.
func (s *Store) DeleteWorldData(world string) error {
    tx, err := s.db.Begin()
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer tx.Rollback()

    if _, err := tx.Exec(`DELETE FROM caravan_items WHERE world = ?`, world); err != nil {
        return fmt.Errorf("failed to delete caravan items for world %q: %w", world, err)
    }
    if _, err := tx.Exec(`DELETE FROM agents WHERE world = ?`, world); err != nil {
        return fmt.Errorf("failed to delete agents for world %q: %w", world, err)
    }
    if _, err := tx.Exec(`DELETE FROM worlds WHERE name = ?`, world); err != nil {
        return fmt.Errorf("failed to remove world %q: %w", world, err)
    }

    return tx.Commit()
}
```

Then in `worldDeleteCmd.RunE`, replace the three separate calls with:

```go
if err := sphereStore.DeleteWorldData(name); err != nil {
    sphereStore.Close()
    return err
}
sphereStore.Close()
```

Note: The individual `DeleteAgentsForWorld`, `DeleteCaravanItemsForWorld`,
and `RemoveWorld` methods should remain — they have other callers or are
useful independently.

---

## Task 3: Fix `world delete` error message formatting

**File:** `cmd/world.go`, `worldDeleteCmd.RunE`

**Problem 1:** The active-session error embeds `\n` in the error string:

```go
return fmt.Errorf("cannot delete world %q: %d active session(s)\n"+
    "Stop sessions first: sol session stop %s",
    name, len(activeSessions), activeSessions[0])
```

Multiline errors are unconventional in Go and break log parsing.

**Problem 2:** Only the first active session name is shown.

**Fix:** Print the guidance to stderr, then return a clean single-line
error:

```go
if len(activeSessions) > 0 {
    fmt.Fprintf(os.Stderr, "Active sessions:\n")
    for _, s := range activeSessions {
        fmt.Fprintf(os.Stderr, "  %s\n", s)
    }
    fmt.Fprintf(os.Stderr, "\nStop sessions first, e.g.: sol session stop %s\n", activeSessions[0])
    return fmt.Errorf("cannot delete world %q: %d active session(s)", name, len(activeSessions))
}
```

---

## Task 4: Fix `world delete` output — use `filepath.Join` for paths

**File:** `cmd/world.go`, `worldDeleteCmd.RunE`

Replace hardcoded `/` separators in the confirmation output:

```go
fmt.Printf("  - World database: %s\n", filepath.Join(home, ".store", name+".db"))
fmt.Printf("  - World directory: %s\n", filepath.Join(home, name))
```

Similarly fix the paths in `worldInitCmd.RunE` output (around line 129):

```go
fmt.Printf("  Config:   %s\n", config.WorldConfigPath(name))
fmt.Printf("  Database: %s\n", filepath.Join(config.StoreDir(), name+".db"))
```

Use the existing helper functions (`config.WorldConfigPath`,
`config.StoreDir`, `config.WorldDir`) rather than reconstructing paths.

---

## Task 5: Deduplicate `world status` and `sol status` display

**Files:** `cmd/world.go` (worldStatusCmd) and `cmd/status.go`

**Problem:** `world status` copy-pastes ~60 lines of display code from
`printStatus` in `status.go` and omits the caravan section.

**Fix:** Export `printStatus` (rename to `PrintStatus` or extract to a
shared helper) and call it from both commands. The `world status` command
should:

1. Print its config section
2. Call the shared status display function

Approach:
- Rename `printStatus` in `cmd/status.go` to `printWorldStatus` (still
  unexported — both callers are in package `cmd`)
- In `worldStatusCmd`, replace the duplicated lines (from `"Prefect:"`
  through `"Health:"`) with a call to `printWorldStatus(result)`
- This automatically adds the caravan section that `world status` was
  missing

---

## Task 6: Tests

### 6a. Test `world delete` with agents

**File:** `test/integration/world_lifecycle_test.go`

Add `TestWorldDeleteCleansUpAgents`:

```go
func TestWorldDeleteCleansUpAgents(t *testing.T) {
    // 1. Create a world
    // 2. Register an agent in it: sol agent create --world=X --name=Toast --role=dev
    // 3. Delete the world: sol world delete X --confirm
    // 4. Verify agent is gone from sphere.db:
    //    Open sphere store, list agents, confirm none for world X
}
```

### 6b. Test `world delete` with caravan items

**File:** `test/integration/world_lifecycle_test.go`

Add `TestWorldDeleteCleansUpCaravanItems`:

```go
func TestWorldDeleteCleansUpCaravanItems(t *testing.T) {
    // 1. Create a world, create a work item
    // 2. Create a caravan, add the work item
    // 3. Delete the world: sol world delete X --confirm
    // 4. Verify caravan still exists but item is gone:
    //    sol caravan status <id> should show 0 items
}
```

### 6c. Test `world list --json` empty

**File:** `test/integration/world_lifecycle_test.go`

Add `TestWorldListJSONEmpty`:

```go
func TestWorldListJSONEmpty(t *testing.T) {
    // sol world list --json with no worlds
    // Output should be valid JSON: []
    // Parse with json.Unmarshal to verify
}
```

---

## Task 7: Verify

1. `make build` — compiles cleanly
2. `make test` — all tests pass
3. Manual verification:
   ```bash
   export SOL_HOME=/tmp/sol-test-review
   mkdir -p /tmp/sol-test-review/.store
   bin/sol world init testworld --source-repo=/tmp/fakerepo
   bin/sol world status testworld
   # → should show config section AND caravan section (if any)
   bin/sol world delete testworld --confirm
   rm -rf /tmp/sol-test-review
   ```

---

## Guidelines

- The `DeleteWorldData` transaction is the most important change in this
  prompt — get it right.
- Keep `printWorldStatus` minimal: just rename and call. Don't refactor
  the display code beyond eliminating the duplication.
- All existing tests must continue to pass.
- Commit with message:
  `fix(world): arc 1 review — delete hardening, status dedup, caravan cleanup`
