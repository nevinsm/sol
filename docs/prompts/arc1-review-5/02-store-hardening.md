# Prompt 02: Arc 1 Review-5 — Store Hardening

You are fixing store-layer issues found during the fifth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 01 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/store/schema.go` — migration functions, schema DDL
- `internal/store/store.go` — open/close, DB() method
- `internal/store/workitems.go` — generateID, CreateWorkItem
- `internal/store/agents.go` — UpdateAgentState
- `internal/store/caravans.go` — UpdateCaravanStatus
- `internal/store/messages.go` — generateMessageID
- `internal/store/escalations.go` — generateEscalationID
- `internal/store/merge_requests.go` — generateMergeRequestID

---

## Task 1: Wrap migrations in transactions

**File:** `internal/store/schema.go`

The `migrateWorld()` and `migrateSphere()` functions run each migration step as a separate `Exec` call. If the process crashes mid-migration (e.g., after applying V3 DDL but before updating schema_version), the database is in a partially-migrated state. While most DDL uses `IF NOT EXISTS`, `ALTER TABLE ... ADD COLUMN` is not idempotent (the `columnExists` guard mitigates this for V3, but the pattern is fragile).

**Fix:** Wrap each migration function's body in a transaction. The entire migration runs atomically or not at all.

For `migrateWorld()`:

```go
func (s *Store) migrateWorld() error {
	v, err := s.schemaVersion()
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v >= 5 {
		return nil // already at latest version
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	if v < 1 {
		if _, err := tx.Exec(worldSchemaV1); err != nil {
			return fmt.Errorf("failed to create world schema v1: %w", err)
		}
	}
	if v < 2 {
		if _, err := tx.Exec(worldSchemaV2); err != nil {
			return fmt.Errorf("failed to create world schema v2: %w", err)
		}
	}
	if v < 3 {
		exists, err := columnExists(tx, "merge_requests", "blocked_by")
		if err != nil {
			return fmt.Errorf("failed to check merge_requests schema: %w", err)
		}
		if !exists {
			if _, err := tx.Exec(worldSchemaV3); err != nil {
				return fmt.Errorf("failed to apply world schema v3: %w", err)
			}
		}
	}
	if v < 4 {
		if _, err := tx.Exec(worldSchemaV4); err != nil {
			return fmt.Errorf("failed to apply world schema v4: %w", err)
		}
	}
	if v < 5 {
		if _, err := tx.Exec(worldSchemaV5); err != nil {
			return fmt.Errorf("failed to apply world schema v5: %w", err)
		}
	}
	if v < 1 {
		if _, err := tx.Exec("INSERT INTO schema_version VALUES (5)"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	} else {
		if _, err := tx.Exec("UPDATE schema_version SET version = 5"); err != nil {
			return fmt.Errorf("failed to set schema version: %w", err)
		}
	}
	return tx.Commit()
}
```

Apply the same pattern to `migrateSphere()`: wrap all operations from the first `if v < 1` through the final version update in a single transaction. Note: `columnExists` and `tableExists` currently accept an interface — ensure they also work when passed a `*sql.Tx`. The `*sql.Tx` type already satisfies both `QueryRow` and `Query` interfaces, so no signature change is needed.

For `migrateSphere()`, the `columnExists` calls inside the `v < 4` block must use `tx` instead of `s.db`:

```go
func (s *Store) migrateSphere() error {
	v, err := s.schemaVersion()
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}
	if v >= 6 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	// ... all migration steps use tx instead of s.db ...
	// ... columnExists(tx, ...) and tableExists(tx, ...) ...

	return tx.Commit()
}
```

**Important:** SQLite DDL within transactions works correctly for `CREATE TABLE`, `CREATE INDEX`, and `ALTER TABLE`. The `IF NOT EXISTS` / `columnExists` guards remain for safety.

---

## Task 2: Increase ID entropy from 4 to 8 bytes

**File:** `internal/store/workitems.go` — `generateID()`

The current 4-byte (32-bit) random ID hits 50% collision probability at ~65K items. For a system with 10-30+ agents generating work items over time, this is a real risk. The INSERT will fail on collision (PRIMARY KEY), but there's no retry.

**Fix:** Increase to 8 bytes (64 bits). The ID format stays `"sol-" + hex`, but grows from 8 hex chars to 16:

```go
func generateID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate work item ID: %w", err)
	}
	return "sol-" + hex.EncodeToString(b), nil
}
```

Apply the same change to all other ID generators in the store package. Find them all — they follow the same `make([]byte, 4)` pattern:

- `internal/store/messages.go` — `generateMessageID()`
- `internal/store/escalations.go` — `generateEscalationID()`
- `internal/store/caravans.go` — `generateCaravanID()`
- `internal/store/merge_requests.go` — `generateMergeRequestID()`

Each should change from `make([]byte, 4)` to `make([]byte, 8)`.

**Note:** The ID format in CLAUDE.md says "sol-" + 8 hex chars. This change makes them 16 hex chars. That's fine — the format is a convention, not a schema constraint. Existing IDs in databases remain valid (they're just shorter). Do NOT update CLAUDE.md — that's a separate concern.

Update any tests that assert exact ID format length. Search for tests matching `len(id) == 12` or `len("sol-") + 8` patterns and update to expect 20 characters (`"sol-"` + 16 hex chars).

---

## Task 3: Add status validation to UpdateCaravanStatus

**File:** `internal/store/caravans.go`, `UpdateCaravanStatus` function (around line 147)

Unlike `UpdateWorkItem` which validates against `validWorkItemStatuses`, `UpdateCaravanStatus` accepts any arbitrary string.

**Fix:** Add a valid-statuses check at the top of the function:

```go
var validCaravanStatuses = map[string]bool{
	"open":   true,
	"ready":  true,
	"closed": true,
}

