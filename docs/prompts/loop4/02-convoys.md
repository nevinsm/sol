# Prompt 02: Loop 4 — Convoys and Dependencies

You are extending the `gt` orchestration system with convoy tracking and
work item dependencies. Convoys group related work items into batches
with dependency ordering. As items merge, the system tracks readiness
so the next batch of work can be dispatched.

**Working directory:** `~/gt-src/`
**Prerequisite:** Loop 4 prompt 01 (workflow engine) is complete.

Read all existing code first. Understand the store package
(`internal/store/` — especially `schema.go`, `workitems.go`, and
`merge_requests.go`), the dispatch package (`internal/dispatch/`), and
the protocol package (`internal/store/protocol.go`).

Read `docs/target-architecture.md` — the convoy schema (Section 3.1
under town.db), Section 2.11 (Workflow Orchestration), and the Loop 4
definition of done.

---

## Task 1: Schema Migration — Dependencies

Add a `dependencies` table to the rig database.

### Rig Schema V4

Add `rigSchemaV4` to `internal/store/schema.go`:

```sql
CREATE TABLE IF NOT EXISTS dependencies (
    from_id TEXT NOT NULL REFERENCES work_items(id),
    to_id   TEXT NOT NULL REFERENCES work_items(id),
    PRIMARY KEY (from_id, to_id)
);
CREATE INDEX IF NOT EXISTS idx_deps_from ON dependencies(from_id);
CREATE INDEX IF NOT EXISTS idx_deps_to ON dependencies(to_id);
```

Semantics: `from_id` depends on `to_id` — `to_id` must reach status
`closed` or `done` before `from_id` is ready for dispatch.

Update `migrateRig()` to apply V4 and set version to 4.

### Dependency CRUD

Create `internal/store/dependencies.go`:

```go
// AddDependency records that fromID depends on toID.
// Both work items must exist. Returns error on cycle detection.
func (s *Store) AddDependency(fromID, toID string) error

// RemoveDependency removes a dependency relationship.
func (s *Store) RemoveDependency(fromID, toID string) error

// GetDependencies returns the IDs of work items that itemID depends on.
// (What must complete before this item can start.)
func (s *Store) GetDependencies(itemID string) ([]string, error)

// GetDependents returns the IDs of work items that depend on itemID.
// (What is waiting for this item to complete.)
func (s *Store) GetDependents(itemID string) ([]string, error)

// IsReady returns true if all dependencies of itemID are satisfied
// (status is "done" or "closed"). An item with no dependencies is
// always ready.
func (s *Store) IsReady(itemID string) (bool, error)
```

### Cycle Detection

`AddDependency` must reject cycles. Before inserting, check if adding
the edge `from → to` would create a cycle by walking the dependency
graph from `to` to see if `from` is reachable:

```go
func (s *Store) wouldCreateCycle(fromID, toID string) (bool, error) {
    // BFS/DFS from toID following dependencies.
    // If we reach fromID, adding from→to creates a cycle.
    visited := map[string]bool{}
    queue := []string{toID}
    for len(queue) > 0 {
        current := queue[0]
        queue = queue[1:]
        if current == fromID {
            return true, nil
        }
        if visited[current] {
            continue
        }
        visited[current] = true
        deps, err := s.GetDependencies(current)
        if err != nil {
            return false, err
        }
        queue = append(queue, deps...)
    }
    return false, nil
}
```

---

## Task 2: Schema Migration — Convoys

Add convoy tables to the town database.

### Town Schema V3

Add `townSchemaV3` to `internal/store/schema.go`:

```sql
CREATE TABLE IF NOT EXISTS convoys (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'open',
    owner      TEXT,
    created_at TEXT NOT NULL,
    closed_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_convoys_status ON convoys(status);

CREATE TABLE IF NOT EXISTS convoy_items (
    convoy_id    TEXT NOT NULL REFERENCES convoys(id),
    work_item_id TEXT NOT NULL,
    rig          TEXT NOT NULL,
    PRIMARY KEY (convoy_id, work_item_id)
);
CREATE INDEX IF NOT EXISTS idx_convoy_items_convoy ON convoy_items(convoy_id);
```

