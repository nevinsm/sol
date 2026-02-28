# Prompt 02: Store Data Integrity

You are fixing error handling and data integrity issues in the store
layer found during the second Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 review-2 prompt 01 is complete. All tests pass.

Read all files mentioned below before making changes.

---

## Task 1: Fix `GetEscalation` masking real DB errors

**File:** `internal/store/escalations.go`

**Bug:** `GetEscalation` returns `"escalation %q not found"` for ANY
error from `QueryRow().Scan()`, including actual database errors (disk
I/O, corruption, etc.). It should distinguish `sql.ErrNoRows` from
other errors.

**Fix:**

```go
err := s.db.QueryRow(...).Scan(...)
if err == sql.ErrNoRows {
    return nil, fmt.Errorf("escalation %q not found", id)
}
if err != nil {
    return nil, fmt.Errorf("failed to get escalation %q: %w", id, err)
}
```

Make sure `database/sql` is imported.

---

## Task 2: Fix silent `time.Parse` errors in 5 store files

The first review pass fixed `time.Parse` errors in `worlds.go` and
`agents.go`. The same silent `_ =` pattern remains in 5 other files.

**Pattern to fix:** Replace every instance of:

```go
x.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
```

With:

```go
x.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
if err != nil {
    return nil, fmt.Errorf("failed to parse created_at for <type> %q: %w", id, err)
}
```

### File: `internal/store/workitems.go`

Fix `GetWorkItem` (around line 177) and `ListWorkItems` (around line 252).
Both have `created_at` and `updated_at` parse calls that discard errors.

In `GetWorkItem`, the ID is available as the function parameter.
In `ListWorkItems`, use `w.ID` (already scanned at that point).

### File: `internal/store/merge_requests.go`

Fix `GetMergeRequest` (around line 78) and `ListMergeRequests` (around
line 120). Both have `created_at` parse calls that discard errors.

### File: `internal/store/caravans.go`

Fix `GetCaravan` (around line 81) and `ListCaravans` (around line 119).
Both have `created_at` and `updated_at` parse calls that discard errors.

### File: `internal/store/escalations.go`

Fix `GetEscalation` (around line 79) and `ListEscalations` (around
line 113). Both have `created_at` parse calls that discard errors.

### File: `internal/store/messages.go`

Fix `GetMessage` (around line 113) and `ListMessages` (around line 204).
Both have `created_at` parse calls that discard errors.

**Important:** In each function, you will need to declare `var err error`
if the existing code uses short variable declarations (`:=`) for
`rows.Scan`. Alternatively, restructure to use named error returns. Pick
whichever approach is cleanest for each function.

---

## Task 3: Expand `DeleteWorldData` to clean up messages and escalations

**File:** `internal/store/worlds.go`

**Bug:** `DeleteWorldData` deletes `caravan_items`, `agents`, and the
`world` record, but orphans `messages` and `escalations` that reference
agents in the deleted world. Agent IDs follow the format `{world}/{name}`
(e.g., `myworld/Toast`).

**Fix:** Add message and escalation cleanup to the transaction, before
the agent deletion (since messages reference agents by sender/recipient):

```go
func (s *Store) DeleteWorldData(world string) error {
    tx, err := s.db.Begin()
    if err != nil {
        return fmt.Errorf("failed to begin transaction: %w", err)
    }
    defer tx.Rollback()

    // Clean up messages where sender or recipient is an agent in this world.
    // Agent IDs are formatted as "{world}/{name}".
    pattern := world + "/%"
    if _, err := tx.Exec(`DELETE FROM messages WHERE sender LIKE ? OR recipient LIKE ?`, pattern, pattern); err != nil {
        return fmt.Errorf("failed to delete messages for world %q: %w", world, err)
    }

    // Clean up escalations sourced from this world.
    if _, err := tx.Exec(`DELETE FROM escalations WHERE source LIKE ?`, pattern); err != nil {
        return fmt.Errorf("failed to delete escalations for world %q: %w", world, err)
    }

    // Existing cleanup...
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

Read the `messages` and `escalations` table schemas in `schema.go` to
verify the column names (`sender`, `recipient`, `source`).

**Note:** The `LIKE` pattern with `world/%` is safe because world names
are validated to contain only `[a-zA-Z0-9_-]` — no SQL LIKE wildcards.

---

## Task 4: Remove dead `RemoveWorld` method

**File:** `internal/store/worlds.go`

**Bug:** `RemoveWorld` is a simpler version of `DeleteWorldData` that
only deletes the world record without cleaning up related data. It is
never called outside of test code and could mislead future developers
into using the incomplete deletion path.

**Fix:**
1. Remove the `RemoveWorld` method from `worlds.go`.
2. Update any tests that call `RemoveWorld` to use `DeleteWorldData`
   instead. Check `worlds_test.go` for `TestRemoveWorld` and related
   tests — rewrite them to test `DeleteWorldData` if needed, or remove
   them if `DeleteWorldData` is already tested.
3. Grep for `RemoveWorld` across the codebase to ensure no remaining
   references.

---

## Task 5: Add JSON tags to `store.World` struct

**File:** `internal/store/worlds.go`

**Bug:** The `World` struct has no JSON tags. If ever serialized directly,
fields would be PascalCase (`SourceRepo`) instead of snake_case
(`source_repo`), inconsistent with every other struct in the store.

**Fix:**

```go
type World struct {
    Name       string    `json:"name"`
    SourceRepo string    `json:"source_repo"`
    CreatedAt  time.Time `json:"created_at"`
    UpdatedAt  time.Time `json:"updated_at"`
}
```

Check `cmd/world.go` — the `worldListCmd` uses a local `worldJSON`
struct to work around this. After adding JSON tags to `store.World`,
evaluate whether `worldJSON` can be replaced with `store.World` directly.
If the fields match 1:1, simplify the list command to use `store.World`.
If the list command intentionally omits `UpdatedAt`, keep the local
struct but add a comment explaining why.

---

## Task 6: Verify

1. `make build` — compiles cleanly
2. `make test` — all tests pass
3. Grep verification:
   ```bash
   # No silent time.Parse in store layer
   grep -rn 'time\.Parse.*_ =' internal/store/*.go
   # → no matches

   # No RemoveWorld method
   grep -rn 'func.*RemoveWorld\|\.RemoveWorld(' internal/store/ cmd/
   # → no matches

   # GetEscalation distinguishes ErrNoRows
   grep -A5 'ErrNoRows' internal/store/escalations.go
   # → should show the sql.ErrNoRows check

   # DeleteWorldData cleans messages and escalations
   grep -n 'messages\|escalations' internal/store/worlds.go
   # → should show DELETE statements for both
   ```

---

## Guidelines

- The `time.Parse` fixes are mechanical but must be done carefully —
  each function has slightly different variable scoping.
- The `DeleteWorldData` expansion adds LIKE queries. Verify the agent ID
  format by reading `internal/store/agents.go` — the `CreateAgent`
  function should show how agent IDs are constructed.
- When removing `RemoveWorld`, check if any integration tests call it
  indirectly through the CLI. Grep broadly.
- All existing tests must continue to pass.
- Commit with message:
  `fix(store): arc 1 review-2 — error handling, data cleanup, dead code`
