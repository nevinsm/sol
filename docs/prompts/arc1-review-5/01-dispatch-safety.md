# Prompt 01: Arc 1 Review-5 — Dispatch Safety

You are fixing concurrency and atomicity bugs in the dispatch layer found during the fifth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 review-4 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/dispatch/flock.go` — advisory locking (work item and merge slot)
- `internal/dispatch/dispatch.go` — Cast and Resolve functions
- `internal/store/agents.go` — UpdateAgentState, FindIdleAgent
- `internal/tether/tether.go` — tether read/write/clear

---

## Task 1: Add per-agent advisory lock to Cast

**File:** `internal/dispatch/flock.go`

Currently only work items have advisory locks. Two concurrent casts for *different* work items targeting the *same* agent can both pass the `agent.State != "idle"` check before either writes the state update. The time window is significant — it includes git worktree creation.

**Fix:** Add an `AgentLock` type mirroring `WorkItemLock`, using lock file `$SOL_HOME/.runtime/locks/agent-{agentID-with-slash-replaced}.lock`:

```go
// AgentLock holds an advisory flock on an agent.
type AgentLock struct {
	file *os.File
	path string
}

// AcquireAgentLock takes an exclusive advisory lock on the given agent.
// Lock file: $SOL_HOME/.runtime/locks/agent-{sanitizedID}.lock.
// Uses LOCK_EX | LOCK_NB (non-blocking exclusive lock).
func AcquireAgentLock(agentID string) (*AgentLock, error) {
	lockDir := filepath.Join(config.RuntimeDir(), "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to acquire lock for agent %s: %w", agentID, err)
	}

	// Replace "/" in agent IDs (e.g., "ember/Toast") with "--" for safe filenames.
	safe := strings.ReplaceAll(agentID, "/", "--")
	lockPath := filepath.Join(lockDir, "agent-"+safe+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock for agent %s: %w", agentID, err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("agent %s is being dispatched by another process", agentID)
		}
		return nil, fmt.Errorf("failed to acquire lock for agent %s: %w", agentID, err)
	}

	return &AgentLock{file: f, path: lockPath}, nil
}

// Release releases the agent lock.
func (l *AgentLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	os.Remove(l.path)
	l.file = nil
	return nil
}
```

Add `"strings"` to imports if not already present.

**File:** `internal/dispatch/dispatch.go`

In the `Cast` function, acquire the agent lock immediately after the agent is known (after the `FindIdleAgent` / `GetAgent` / `autoProvision` block, before the re-cast check). The lock must be held through the entire Cast operation and released in the `defer`.

Insert after the agent is resolved (after the `agent` variable is set and `agentID` is computed, before the re-cast detection):

```go
	// Acquire per-agent lock to prevent concurrent dispatch to same agent.
	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()
```

This lock must be acquired *after* the work-item lock (which is already held) to maintain a consistent lock ordering and avoid deadlocks.

---

## Task 2: Add locking to Resolve

**File:** `internal/dispatch/dispatch.go`

The `Resolve` function has no locking at all. Concurrent resolves (e.g., agent calls `sol resolve` while sentinel triggers a resolve) can create duplicate merge requests.

**Fix:** Add both work-item and agent locks at the top of Resolve, after reading the tether:

```go
func Resolve(opts ResolveOpts, worldStore WorldStore, sphereStore SphereStore, mgr SessionManager, logger *events.Logger) (*ResolveResult, error) {
	agentID := opts.World + "/" + opts.AgentName
	sessName := SessionName(opts.World, opts.AgentName)
	worktreeDir := WorktreePath(opts.World, opts.AgentName)

	// 1. Read tether — get work item ID.
	workItemID, err := tether.Read(opts.World, opts.AgentName)
	if err != nil {
		return nil, fmt.Errorf("failed to read tether: %w", err)
	}
	if workItemID == "" {
		return nil, fmt.Errorf("no work tethered for agent %q in world %q", opts.AgentName, opts.World)
	}

	// Acquire locks: work item first, then agent (consistent ordering with Cast).
	lock, err := AcquireWorkItemLock(workItemID)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	agentLock, err := AcquireAgentLock(agentID)
	if err != nil {
		return nil, err
	}
	defer agentLock.Release()

	// ... rest of Resolve continues from here (branchName := ...)
```

Move the `branchName` assignment and everything after it to follow the lock acquisition.

---

## Task 3: Add rollback to Resolve

**File:** `internal/dispatch/dispatch.go`

Unlike Cast, Resolve has zero rollback. If `UpdateWorkItem` to "done" succeeds but `CreateMergeRequest` fails, the item is "done" with no MR. If `UpdateAgentState` fails, the agent is stuck "working".

**Fix:** Add a rollback closure that restores state on failure. Define it after the git operations but before the state updates:

```go
	// Track what has been done so we can undo on failure.
	var workItemUpdated bool

	rollback := func() {
		if workItemUpdated {
			if err := worldStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{Status: "tethered"}); err != nil {
				fmt.Fprintf(os.Stderr, "resolve rollback: failed to reset work item %s: %v\n", workItemID, err)
			}
		}
	}