Update `migrateTown()` to apply V3 and set version to 3.

### Convoy Types

```go
// internal/store/convoys.go

// Convoy represents a group of related work items tracked together.
type Convoy struct {
    ID        string
    Name      string
    Status    string     // "open", "ready", "closed"
    Owner     string     // agent or operator who created it
    CreatedAt time.Time
    ClosedAt  *time.Time
}

// ConvoyItem is a work item associated with a convoy.
type ConvoyItem struct {
    ConvoyID   string
    WorkItemID string
    Rig        string
}
```

### Convoy ID Format

Convoy IDs follow the same pattern as other IDs: `"convoy-"` + 8 hex
chars from `crypto/rand`.

### Convoy CRUD

```go
// CreateConvoy creates a convoy with the given name and owner.
// Returns the convoy ID.
func (s *Store) CreateConvoy(name, owner string) (string, error)

// GetConvoy returns a convoy by ID.
func (s *Store) GetConvoy(id string) (*Convoy, error)

// ListConvoys returns convoys, optionally filtered by status.
// If status is empty, returns all convoys.
// Ordered by created_at DESC (newest first).
func (s *Store) ListConvoys(status string) ([]Convoy, error)

// UpdateConvoyStatus sets the convoy's status. If status is "closed",
// also sets closed_at.
func (s *Store) UpdateConvoyStatus(id, status string) error

// AddConvoyItem associates a work item with a convoy.
func (s *Store) AddConvoyItem(convoyID, workItemID, rig string) error

// RemoveConvoyItem removes a work item from a convoy.
func (s *Store) RemoveConvoyItem(convoyID, workItemID string) error

// ListConvoyItems returns all items in a convoy.
func (s *Store) ListConvoyItems(convoyID string) ([]ConvoyItem, error)
```

### Convoy Readiness

Convoy readiness depends on work item dependencies and merge status.
Add a helper that checks readiness of convoy items across rigs:

```go
// ConvoyItemStatus represents the status of a work item within a convoy.
type ConvoyItemStatus struct {
    ConvoyItem
    WorkItemStatus string // status from the rig's work_items table
    Ready          bool   // true if all dependencies are satisfied
}

// CheckConvoyReadiness returns the status of all items in a convoy.
// This requires opening each rig's database to check work item status
// and dependency satisfaction.
//
// The rigOpener function opens a rig store by name — the caller provides
// this so the convoy checker doesn't need to know about store paths.
func (s *Store) CheckConvoyReadiness(convoyID string,
    rigOpener func(rig string) (*Store, error)) ([]ConvoyItemStatus, error)
```

The function:
1. Lists all convoy items
2. Groups items by rig
3. For each rig, opens the rig store
4. For each item in that rig: get work item status, check `IsReady()`
5. Returns the combined results

An item is "ready for dispatch" when:
- Its work item status is `"open"` (not yet assigned)
- All its dependencies are satisfied (`IsReady()` returns true)

### Auto-Close

```go
// TryCloseConvoy checks if all items in a convoy are done/closed.
// If so, sets the convoy status to "closed".
// Returns true if the convoy was closed.
func (s *Store) TryCloseConvoy(convoyID string,
    rigOpener func(rig string) (*Store, error)) (bool, error)
```

---

## Task 3: CLI Commands

### gt convoy create

```
gt convoy create <name> --rig=<rig> [--owner=<owner>] [<item-id> ...]
```

- `<name>`: convoy name (e.g., "auth-feature")
- `--rig`: rig for the listed items (all items in one `create` call
  share a rig — use multiple `gt convoy add` for multi-rig)
- `--owner`: optional owner (default: "operator")
- `<item-id>`: zero or more work item IDs to add immediately

