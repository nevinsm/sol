# Prompt 01: Loop 2 — Merge Request Store + Done Extension

You are extending the `sol` orchestration system with the data layer for the
merge pipeline. This prompt adds the `merge_requests` table to the world
database, store CRUD operations for merge requests, a per-world merge slot
lock, and modifies `sol resolve` to submit completed work to the merge queue
instead of finalizing it immediately.

**Working directory:** `~/sol-src/`
**Prerequisite:** Loop 1 is complete (prompts 01–04).

Read all existing code first. Understand the store package
(`internal/store/` — schema versioning, work items, agents), the dispatch
package (`internal/dispatch/` — especially `Done()` and the flock pattern
in `flock.go`), and the config package (`internal/config/config.go`).

Read `docs/target-architecture.md` Section 3.9 (Forge) and Section 5
(Build Loops, Loop 2 requirements) for design context. Pay close attention
to the `merge_requests` table schema and the merge pipeline flow.

---

## Task 1: World Schema V2 — Merge Requests Table

Add a V2 migration to `internal/store/schema.go` that creates the
`merge_requests` table. The V1 schema (work items, labels) is unchanged.

### Schema

```sql
CREATE TABLE IF NOT EXISTS merge_requests (
    id           TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES work_items(id),
    branch       TEXT NOT NULL,
    phase        TEXT NOT NULL DEFAULT 'ready',
    claimed_by   TEXT,
    claimed_at   TEXT,
    attempts     INTEGER NOT NULL DEFAULT 0,
    priority     INTEGER NOT NULL DEFAULT 2,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    merged_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_mr_phase ON merge_requests(phase);
CREATE INDEX IF NOT EXISTS idx_mr_work_item ON merge_requests(work_item_id);
```

**Fields:**
- `id`: `"mr-"` + 8 random hex chars (e.g., `mr-a1b2c3d4`)
- `work_item_id`: FK to work_items — the work this merge request is for
- `branch`: the outpost's branch name (e.g., `outpost/Toast/sol-a1b2c3d4`)
- `phase`: state machine — `ready`, `claimed`, `merged`, `failed`
- `claimed_by`: forge agent ID that claimed this MR (null when ready)
- `claimed_at`: RFC3339 UTC timestamp when claimed (null when ready)
- `attempts`: number of merge attempts (incremented on each claim)
- `priority`: inherited from work item priority (lower = higher priority)
- `created_at`, `updated_at`: RFC3339 UTC timestamps
- `merged_at`: RFC3339 UTC timestamp when successfully merged (null until then)

### Migration Pattern

Follow the existing V1 migration pattern. Add a `worldSchemaV2` constant
and extend `migrateRig()`:

```go
const worldSchemaV2 = `
CREATE TABLE IF NOT EXISTS merge_requests (
    ...
);
CREATE INDEX IF NOT EXISTS idx_mr_phase ON merge_requests(phase);
CREATE INDEX IF NOT EXISTS idx_mr_work_item ON merge_requests(work_item_id);
`

func (s *Store) migrateRig() error {
    v, err := s.schemaVersion()
    if err != nil {
        return fmt.Errorf("failed to check schema version: %w", err)
    }
    if v < 1 {
        // Apply V1 schema (existing)
        if _, err := s.db.Exec(worldSchemaV1); err != nil {
            return fmt.Errorf("failed to create world schema v1: %w", err)
        }
    }
    if v < 2 {
        // Apply V2 schema (new)
        if _, err := s.db.Exec(worldSchemaV2); err != nil {
            return fmt.Errorf("failed to create world schema v2: %w", err)
        }
    }
    if v < 2 {
        if _, err := s.db.Exec("UPDATE schema_version SET version = 2"); err != nil {
            return fmt.Errorf("failed to set schema version: %w", err)
        }
    }
    return nil
}
```

Handle the case where V1 already ran (schema_version = 1): only run the
V2 DDL and update the version to 2. Handle fresh databases: run both V1
and V2, set version to 2.

---

## Task 2: MergeRequest Struct and Store CRUD

Create `internal/store/merge_requests.go` with the MergeRequest type and
all CRUD operations.

### Data Structure

