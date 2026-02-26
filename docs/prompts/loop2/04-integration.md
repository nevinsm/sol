# Prompt 04: Loop 2 — Integration Tests + Acceptance

You are writing the integration tests and performing final acceptance
verification for Loop 2 of the `gt` orchestration system. Loop 2 is
"merge pipeline" — completed work flows through a merge queue, a
refinery agent validates and merges it into the target branch with
quality gates.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompts 01, 02, and 03 are complete.

Read all existing code first. Understand the full Loop 2 pipeline:
merge request store (`internal/store/merge_requests.go`), the Done
extension (`internal/dispatch/dispatch.go` — `Done()` now creates MRs),
the refinery package (`internal/refinery/`), the CLI commands
(`cmd/refinery.go`), and the updated status package (`internal/status/`).
Also review the Loop 0 and Loop 1 integration tests
(`test/integration/loop0_test.go`, `test/integration/loop1_test.go`) and
CLI smoke tests (`test/integration/cli_test.go`) for patterns.

---

## Task 1: Fix Any Broken Tests

Run `make test`. If any tests fail, fix them before proceeding. The
previous prompts may have left inconsistencies (especially around the
updated `RigStore` interface in dispatch or the updated `Gather()`
signature in status). Get to green first.

---

## Task 2: Integration Test Helpers

Extend the test helpers in `test/integration/helpers_test.go` with
merge-pipeline-specific utilities.

```go
// createSourceRepo creates a bare git repo and a clone with an initial commit.
// Returns paths to the bare repo (origin) and the working clone.
func createSourceRepo(t *testing.T, gtHome string) (bareRepo, workingClone string) {
    // 1. git init --bare <gtHome>/.test-origin.git
    // 2. git clone <bare> <gtHome>/.test-clone
    // 3. In clone: create main.go, git add, git commit, git push origin main
    // Return (bare, clone)
}

// createBranchWithFile creates a new branch in the repo with a file change,
// pushes it to origin, and returns to the original branch.
func createBranchWithFile(t *testing.T, repoDir, branch, filename, content string) {
    // 1. git checkout -b <branch>
    // 2. Write filename with content
    // 3. git add + commit
    // 4. git push origin <branch>
    // 5. git checkout main (or the previous branch)
}

// waitForMergePhase polls the store until a MR reaches the expected phase.
func waitForMergePhase(t *testing.T, rigStore *store.Store, mrID, expectedPhase string,
    timeout time.Duration) {
    pollUntil(timeout, 500*time.Millisecond, func() bool {
        mr, err := rigStore.GetMergeRequest(mrID)
        return err == nil && mr != nil && mr.Phase == expectedPhase
    })
}
```

---

## Task 3: Integration Test Suite

Create `test/integration/loop2_test.go`. These tests exercise the full
Loop 2 pipeline end-to-end with real SQLite, real git, and real process
execution.

All integration tests should guard with:
```go
if testing.Short() {
    t.Skip("skipping integration test")
}
```

### Test 1: Full Merge Pipeline (Happy Path)

The complete flow: dispatch → done → MR queued → refinery merges.

```go
func TestMergePipelineHappyPath(t *testing.T)
```

1. Set up test environment:
   - Create temp `GT_HOME`
   - Create source repo with initial commit on `main`
   - Open rig and town stores

2. Dispatch work:
   - `store.CreateWorkItem("Add feature X", ...)`
   - `dispatch.Sling(...)` — dispatches to auto-provisioned agent

3. Simulate polecat completing work:
   - In the polecat's worktree, create a file (e.g., `feature.go`)
   - `git add` + `git commit` in the worktree
   - Call `dispatch.Done(...)` — should create a merge request

4. Verify MR created:
   - `store.ListMergeRequests("ready")` returns 1 MR
   - MR has correct `work_item_id` and `branch`

5. Start refinery (use package API, not CLI, for test control):
   ```go
   cfg := refinery.DefaultConfig()
   cfg.PollInterval = 1 * time.Second  // fast for testing
   cfg.QualityGates = []string{"true"} // always-pass gate
   ref := refinery.New(rig, sourceRepo, rigStore, townStore, cfg, logger)
   ctx, cancel := context.WithCancel(context.Background())
   defer cancel()
   go ref.Run(ctx)
   ```

6. Wait for merge:
   ```go
   waitForMergePhase(t, rigStore, mrID, "merged", 30*time.Second)
   ```

7. Verify post-merge state:
   - MR phase is `"merged"`, `merged_at` is set
   - Work item status is `"closed"`
   - The polecat's branch changes are on `main` in the source repo:
     ```bash
     git -C <sourceRepo> log --oneline main
     # Should show the merge commit
     git -C <sourceRepo> show main:feature.go
     # Should contain the polecat's changes
     ```

### Test 2: Quality Gate Failure and Retry

The refinery retries when quality gates fail, then succeeds.

```go
func TestMergePipelineQualityGateRetry(t *testing.T)
```