**Behavior:**
1. Create convoy record in town.db
2. Add each listed item to the convoy
3. Print: `Created convoy <id>: "<name>" (N items)`

### gt convoy add

```
gt convoy add <convoy-id> --rig=<rig> <item-id> [<item-id> ...]
```

Add items to an existing convoy. Used for multi-rig convoys or adding
items after creation.

### gt convoy check

```
gt convoy check <convoy-id> [--json]
```

**Behavior:**
1. Call `CheckConvoyReadiness`
2. Print ready items and blocked items separately

**Human output:**
```
Convoy: auth-feature (convoy-a1b2c3d4)
Status: open

Ready for dispatch:
  gt-11111111  Add login validation      (myrig)
  gt-22222222  Add password reset        (myrig)

Blocked:
  gt-33333333  Add session management    (myrig)  ← waiting on gt-11111111
  gt-44444444  Integration tests         (myrig)  ← waiting on gt-22222222, gt-33333333
```

For blocked items, show which dependencies are not yet satisfied.

### gt convoy status

```
gt convoy status [<convoy-id>] [--json]
```

If `<convoy-id>` provided, show detailed status for that convoy (same as
`check` but includes completed items). If omitted, list all open convoys:

```
Open convoys:
  convoy-a1b2c3d4  auth-feature     4 items  (2 done, 1 ready, 1 blocked)
  convoy-e5f6a7b8  api-refactor     6 items  (0 done, 3 ready, 3 blocked)
```

### gt convoy launch

```
gt convoy launch <convoy-id> --rig=<rig> [--formula=<name>] [--var=key=val ...]
```

**Behavior:**
1. Call `CheckConvoyReadiness`
2. For each ready item in the specified rig:
   - Call `gt sling <item-id> <rig>` (or the dispatch function directly)
   - If `--formula` provided, pass it to sling for workflow instantiation
     (this wiring is done in prompt 03)
3. Print dispatched items and remaining blocked items
4. Call `TryCloseConvoy` to auto-close if all items done

### gt store dep

Add dependency management to the existing `gt store` command group:

```
gt store dep add <from-id> <to-id> --db=<rig>
gt store dep remove <from-id> <to-id> --db=<rig>
gt store dep list <item-id> --db=<rig> [--json]
```

- `add`: add dependency (from depends on to)
- `remove`: remove dependency
- `list`: show what an item depends on and what depends on it

**List output:**
```
Work item: gt-33333333

Depends on:
  gt-11111111  Add login validation  (done)

Depended on by:
  gt-44444444  Integration tests     (open)
```

---

## Task 4: Protocol Message

Add a convoy-related protocol message type for future use (Loop 5
deacon will consume it):

In `internal/store/protocol.go`, add:

```go
const ProtoConvoyNeedsFeeding = "CONVOY_NEEDS_FEEDING"

// ConvoyNeedsFeedingPayload is sent when a convoy has items ready
// for dispatch (e.g., after a merge unblocks dependent work).
type ConvoyNeedsFeedingPayload struct {
    ConvoyID string `json:"convoy_id"`
    Rig      string `json:"rig"`
    ReadyCount int  `json:"ready_count"`
}
```

This message type is defined now but not emitted yet — Loop 5's deacon
will emit it when convoy readiness changes.

---

## Task 5: Status Integration

Extend `internal/status/status.go` to include convoy summary when
displaying rig status.

In the `RigStatus` struct (or equivalent), add:

```go
type ConvoyInfo struct {
    ID          string
    Name        string
    Status      string
    TotalItems  int
    ReadyItems  int
    DoneItems   int
}
```

When gathering status, query open convoys that have items in this rig
and include summary counts.

---

## Task 6: Tests

### Dependency Tests

Create `internal/store/dependencies_test.go`:

```go
func TestAddDependency(t *testing.T)
    // Add dependency between two items → success
    // Verify with GetDependencies

func TestAddDependencyCycleDetection(t *testing.T)
    // A depends on B, B depends on A → error
    // A→B, B→C, C→A → error (transitive cycle)

func TestRemoveDependency(t *testing.T)
    // Add then remove → GetDependencies returns empty

func TestGetDependencies(t *testing.T)
    // Item with 3 deps → returns all 3
    // Item with no deps → returns empty slice

func TestGetDependents(t *testing.T)
    // 3 items depend on X → returns all 3

func TestIsReady(t *testing.T)
    // Item with no deps → ready
    // Item with dep on "open" item → not ready
    // Item with dep on "done" item → ready
    // Item with dep on "closed" item → ready
    // Item with mixed deps (one done, one open) → not ready
```

### Convoy Tests

Create `internal/store/convoys_test.go`:

```go
func TestCreateConvoy(t *testing.T)
    // Create → returns valid ID with "convoy-" prefix
    // Verify with GetConvoy

func TestAddConvoyItem(t *testing.T)
    // Add 3 items → ListConvoyItems returns 3

func TestRemoveConvoyItem(t *testing.T)
    // Add then remove → ListConvoyItems returns fewer

func TestListConvoys(t *testing.T)
    // Create 3 convoys → list all → 3
    // List by status="open" → filters correctly

func TestUpdateConvoyStatus(t *testing.T)
    // Update to "closed" → sets closed_at

func TestCheckConvoyReadiness(t *testing.T)
    // Convoy with 3 items, no deps → all ready
    // Convoy with deps: A→B, C (no deps)
    //   B open → A not ready, C ready
    //   B done → A ready, C ready

func TestTryCloseConvoy(t *testing.T)
    // All items done/closed → convoy auto-closed
    // Some items open → convoy stays open
```

### CLI Smoke Tests

Add to `test/integration/cli_loop4_test.go`:

```go
func TestCLIConvoyCreateHelp(t *testing.T)
func TestCLIConvoyAddHelp(t *testing.T)
func TestCLIConvoyCheckHelp(t *testing.T)
func TestCLIConvoyStatusHelp(t *testing.T)
func TestCLIConvoyLaunchHelp(t *testing.T)
func TestCLIStoreDepAddHelp(t *testing.T)
func TestCLIStoreDepListHelp(t *testing.T)
```

---

## Task 7: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export GT_HOME=/tmp/gt-test
   mkdir -p /tmp/gt-test/.store

   # Create rig store with work items
   bin/gt store create --title="Add login" --db=myrig
   bin/gt store create --title="Add auth middleware" --db=myrig
   bin/gt store create --title="Integration tests" --db=myrig

   # Add dependencies: integration tests depend on both others
   bin/gt store dep add gt-<tests-id> gt-<login-id> --db=myrig
   bin/gt store dep add gt-<tests-id> gt-<auth-id> --db=myrig
   bin/gt store dep list gt-<tests-id> --db=myrig

   # Create a convoy
   bin/gt convoy create "auth-feature" --rig=myrig \
     gt-<login-id> gt-<auth-id> gt-<tests-id>

   # Check readiness
   bin/gt convoy check <convoy-id>
   # → login and auth ready, tests blocked

   bin/gt convoy status
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Guidelines

- Convoys span multiple rigs — the `rig` column on `convoy_items`
  allows a single convoy to track items across different rigs. The
  `CheckConvoyReadiness` function opens each rig's database as needed.
- Cycle detection is mandatory — `AddDependency` must reject cycles to
  prevent deadlocks in the dispatch graph.
- Convoy IDs use the same format as other IDs: `"convoy-"` + 8 hex
  chars.
- All timestamps are RFC3339 in UTC.
- Error messages include context with `%w` wrapping.
- The `CONVOY_NEEDS_FEEDING` protocol message is defined but not emitted
  yet — the deacon (Loop 5) will be responsible for emitting it when it
  detects a convoy with unstarted ready items.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(convoy): add convoy tracking with dependencies and readiness checking`