```go
// internal/store/merge_requests.go
package store

import "time"

// MergeRequest represents a merge request in the world database.
type MergeRequest struct {
    ID         string
    WorkItemID string
    Branch     string
    Phase      string    // ready, claimed, merged, failed
    ClaimedBy  string    // forge agent ID (empty if unclaimed)
    ClaimedAt  *time.Time
    Attempts   int
    Priority   int
    CreatedAt  time.Time
    UpdatedAt  time.Time
    MergedAt   *time.Time
}
```

### CRUD Operations

```go
// CreateMergeRequest creates a new merge request with phase=ready.
// Returns the generated MR ID (mr-XXXXXXXX).
func (s *Store) CreateMergeRequest(workItemID, branch string, priority int) (string, error)

// GetMergeRequest returns a merge request by ID.
func (s *Store) GetMergeRequest(id string) (*MergeRequest, error)

// ListMergeRequests returns merge requests filtered by phase.
// If phase is empty, returns all. Ordered by priority ASC, created_at ASC
// (highest priority first, oldest first within same priority).
func (s *Store) ListMergeRequests(phase string) ([]MergeRequest, error)

// ClaimMergeRequest atomically claims the next ready merge request.
// Sets phase=claimed, claimed_by=claimerID, claimed_at=now, attempts++.
// Returns the claimed MR, or nil if no ready MRs exist.
// Uses a single UPDATE ... WHERE to prevent races.
func (s *Store) ClaimMergeRequest(claimerID string) (*MergeRequest, error)

// UpdateMergeRequestPhase updates the phase of a merge request.
// Also sets updated_at=now. If phase=merged, also sets merged_at=now.
// If phase=ready, clears claimed_by and claimed_at (release).
func (s *Store) UpdateMergeRequestPhase(id, phase string) error

// ReleaseStaleClaims releases merge requests that have been claimed for
// longer than the given TTL. Sets them back to phase=ready, clears
// claimed_by and claimed_at. Returns the number of released MRs.
func (s *Store) ReleaseStaleClaims(ttl time.Duration) (int, error)
```

### Implementation Notes

**ID generation:** Use the same pattern as work items — `"mr-"` +
`crypto/rand` 4-byte hex:
```go
func generateMRID() string {
    b := make([]byte, 4)
    rand.Read(b)
    return "mr-" + hex.EncodeToString(b)
}
```

**ClaimMergeRequest** must be atomic to prevent two forges from
claiming the same MR. Use a single SQL statement:

```sql
UPDATE merge_requests
SET phase = 'ready', claimed_by = ?, claimed_at = ?,
    attempts = attempts + 1, updated_at = ?
WHERE id = (
    SELECT id FROM merge_requests
    WHERE phase = 'ready'
    ORDER BY priority ASC, created_at ASC
    LIMIT 1
)
RETURNING id, work_item_id, branch, phase, claimed_by, claimed_at,
          attempts, priority, created_at, updated_at, merged_at
```

Wait — the SET should use `phase = 'claimed'`, not `'ready'`. Fix:

```sql
UPDATE merge_requests
SET phase = 'claimed', claimed_by = ?, claimed_at = ?,
    attempts = attempts + 1, updated_at = ?
WHERE id = (
    SELECT id FROM merge_requests
    WHERE phase = 'ready'
    ORDER BY priority ASC, created_at ASC
    LIMIT 1
)
RETURNING id, work_item_id, branch, phase, claimed_by, claimed_at,
          attempts, priority, created_at, updated_at, merged_at
```

If no rows match, `RETURNING` returns no rows — return `(nil, nil)`.

**ReleaseStaleClaims** should use:

```sql
UPDATE merge_requests
SET phase = 'ready', claimed_by = NULL, claimed_at = NULL, updated_at = ?
WHERE phase = 'claimed' AND claimed_at < ?
```

Where the threshold is `time.Now().UTC().Add(-ttl)`.

**Error messages:**
- Not found: `"merge request %q not found"`
- Create failure: `"failed to create merge request: %w"`
- Claim failure: `"failed to claim merge request: %w"`
- Invalid phase: `"invalid merge request phase %q"`

---

## Task 3: Merge Slot Lock

