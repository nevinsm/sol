# Prompt 01: Arc 3 — Schema V7 (Caravan Phases) + Brief Package

**Working directory:** ~/gt-src/
**Prerequisite:** Arc 2 complete

## Context

Read these files to understand existing patterns before making changes:

- `internal/store/schema.go` — migration pattern (how V1–V6 are applied)
- `internal/store/caravans.go` — `CreateCaravanItem`, `CheckCaravanReadiness`, `TryCloseCaravan`
- `internal/store/store.go` — how stores are opened, DSN conventions
- `docs/decisions/0013-brief-system.md` — brief system design
- `docs/arc-roadmap.md` — Arc 3 section on brief system and caravan phases

## Task 1: Schema V7 — Add `phase` Column to `caravan_items`

Add sphere DB migration V7 that adds a `phase` column to the `caravan_items` table.

In `internal/store/schema.go`, add the V7 migration following the existing pattern:

```sql
ALTER TABLE caravan_items ADD COLUMN phase INTEGER NOT NULL DEFAULT 0;
```

Update `currentSphereVersion` to 7.

Default 0 ensures backward compatibility — all existing items behave as phase 0
(immediate dispatch).

## Task 2: Update `CreateCaravanItem` to Accept Phase

In `internal/store/caravans.go`, update `CreateCaravanItem` to accept a `phase int`
parameter. Update the INSERT statement to include the phase column.

The function signature should be:

```go
func (s *Store) CreateCaravanItem(caravanID, workItemID, world string, phase int) error
```

Update all callers of `CreateCaravanItem` to pass `0` as the phase (preserving
existing behavior).

## Task 3: Update `CheckCaravanReadiness` for Phase Ordering

In `internal/store/caravans.go`, update `CheckCaravanReadiness` to enforce phase
ordering. An item in phase N is only `Ready` if:

1. All its within-world dependencies are satisfied (existing logic), AND
2. All items in phases < N are done or closed

Add a `Phase` field to `CaravanItemStatus`:

```go
type CaravanItemStatus struct {
    WorkItemID     string `json:"work_item_id"`
    World          string `json:"world"`
    Phase          int    `json:"phase"`
    WorkItemStatus string `json:"work_item_status"`
    Ready          bool   `json:"ready"`
}
```

Implementation approach:
1. Query all caravan items with their phases
2. For each phase > 0, check if all items in lower phases have status "done" or "closed"
3. If any lower-phase item is not done/closed, items in this phase are not ready
   (regardless of their own dependency status)
4. Phase 0 items use only the existing dependency check (no phase gate)

`TryCloseCaravan` should not need changes — it already checks all items regardless.

## Task 4: Brief Package — `internal/brief/`

Create `internal/brief/brief.go` with three functions:

### `Inject(path string, maxLines int) (string, error)`

Reads the brief file at `path`. If the file doesn't exist, returns empty string
and nil error (missing brief = clean start). If the file is empty, returns empty
string and nil error.

If content exceeds `maxLines`, truncate to `maxLines` and append a notice:

```go
func Inject(path string, maxLines int) (string, error)
```

Return value is the framed content ready for stdout:

```
<brief>
[contents, possibly truncated]
</brief>
```

If truncated, append before the closing tag:

```
---
TRUNCATED: Brief exceeded %d lines. Read the full file at %s and consolidate.
```

If the file is missing or empty, return `""` (no frame, no output).

### `WriteSessionStart(briefDir string) error`

Writes the current time (RFC3339 UTC) to `{briefDir}/.session_start`. Creates
the file if it doesn't exist, overwrites if it does.

```go
func WriteSessionStart(briefDir string) error
```

### `CheckSave(briefPath string) (bool, error)`

Compares the mtime of the brief file against the `.session_start` timestamp in
the same directory.

```go
func CheckSave(briefPath string) (bool, error)
```

Returns `(true, nil)` if the brief file was modified after `.session_start`.
Returns `(false, nil)` if the brief file was NOT modified (or doesn't exist).
Returns error only for unexpected filesystem failures.

If `.session_start` doesn't exist, return `(true, nil)` — no session start
recorded means we can't enforce the check.

## Task 5: Tests

### Schema V7 tests (in `internal/store/`)

Add to the existing schema test file or caravans test file:

- `TestCaravanPhaseDefault` — create caravan item without explicit phase, verify
  it defaults to 0
- `TestCaravanPhaseReadiness` — create a caravan with items in phases 0 and 1.
  Phase 0 items should be ready. Phase 1 items should NOT be ready until phase 0
  items are done. Mark phase 0 items done, verify phase 1 items become ready.
- `TestCaravanPhaseMultiple` — three phases (0, 1, 2). Only phase 0 ready initially.
  Complete phase 0, phase 1 becomes ready. Complete phase 1, phase 2 becomes ready.
- `TestCaravanPhaseMixedWorlds` — items across different worlds in same phase.
  Phase gate is per-caravan, not per-world.

### Brief package tests (`internal/brief/brief_test.go`)

- `TestInjectFileNotFound` — missing file returns empty string, nil error
- `TestInjectEmptyFile` — empty file returns empty string, nil error
- `TestInjectWithinLimit` — content within maxLines returns full content in frame
- `TestInjectExceedsLimit` — content over maxLines truncated with notice
- `TestInjectExactLimit` — content exactly at maxLines returns without truncation
- `TestWriteSessionStart` — writes timestamp file, verify RFC3339 format
- `TestWriteSessionStartOverwrite` — calling twice overwrites (not appends)
- `TestCheckSaveUpdated` — brief mtime after session_start returns true
- `TestCheckSaveNotUpdated` — brief mtime before session_start returns false
- `TestCheckSaveNoSessionStart` — missing .session_start returns true
- `TestCheckSaveNoBrief` — missing brief file returns false

## Verification

- `make build && make test` passes
- Schema V7 migration applies cleanly (verified by tests)
- Brief package has no external dependencies (only stdlib)

## Guidelines

- Brief package is pure library code — no CLI, no cobra, no store dependencies
- Phase ordering is per-caravan — items across different worlds in the same
  caravan share the same phase gate
- All existing callers of `CreateCaravanItem` pass phase 0 — no behavior change
- The brief frame format (`<brief>...</brief>`) is intentional — Claude Code
  recognizes XML-like tags as structured content

## Commit

```
feat(arc3): add caravan phases (schema V7) and brief package
```
