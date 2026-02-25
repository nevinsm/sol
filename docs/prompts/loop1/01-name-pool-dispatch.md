# Prompt 01: Loop 1 — Name Pool + Dispatch Serialization

You are extending the `gt` orchestration system to support multi-agent
dispatch. This prompt adds two foundational capabilities: a themed name
pool for auto-provisioning agents, and per-work-item advisory locking to
serialize concurrent dispatches.

**Working directory:** `~/gt-src/`
**Prerequisite:** Loop 0 is complete (prompts 01–04).

Read all existing code first. Understand the store package (agents, work
items), dispatch package (sling/prime/done), session manager, hook, and
config. Pay special attention to `dispatch.Sling()` and
`store.FindIdleAgent()` — you'll be modifying both flows.

Read `docs/target-architecture.md` Sections 3.10 (Agent Identity) and 5
(Build Loops, Loop 1 requirements) for design context.

---

## Task 1: Name Pool Package

Create `internal/namepool/` — a package that manages a pool of themed
agent names. Names are immediately distinguishable at a glance (GLASS
principle: "Toast" vs "Jasper" is better than "agent-07" vs "agent-12").

### Embedded Default Names

Create `internal/namepool/names.txt` with at least 50 names, one per
line. These should be short, memorable, and easy to distinguish in tmux
session lists and logs. Example categories: food, animals, weather,
minerals, colors. No duplicates, no names containing spaces or special
characters.

```
# internal/namepool/names.txt
# Each line is one agent name. Lines starting with # are ignored.
Toast
Jasper
Sage
Copper
Flint
Ember
Ridge
Coral
Dusk
Birch
Maple
Onyx
Pearl
Slate
Cedar
Moss
Ivy
Raven
Pike
Opal
Cider
Cobalt
Garnet
Aspen
Haze
Clover
Basil
Rusty
Lark
Quartz
Sienna
Indigo
Nimbus
Sorrel
Thyme
Sable
Wren
Talon
Agate
Frost
Briar
Mica
Orchid
Drift
Poppy
Thorn
Crimson
Falcon
Obsidian
Thistle
Fern
Pewter
```

### Go Interface

```go
// internal/namepool/namepool.go
package namepool

import "embed"

//go:embed names.txt
var defaultNames string

// Pool manages a set of agent names.
type Pool struct {
    names []string
}

// Load returns a Pool. If overridePath is non-empty and the file exists,
// it reads names from that file instead of the embedded default. If the
// override file does not exist, it falls back to the embedded default
// (no error). Lines starting with "#" and blank lines are skipped.
func Load(overridePath string) (*Pool, error)

// Names returns a copy of the available name list.
func (p *Pool) Names() []string

// AllocateName returns the first name in the pool that is not already
// used by an agent in the given rig. usedNames is the set of names
// already taken (typically from store.ListAgents). Returns an error if
// all names are exhausted.
func (p *Pool) AllocateName(usedNames []string) (string, error)
```

The override file path should be `$GT_HOME/{rig}/names.txt`. The caller
resolves this and passes it in.

**Error messages:**
- Exhaustion: `"name pool exhausted: all %d names are in use"`
- Parse errors: `"failed to read name pool override %q: %w"`

### Implementation Notes

- `Load` parses the name source line-by-line, trimming whitespace,
  skipping blank lines and `#`-prefixed comment lines.
- `AllocateName` does a linear scan: for each name in pool order, check
  if it exists in usedNames. Return the first that doesn't. This is O(n²)
  but n ≤ 100 so it doesn't matter.
- The pool is immutable after construction — no mutex needed.

---

## Task 2: Dispatch Serialization (Flock)

Create `internal/dispatch/flock.go` — per-work-item advisory file locking
to prevent two concurrent `gt sling` invocations from dispatching the
same work item to two different agents.

### Go Interface