func (s *Store) UpdateCaravanStatus(id, status string) error {
	if !validCaravanStatuses[status] {
		return fmt.Errorf("invalid caravan status %q", status)
	}
	// ... rest unchanged
}
```

---

## Task 4: Add state validation to UpdateAgentState

**File:** `internal/store/agents.go`, `UpdateAgentState` function (around line 68)

Same issue — accepts any arbitrary state string.

**Fix:** Add a valid-states check:

```go
var validAgentStates = map[string]bool{
	"idle":    true,
	"working": true,
	"stalled": true,
}

func (s *Store) UpdateAgentState(id, state, tetherItem string) error {
	if !validAgentStates[state] {
		return fmt.Errorf("invalid agent state %q", state)
	}
	// ... rest unchanged
}
```

---

## Task 5: Add ErrNotFound sentinel error

**File:** `internal/store/store.go`

All `Get*` functions return `fmt.Errorf("X %q not found", id)` when `sql.ErrNoRows` is hit. Callers cannot use `errors.Is()` to distinguish "not found" from other errors — they'd need string matching.

**Fix:** Define a sentinel error and use it consistently:

In `internal/store/store.go`, add:

```go
import "errors"

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")
```

Then update every "not found" error in the store to wrap `ErrNotFound`:

| File | Function | Current | New |
|------|----------|---------|-----|
| `agents.go` | `GetAgent` | `fmt.Errorf("agent %q not found", id)` | `fmt.Errorf("agent %q: %w", id, ErrNotFound)` |
| `agents.go` | `UpdateAgentState` | `fmt.Errorf("agent %q not found", id)` | `fmt.Errorf("agent %q: %w", id, ErrNotFound)` |
| `workitems.go` | `GetWorkItem` | `fmt.Errorf("work item %q not found", id)` | `fmt.Errorf("work item %q: %w", id, ErrNotFound)` |
| `escalations.go` | `GetEscalation` | `fmt.Errorf("escalation %q not found", id)` | `fmt.Errorf("escalation %q: %w", id, ErrNotFound)` |
| `caravans.go` | `GetCaravan` | `fmt.Errorf("caravan %q not found", id)` | `fmt.Errorf("caravan %q: %w", id, ErrNotFound)` |
| `caravans.go` | `UpdateCaravanStatus` | `fmt.Errorf("caravan %q not found", id)` | `fmt.Errorf("caravan %q: %w", id, ErrNotFound)` |
| `merge_requests.go` | `GetMergeRequest` | `fmt.Errorf("merge request %q not found", id)` | `fmt.Errorf("merge request %q: %w", id, ErrNotFound)` |
| `worlds.go` | `GetWorld` | `fmt.Errorf("world %q not found", name)` | `fmt.Errorf("world %q: %w", name, ErrNotFound)` |

Also update `worlds.go` `GetWorld` to use `errors.Is` instead of `==` for `sql.ErrNoRows`:

```go
// Before:
if err == sql.ErrNoRows {
// After:
if errors.Is(err, sql.ErrNoRows) {
```

Do the same for all other `err == sql.ErrNoRows` comparisons in the store package.

---

## Task 6: Tests

Add a test in `internal/store/store_test.go`:

```go
func TestErrNotFound(t *testing.T)
```
- Call `GetAgent("nonexistent")` and verify `errors.Is(err, store.ErrNotFound)` is true
- Call `GetWorkItem("nonexistent")` and verify `errors.Is(err, store.ErrNotFound)` is true
- Verify the error message still contains the entity ID (e.g., `strings.Contains(err.Error(), "nonexistent")`)

Add a test for status validation:

```go
func TestInvalidCaravanStatus(t *testing.T)
```
- Create a caravan, then call `UpdateCaravanStatus(id, "banana")`
- Verify it returns an error containing "invalid caravan status"

```go
func TestInvalidAgentState(t *testing.T)
```
- Create an agent, then call `UpdateAgentState(id, "banana", "")`
- Verify it returns an error containing "invalid agent state"

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Run new tests: `go test -v -run "TestErrNotFound|TestInvalidCaravan|TestInvalidAgent" ./internal/store/`

## Commit

```
fix(store): arc 1 review-5 — transactional migrations, 8-byte IDs, status validation, ErrNotFound
```