Add a per-world merge slot lock to `internal/dispatch/flock.go`, following
the existing `WorkItemLock` pattern exactly.

### Go Interface

```go
// MergeSlotLock holds an advisory flock on a world's merge slot.
type MergeSlotLock struct {
    file *os.File
    path string
}

// AcquireMergeSlotLock takes an exclusive advisory lock on the merge slot
// for the given world. Only one merge may be in progress per world at a time.
// Lock file: $SOL_HOME/.runtime/locks/{world}-merge-slot.lock.
// Uses LOCK_EX | LOCK_NB (non-blocking exclusive lock).
func AcquireMergeSlotLock(world string) (*MergeSlotLock, error)

// Release releases the merge slot lock and removes the lock file.
func (l *MergeSlotLock) Release() error
```

The implementation is nearly identical to `AcquireWorkItemLock` — same
lock directory, same flock pattern, different file name.

**Error messages:**
- Contention: `"merge slot busy for world %q"` (when EAGAIN)
- File errors: `"failed to acquire merge slot for world %s: %w"`

---

## Task 4: Extend dispatch.Done()

Modify `dispatch.Done()` in `internal/dispatch/dispatch.go` to create a
merge request after the git push. The outpost's work is done — it goes
idle — but the merge request enters the queue for the forge.

### Interface Changes

Add merge request creation to the `WorldStore` interface:

```go
// WorldStore defines the world store operations used by dispatch.
type WorldStore interface {
    GetWorkItem(id string) (*store.WorkItem, error)
    UpdateWorkItem(id string, updates store.WorkItemUpdates) error
    CreateMergeRequest(workItemID, branch string, priority int) (string, error) // NEW
    Close() error
}
```

### Modified Done Flow

The existing steps remain the same, but a new step is inserted between
the git push and the work item status update:

```
Done() {
    1. Read tether → get workItemID              (unchanged)
    2. Git add, commit, push                   (unchanged)
    3. Create merge request (phase=ready)      (NEW)
    4. Update work item: status → "done"       (unchanged)
    5. Update agent: state → "idle"            (unchanged)
    6. Clear tether file                         (unchanged)
    7. Stop session (background)               (unchanged)
}
```

Insert after the git push (after step 2, before existing step 3):

```go
// 3. Create merge request for the forge to process.
mrID, err := worldStore.CreateMergeRequest(workItemID, branchName, item.Priority)
if err != nil {
    return nil, fmt.Errorf("failed to create merge request for %q: %w", workItemID, err)
}
```

### Updated DoneResult

Add the merge request ID to the result:

```go
type DoneResult struct {
    WorkItemID     string
    Title          string
    AgentName      string
    BranchName     string
    MergeRequestID string  // NEW
}
```

### Updated CLI Output

In `cmd/done.go`, update the output to mention the merge request:

```
Done: sol-a1b2c3d4 (Implement login page)
  Branch: outpost/Toast/sol-a1b2c3d4
  Merge request: mr-e5f6a7b8 (queued)
  Agent Toast is now idle.
```

---

## Task 5: Tests

### Schema Migration Tests

Add to `internal/store/store_test.go` (or a new file):

```go
func TestMigrateRigV2(t *testing.T)
    // Open a fresh world store
    // Verify merge_requests table exists
    // Verify schema_version is 2

func TestMigrateRigV1ToV2(t *testing.T)
    // Open a world store (creates V1)
    // Close and reopen (should apply V2 migration)
    // Verify merge_requests table exists
    // Verify existing work_items are untouched
    // Verify schema_version is 2
```

### Merge Request CRUD Tests

Create `internal/store/merge_requests_test.go`:

```go
func TestCreateMergeRequest(t *testing.T)
    // Create a work item first (FK dependency)
    // Create a merge request
    // Verify: ID starts with "mr-", phase is "ready"
    // Get it back and verify all fields

func TestListMergeRequests(t *testing.T)
    // Create 3 MRs with different priorities
    // ListMergeRequests("") -> all 3, ordered by priority then age
    // ListMergeRequests("ready") -> all 3 (all are ready)
    // ListMergeRequests("claimed") -> empty

func TestClaimMergeRequest(t *testing.T)
    // Create 2 MRs: priority 1 and priority 3
    // Claim -> should get priority 1 first
    // Verify: phase=claimed, claimed_by set, attempts=1
    // Claim again -> should get priority 3
    // Claim again -> nil (no more ready MRs)

func TestClaimMergeRequestOrdering(t *testing.T)
    // Create 3 MRs with same priority
    // Claim -> should get oldest first (FIFO within priority)

func TestUpdateMergeRequestPhase(t *testing.T)
    // Create and claim a MR
    // Update to "merged" -> verify merged_at is set
    // Create another, claim, update to "failed" -> verify merged_at is nil
    // Create another, claim, update to "ready" -> verify claimed_by cleared

func TestReleaseStaleClaims(t *testing.T)
    // Create and claim a MR
    // ReleaseStaleClaims with 1-hour TTL -> 0 released (claim is fresh)
    // Manually set claimed_at to 31 minutes ago (direct SQL)
    // ReleaseStaleClaims with 30-minute TTL -> 1 released
    // Verify: MR is back to phase=ready, claimed_by cleared

func TestReleaseStaleLeavesRecentClaims(t *testing.T)
    // Create and claim two MRs
    // Set one claimed_at to 31 minutes ago, leave other fresh
    // ReleaseStaleClaims(30min) -> 1 released
    // Verify: only the stale one was released
```

### Merge Slot Lock Tests

Add to `internal/dispatch/flock_test.go`:

```go
func TestMergeSlotAcquireRelease(t *testing.T)
    // Set SOL_HOME to temp dir
    // Acquire merge slot for "testrig"
    // Verify lock file exists at .runtime/locks/testrig-merge-slot.lock
    // Release
    // Verify lock file removed

func TestMergeSlotDoubleAcquire(t *testing.T)
    // Acquire merge slot for "testrig"
    // Attempt second acquire for "testrig" -> error containing "busy"
    // Release first, acquire again -> succeeds

func TestMergeSlotDifferentWorlds(t *testing.T)
    // Acquire merge slot for "rig1"
    // Acquire merge slot for "rig2" -> succeeds (different worlds)
    // Release both
```

### Done Extension Tests

Add to `internal/dispatch/dispatch_test.go`:

```go
func TestDoneCreatesMergeRequest(t *testing.T)
    // Cast a work item to an agent (standard setup)
    // Call Done
    // Verify: MergeRequestID is set in DoneResult
    // Verify: merge request exists in store with phase=ready
    // Verify: merge request has correct branch and work_item_id
    // Verify: agent is idle, work item is done (existing behavior unchanged)
```

Update the existing mock `WorldStore` to include the new
`CreateMergeRequest` method. The mock can store MRs in a slice and
return a generated ID.

---

## Task 6: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   bin/sol store create --world=testrig --title="Test merge pipeline"
   bin/sol cast <id> testrig
   # In the agent's session, do some work, then:
   bin/sol done --world=testrig --agent=<name>
   # Should show: Merge request: mr-XXXXXXXX (queued)
   # Verify in SQLite:
   sqlite3 /tmp/sol-test/.store/testrig.db \
     "SELECT id, work_item_id, branch, phase FROM merge_requests"
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- The `merge_requests` table is in the **world** database (not sphere). Each
  world has its own merge queue.
- Merge request IDs use the `mr-` prefix to distinguish them from work
  item IDs (`sol-` prefix).
- `ClaimMergeRequest` is designed for a single forge per world. The
  atomic `UPDATE ... WHERE` prevents races even if multiple forges
  exist (future-proofing).
- The `attempts` field tracks how many times a MR has been claimed. This
  is used by the forge (prompt 02) to detect repeated failures.
- `Done()` now creates a merge request but still sets the work item to
  "done" and the agent to "idle". From the outpost's perspective, its
  work is done. The merge request tracks the merge lifecycle separately.
- Don't modify the sphere schema. The forge agent is registered in
  sphere.db using the existing `CreateAgent` method (handled in prompt 02).
- Keep the `WorldStore` interface additions minimal — only add what's
  needed for the Done extension. The forge in prompt 02 will call
  store methods directly (not through the dispatch interface).
- Commit after tests pass with message:
  `feat(store): add merge request schema, CRUD, and done extension for merge pipeline`