```go
// internal/dispatch/flock.go
package dispatch

import (
    "fmt"
    "os"
    "path/filepath"
    "syscall"

    "github.com/nevinsm/gt/internal/config"
)

// WorkItemLock holds an advisory flock on a work item.
type WorkItemLock struct {
    file *os.File
    path string
}

// AcquireWorkItemLock takes an exclusive advisory lock on the given work
// item ID. The lock file is created at $GT_HOME/.runtime/locks/{itemID}.lock.
// Returns an error if the lock cannot be acquired (EAGAIN = already held).
// Uses LOCK_EX | LOCK_NB (non-blocking exclusive lock).
func AcquireWorkItemLock(itemID string) (*WorkItemLock, error)

// Release releases the advisory lock and removes the lock file.
func (l *WorkItemLock) Release() error
```

### Lock File Location

Lock files live at `$GT_HOME/.runtime/locks/{itemID}.lock`. The `locks/`
directory must be created if it doesn't exist (use `os.MkdirAll`).

### Error Messages

- Lock contention: `"work item %s is being dispatched by another process"`
- File errors: `"failed to acquire lock for work item %s: %w"`

### Integration with Sling

Modify `dispatch.Sling()` to acquire a work item lock at the very start
of the function, before any store reads. Release the lock in a `defer`
after the function returns. The flow becomes:

```
Sling() {
    lock := AcquireWorkItemLock(opts.WorkItemID)  // NEW
    defer lock.Release()                           // NEW
    ... existing sling logic ...
}
```

If `AcquireWorkItemLock` returns an error (another sling is in progress
for this item), `Sling` returns that error immediately.

---

## Task 3: Auto-Provisioning in Sling

Modify `dispatch.Sling()` so that when no `AgentName` is specified and no
idle agent exists, it auto-provisions a new agent from the name pool
instead of returning an error.

### Current Behavior

When `opts.AgentName` is empty, `Sling` calls `townStore.FindIdleAgent(rig)`.
If nil, it returns an error like `"no idle agent available for rig %q"`.

### New Behavior

When `opts.AgentName` is empty and `FindIdleAgent` returns nil:

1. Load the name pool: `namepool.Load(overridePath)` where overridePath
   is `config.Home()/{rig}/names.txt`
2. List all agents for the rig: `townStore.ListAgents(rig, "")`
3. Extract used names from the agent list
4. Call `pool.AllocateName(usedNames)` to get the next available name
5. Create the agent: `townStore.CreateAgent(name, rig, "polecat")`
6. Use the newly created agent for the dispatch

### Interface Changes

The `TownStore` interface in `dispatch.go` needs two methods added.
These methods already exist on `*store.Store` — you're only adding them
to the interface so dispatch can use them:

```go
type TownStore interface {
    GetAgent(id string) (*store.Agent, error)
    FindIdleAgent(rig string) (*store.Agent, error)
    UpdateAgentState(id, state, hookItem string) error
    ListAgents(rig string, state string) ([]store.Agent, error)  // ADD to interface
    CreateAgent(name, rig, role string) (string, error)          // ADD to interface
    Close() error
}
```

Update any mocks in existing tests to satisfy the expanded interface.

### Sling Output

When auto-provisioning occurs, include it in the output. The existing
`SlingResult` struct already has `AgentName` — no struct changes needed.
The CLI command (`cmd/sling.go`) should print the same output format
regardless of whether the agent was pre-existing or auto-provisioned.

---

## Task 4: Tests

### Name Pool Tests

Create `internal/namepool/namepool_test.go`:

```go
func TestLoadDefault(t *testing.T)
    // Load with empty overridePath
    // Verify at least 50 names returned
    // Verify no duplicates
    // Verify no blank or comment-only entries

func TestLoadOverride(t *testing.T)
    // Write a custom names file to a temp dir
    // Load with that path
    // Verify only the override names are returned

func TestLoadOverrideFallback(t *testing.T)
    // Load with a non-existent override path
    // Verify it falls back to default (no error)

func TestAllocateName(t *testing.T)
    // Load default pool
    // AllocateName with empty usedNames -> returns first name
    // AllocateName with first name used -> returns second name
    // AllocateName with first N names used -> returns N+1th

func TestAllocateNameExhaustion(t *testing.T)
    // Load a pool with 3 names (override file)
    // Mark all 3 as used
    // AllocateName -> error containing "exhausted"

func TestLoadCommentsAndBlanks(t *testing.T)
    // Override file with comments and blank lines
    // Verify they are skipped correctly
```

### Flock Tests

Create `internal/dispatch/flock_test.go`:

```go
func TestAcquireRelease(t *testing.T)
    // Set GT_HOME to temp dir
    // Acquire lock for "gt-aabbccdd"
    // Verify lock file exists
    // Release
    // Verify lock file removed

func TestDoubleAcquire(t *testing.T)
    // Acquire lock for same item ID twice
    // Second acquire should fail with contention error

func TestDifferentItems(t *testing.T)
    // Acquire locks for two different item IDs
    // Both should succeed
    // Release both

func TestReleaseIdempotent(t *testing.T)
    // Acquire, release, release again
    // No error on second release
```

### Auto-Provisioning Tests

Add to `internal/dispatch/dispatch_test.go`:

```go
func TestSlingAutoProvision(t *testing.T)
    // Create a work item but NO agent
    // Sling with empty AgentName
    // Verify: agent auto-created, work dispatched, name from pool

func TestSlingAutoProvisionSkipsUsed(t *testing.T)
    // Create agents with the first 3 pool names
    // Set them all to "working" state
    // Create a work item, sling with empty AgentName
    // Verify: new agent created with the 4th pool name

func TestSlingFlockPreventsDoubleDispatch(t *testing.T)
    // This is hard to test without goroutines.
    // Acquire a work item lock manually, then try to sling the same item.
    // Verify: sling returns contention error.
```

Update the existing mock `mockSessionManager` and add a mock or extend
the `TownStore` mock to include the new `ListAgents` and `CreateAgent`
methods.

---

## Task 5: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual test of auto-provisioning:
   ```bash
   export GT_HOME=/tmp/gt-test
   bin/gt store create --db=testrig --title="First task"
   bin/gt sling <id> testrig    # no --agent flag
   # Should auto-provision an agent named "Toast" (first in pool)
   bin/gt agent list --rig=testrig
   # Should show Toast in "working" state
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Store Change: ListAgents All-Rigs Support

The current `store.ListAgents(rig, state)` always filters by rig
(`WHERE rig = ?`). Modify it so that when `rig` is empty, it omits
the rig filter and returns agents across all rigs. This is needed by
the supervisor in prompt 02, but should be done here since you're
already extending the `TownStore` interface. Build the query
conditionally:

```go
query := `SELECT ... FROM agents WHERE 1=1`
if rig != "" {
    query += ` AND rig = ?`
    args = append(args, rig)
}
if state != "" {
    query += ` AND state = ?`
    args = append(args, state)
}
```

---

## Guidelines

- The name pool is intentionally simple. No persistence of allocation
  state — just check which names are already in the agents table.
- Flock is per-process, not per-goroutine. Two goroutines in the same
  process sharing a file descriptor would share the lock. This is fine
  because `gt sling` is a CLI command (one invocation = one process).
- The flock serializes per-work-item. Two different work items being
  slung concurrently will run `git worktree add` in parallel against the
  same source repo. This is safe as long as the branch names differ
  (they do — `polecat/{agentName}/{itemID}`). If you hit issues, add a
  repo-level flock around the `git worktree add` call.
- Don't modify the store schema. Agent creation via `store.CreateAgent`
  is sufficient for the name pool flow.
- Keep the `TownStore` interface additions minimal. Only add what's
  needed for auto-provisioning.
- Commit after tests pass with message:
  `feat(dispatch): add name pool and flock serialization for multi-agent dispatch`