```

Then set `workItemUpdated = true` after the `UpdateWorkItem` call succeeds, and call `rollback()` before returning errors in subsequent steps (MR creation, agent state update, tether clear). If `UpdateAgentState` fails, also reset the work item:

```go
	// 3. Update work item: status -> done.
	if err := worldStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{Status: "done"}); err != nil {
		return nil, fmt.Errorf("failed to update work item status: %w", err)
	}
	workItemUpdated = true

	// 4. Create merge request (only if push succeeded).
	var mrID string
	if !pushFailed {
		mrID, err = worldStore.CreateMergeRequest(workItemID, branchName, item.Priority)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("failed to create merge request for %q: %w", workItemID, err)
		}
	}

	// 5. Update agent: state -> idle, tether_item -> clear.
	if err := sphereStore.UpdateAgentState(agentID, "idle", ""); err != nil {
		rollback()
		return nil, fmt.Errorf("failed to update agent state: %w", err)
	}

	// 6. Clear tether file.
	if err := tether.Clear(opts.World, opts.AgentName); err != nil {
		// Agent is idle but tether remains — consul will clean this up.
		fmt.Fprintf(os.Stderr, "resolve: failed to clear tether (consul will recover): %v\n", err)
	}
```

---

## Task 4: Record push failure durably

**File:** `internal/dispatch/dispatch.go`

When `git push origin HEAD` fails in Resolve, the work item is still marked "done" but no merge request is created. The work is effectively lost from the merge queue.

**Fix:** When push fails, still create the merge request but mark it with a distinguishable state. Add a `push_failed` label to the work item so operators can find these:

```go
	// git push origin HEAD
	pushCmd := exec.Command("git", "-C", worktreeDir, "push", "origin", "HEAD")
	pushFailed := false
	if out, err := pushCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git push failed: %s\n", strings.TrimSpace(string(out)))
		pushFailed = true
	}

	// 3. Update work item: status -> done.
	if err := worldStore.UpdateWorkItem(workItemID, store.WorkItemUpdates{Status: "done"}); err != nil {
		return nil, fmt.Errorf("failed to update work item status: %w", err)
	}
	workItemUpdated = true

	// 4. Create merge request — always, even if push failed (so it's tracked).
	mrID, err := worldStore.CreateMergeRequest(workItemID, branchName, item.Priority)
	if err != nil {
		rollback()
		return nil, fmt.Errorf("failed to create merge request for %q: %w", workItemID, err)
	}

	// If push failed, immediately mark the MR as failed so forge doesn't try to merge it.
	if pushFailed {
		if err := worldStore.UpdateMergeRequestPhase(mrID, "failed"); err != nil {
			fmt.Fprintf(os.Stderr, "resolve: failed to mark MR as failed after push failure: %v\n", err)
		}
	}
```

This ensures the merge request exists as a record even when push fails, and the "failed" phase signals to both operators and the forge that this MR needs attention.

---

## Task 5: Widen re-cast recovery conditions

**File:** `internal/dispatch/dispatch.go`

The re-cast detection at the top of Cast requires all four conditions: `item.Status == "tethered" && item.Assignee == agentID && agent.State == "working" && agent.TetherItem == opts.WorkItemID`. If a crash happens after updating the work item but before updating agent state, the agent has `State == "idle"` and the re-cast check fails. The normal Cast also fails because `item.Status != "open"`.

**Fix:** Widen the re-cast condition to also handle partial state. Replace the current re-cast detection:

```go
	// 4. Determine if this is a re-cast (crash recovery).
	// Full match: all four fields consistent (clean re-cast).
	// Partial match: work item is tethered to this agent but agent state is stale.
	// This handles crashes between work item update and agent state update.
	reCast := false
	if item.Status == "tethered" && item.Assignee == agentID {
		if agent.State == "working" && agent.TetherItem == opts.WorkItemID {
			reCast = true // clean re-cast
		} else if agent.State == "idle" && (agent.TetherItem == "" || agent.TetherItem == opts.WorkItemID) {
			reCast = true // partial failure recovery — agent wasn't updated
		}
	}
```

This allows recovery when the work item was tethered but the agent state update never happened.

---

## Task 6: Tests

**File:** `internal/dispatch/dispatch_test.go`

Add these tests:

```go
func TestResolveRollbackOnMRFailure(t *testing.T)
```
- Set up a cast (item tethered, agent working, tether file present)
- Create a mock WorldStore where `CreateMergeRequest` returns an error
- Call Resolve
- Verify: work item status is rolled back to "tethered" (not stuck at "done")

```go
func TestResolvePushFailureCreatesMR(t *testing.T)
```
- Set up a resolved state but with a worktree that has no remote (so push fails)
- Call Resolve
- Verify: MR is created with phase "failed"
- Verify: work item is "done", agent is "idle"

```go
func TestReCastPartialFailureRecovery(t *testing.T)
```
- Set up partial failure state: item status "tethered", item assignee "ember/Toast", agent state "idle", agent tether_item ""
- Call Cast with the same work item and agent
- Verify: Cast succeeds (re-cast path)
- Verify: agent state is now "working", session started

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Run new tests in isolation:
  `go test -v -run "TestResolveRollback|TestResolvePushFailure|TestReCastPartial" ./internal/dispatch/`

## Commit

```
fix(dispatch): arc 1 review-5 — agent locking, resolve rollback, push failure tracking
```