1. Set up: create source repo, work item, sling, done (creates MR)

2. Start refinery with a failing quality gate:
   ```go
   cfg.QualityGates = []string{"exit 1"}  // always fails
   cfg.MaxAttempts = 3
   ```

3. Wait for the MR to be claimed and retried:
   Poll until `mr.Attempts >= 3`

4. Verify: MR phase is `"failed"` (max attempts exceeded)

5. Stop the refinery, update the MR phase back to `"ready"` (manual
   reset), set attempts to 0

6. Restart refinery with a passing gate:
   ```go
   cfg.QualityGates = []string{"true"}
   ```

7. Wait for merge

8. Verify: MR is `"merged"`

### Test 3: Merge Conflict

Conflicting changes cause the MR to fail.

```go
func TestMergePipelineConflict(t *testing.T)
```

1. Set up: create source repo with a file `shared.go`

2. Dispatch work, polecat modifies `shared.go` in its worktree

3. Meanwhile, push a conflicting change to `main` directly:
   ```go
   // In the source repo (not the worktree)
   // Modify shared.go with different content
   // git add + commit + push
   ```

4. Call `dispatch.Done()` — creates MR

5. Start refinery with `cfg.QualityGates = []string{"true"}`

6. Wait for refinery to process

7. Verify: MR phase is `"failed"` (conflict detected)

### Test 4: Merge Slot Serialization

Only one merge at a time per rig.

```go
func TestMergeSlotSerialization(t *testing.T)
```

1. Acquire the merge slot lock manually:
   ```go
   lock, err := dispatch.AcquireMergeSlotLock(rig)
   ```

2. Start the refinery with a ready MR

3. Wait briefly (a few seconds) — the refinery should not be able
   to acquire the slot

4. Verify: MR is still `"ready"` (not processed — refinery released
   its claim because the slot was busy)

5. Release the manual lock

6. Wait for the refinery to process the MR on the next poll

7. Verify: MR is `"merged"`

### Test 5: Stale Claim TTL Recovery

A crashed refinery's claim is released after TTL expires.

```go
func TestStaleCaimTTLRecovery(t *testing.T)
```

1. Set up: create MR

2. Claim the MR manually via store (simulate a crashed refinery):
   ```go
   rigStore.ClaimMergeRequest("crashed-refinery")
   ```

3. Manually set `claimed_at` to 31 minutes ago (direct SQL update)

4. Start a new refinery with a short `ClaimTTL` (1 second for testing):
   ```go
   cfg.ClaimTTL = 1 * time.Second
   cfg.QualityGates = []string{"true"}
   ```

5. Wait for the MR to be processed

6. Verify: MR is `"merged"` — the stale claim was released and the
   new refinery picked it up

### Test 6: Multiple MRs Priority Ordering

Higher priority MRs are processed first.

```go
func TestMergeQueuePriorityOrdering(t *testing.T)
```

1. Create 3 work items with priorities 3, 1, 2

2. Sling and done each (creates 3 MRs)

3. Start refinery with `cfg.QualityGates = []string{"true"}`

4. Track the order in which MRs reach `"merged"` phase

5. Verify: priority 1 merged first, then 2, then 3

### Test 7: Status Shows Refinery and Queue

The status command reflects refinery and merge queue state.

```go
func TestStatusWithMergeQueue(t *testing.T)
```

1. Create work items, sling, done (creates MRs)

2. Gather status:
   ```go
   result, err := status.Gather(rig, townStore, rigStore, rigStore, mgr)
   ```

3. Verify:
   - `result.Refinery.Running == false` (not started yet)
   - `result.MergeQueue.Ready > 0`
   - `result.MergeQueue.Total > 0`

4. Start refinery in a tmux session

5. Gather status again:
   - `result.Refinery.Running == true`

---

## Task 4: CLI Smoke Tests

Add to `test/integration/cli_test.go` (or create
`test/integration/cli_loop2_test.go`):

```go
func TestCLIRefineryQueueEmpty(t *testing.T)
    // Create rig store
    // bin/gt refinery queue testrig
    // Verify: output contains "empty"

func TestCLIRefineryQueueWithMRs(t *testing.T)
    // Create work item, sling, done
    // bin/gt refinery queue testrig
    // Verify: output contains the MR ID and "ready"
    // bin/gt refinery queue testrig --json
    // Verify: valid JSON array with MR objects

func TestCLIDoneShowsMergeRequest(t *testing.T)
    // Create work item, sling
    // bin/gt done --rig=testrig --agent=<name>
    // Verify: output contains "Merge request:" and "mr-"

func TestCLIStatusShowsRefinery(t *testing.T)
    // bin/gt status testrig --json
    // Verify: JSON has "refinery" and "merge_queue" fields
```

Each test should use a unique `GT_HOME` temp directory.

---

## Task 5: Loop 2 Acceptance Checklist

Create `test/integration/LOOP2_ACCEPTANCE.md`:

```markdown
# Loop 2 Acceptance Criteria

## 1. Done creates merge request
- [ ] `gt done` creates a merge request with `phase=ready` in the rig store
- [ ] MR has correct `work_item_id` and `branch` fields
- [ ] MR ID starts with `mr-` prefix
- [ ] CLI output shows the merge request ID
- [ ] Work item status is "done", agent is "idle" (existing behavior preserved)

## 2. Refinery polls and claims
- [ ] Refinery polls `merge_requests` table for `phase=ready` items
- [ ] Claims are atomic (UPDATE ... WHERE prevents races)
- [ ] Higher priority MRs are processed first (lower number = higher priority)
- [ ] Oldest MRs processed first within same priority (FIFO)

## 3. Refinery rebases and tests
- [ ] Refinery rebases polecat's branch onto latest target branch
- [ ] Quality gates run in the refinery worktree
- [ ] Quality gates are configurable via `quality-gates.txt`
- [ ] Default quality gate is `go test ./...`

## 4. Merge on success
- [ ] Tests pass → refinery merges to target branch
- [ ] MR phase updated to "merged", `merged_at` timestamp set
- [ ] Work item status updated to "closed"
- [ ] Polecat's remote branch cleaned up (best-effort)

## 5. Retry on failure
- [ ] Quality gate failure → MR returned to "ready" for retry
- [ ] Max 3 attempts before MR marked "failed"
- [ ] Rebase conflict → MR immediately marked "failed"

## 6. Merge slot serialization
- [ ] Only one merge in progress at a time per rig (advisory file lock)
- [ ] Different rigs can merge concurrently

## 7. TTL recovery
- [ ] Stale claims (>30 min) automatically released to "ready"
- [ ] Released MRs are picked up by the next poll cycle

## 8. Refinery lifecycle
- [ ] `gt refinery run <rig>` runs the merge loop in foreground
- [ ] `gt refinery start <rig>` starts refinery in tmux session
- [ ] `gt refinery stop <rig>` stops the refinery session
- [ ] `gt refinery attach <rig>` attaches to the refinery session

## 9. Operator visibility
- [ ] `gt refinery queue <rig>` shows pending, claimed, merged, and failed MRs
- [ ] `gt refinery queue <rig> --json` outputs valid JSON
- [ ] `gt status <rig>` shows refinery running/stopped state
- [ ] `gt status <rig>` shows merge queue depth

## 10. Supervisor integration
- [ ] Supervisor restarts crashed refinery sessions
- [ ] Refinery respawned with `gt refinery run <rig>` (not claude)
- [ ] Polecat respawn behavior unchanged

## 11. All tests pass
- [ ] `make test` exits 0
- [ ] Loop 0 integration tests still pass
- [ ] Loop 1 integration tests still pass
- [ ] Loop 2 integration tests pass
- [ ] CLI smoke tests pass (new commands)
```

---

## Task 6: Verify Backward Compatibility

Ensure all Loop 0 and Loop 1 functionality still works:

1. Run Loop 0 integration tests:
   ```
   go test ./test/integration/ -run TestLoop0 -v -count=1
   ```

2. Run Loop 1 integration tests:
   ```
   go test ./test/integration/ -run TestLoop1 -v -count=1
   ```

3. Run all unit tests: `make test`

4. Verify that the updated `RigStore` interface (with
   `CreateMergeRequest`) doesn't break existing dispatch tests. All
   mocks should implement the new method.

5. Verify that the updated `Gather()` function (with
   `MergeQueueStore`) doesn't break existing status tests. All call
   sites should pass the new parameter.

---

## Task 7: Final Verification

1. `make test` — all pass
2. `make build` — succeeds
3. Run the full integration test suite:
   ```
   go test ./test/integration/ -v -count=1 -timeout=5m
   ```
4. Walk through the acceptance checklist manually for any items not
   covered by automated tests
5. Commit everything with a clear message:
   `test: add Loop 2 integration tests, CLI smoke tests, and acceptance checklist`

---

## Guidelines

- Integration tests are slow. Guard with `t.Short()`.
- Use the refinery package API directly (not the CLI) for fine test
  control. Set `PollInterval` to 1 second and use always-pass quality
  gates (`"true"`) to keep tests fast.
- For git-based tests, use the `createSourceRepo` helper to create
  isolated git repositories per test. Don't share repos between tests.
- Prefer polling with timeout over fixed sleeps. Use `pollUntil` and
  `waitForMergePhase` helpers.
- If you discover bugs in Loop 2 code while writing integration tests,
  fix them. That's the point of integration tests.
- Don't mock anything in integration tests — use real SQLite, real
  git, real processes. Mock-based tests belong in unit test files.
- Use generous timeouts (30 seconds) for merge operations — the
  refinery has poll intervals and quality gates can take time.
- Be careful with git operations in tests: each test needs its own
  source repo to avoid interference. Use `t.TempDir()` or the
  `gtHome` from `setupTestEnv` as the root.
- If a test is flaky due to timing, add a comment explaining why and
  increase the timeout rather than adding sleep.
