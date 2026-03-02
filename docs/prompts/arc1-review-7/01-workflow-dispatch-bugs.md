# Prompt 01: Arc 1 Review-7 — Workflow State Corruption and Dispatch Rollback

You are fixing a state corruption bug in workflow Advance(), a wrong-error-variable bug in handoff Exec(), and an incomplete rollback in dispatch Cast().

**Working directory:** `~/gt-src/`
**Prerequisite:** `make build && make test` passes on current main.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/workflow/workflow.go` — `Advance()` function (around line 419)
- `internal/workflow/workflow_test.go` — existing tests
- `internal/handoff/handoff.go` — `Exec()` function (around line 270)
- `internal/handoff/handoff_test.go` — existing tests
- `internal/dispatch/dispatch.go` — `Cast()` function (around line 130), `rollback` closure (around line 200), `Resolve()` function (around line 560)

---

## Task 1: Fix duplicate Completed entries in Advance()

**File:** `internal/workflow/workflow.go`, `Advance` function

The crash-recovery path in `Advance()` has a bug. When `currentStep.Status` is already `"complete"` (line ~445), the code correctly skips marking it complete again. But line ~456 **unconditionally** appends `state.CurrentStep` to `state.Completed`:

```go
if currentStep.Status == "complete" {
    // Fall through to the next-step logic below.
} else {
    currentStep.Status = "complete"
    currentStep.CompletedAt = &now
    if err := writeJSON(stepPath, currentStep); err != nil {
        return nil, false, fmt.Errorf("failed to write step %q: %w", state.CurrentStep, err)
    }
}

// Update completed list.
state.Completed = append(state.Completed, state.CurrentStep) // BUG: always appends
```

If the step was already complete (and therefore already in `state.Completed`), this creates a duplicate entry, corrupting the workflow state.

**Fix:** Only append when the step was freshly completed:

```go
if currentStep.Status == "complete" {
    // Fall through to the next-step logic below.
} else {
    currentStep.Status = "complete"
    currentStep.CompletedAt = &now
    if err := writeJSON(stepPath, currentStep); err != nil {
        return nil, false, fmt.Errorf("failed to write step %q: %w", state.CurrentStep, err)
    }
    // Only append to Completed list when freshly completing.
    state.Completed = append(state.Completed, state.CurrentStep)
}
```

Move the `state.Completed = append(...)` line **inside** the else block, after `writeJSON` succeeds.

---

## Task 2: Fix wrong error variable in handoff Exec() retry

**File:** `internal/handoff/handoff.go`, `Exec` function (around line 312-318)

When the retry `Start()` also fails, the error return wraps the *original* error `err` instead of `restartErr`:

```go
if restartErr := sessionMgr.Start(...); restartErr != nil {
    fmt.Fprintf(os.Stderr, "handoff: recovery also failed: %v\n", restartErr)
    return fmt.Errorf("failed to start new session (recovery also failed): %w", err) // BUG: should be restartErr
}
```

**Fix:** Change `err` to `restartErr` in the Errorf call:

```go
    return fmt.Errorf("failed to start new session (recovery also failed): %w", restartErr)
```

---

## Task 3: Add workflow cleanup to Cast() rollback

**File:** `internal/dispatch/dispatch.go`, `Cast` function

The `rollback` closure (around line 200) handles tether, work item, agent state, and worktree cleanup. But if `workflow.Instantiate()` succeeds (around line 268) and a later step fails, the workflow directory is **not** cleaned up.

**Fix:** Add workflow cleanup to the rollback function. After the existing worktree removal:

```go
rollback := func() {
    if err := tether.Clear(opts.World, agent.Name); err != nil {
        fmt.Fprintf(os.Stderr, "rollback: failed to clear tether: %v\n", err)
    }
    if err := worldStore.UpdateWorkItem(opts.WorkItemID, store.WorkItemUpdates{Status: "open", Assignee: "-"}); err != nil {
        fmt.Fprintf(os.Stderr, "rollback: failed to reset work item: %v\n", err)
    }
    if err := sphereStore.UpdateAgentState(agent.ID, "idle", ""); err != nil {
        fmt.Fprintf(os.Stderr, "rollback: failed to reset agent state: %v\n", err)
    }
    rmCmd := exec.Command("git", "-C", opts.SourceRepo, "worktree", "remove", "--force", worktreeDir)
    if out, err := rmCmd.CombinedOutput(); err != nil {
        fmt.Fprintf(os.Stderr, "rollback: failed to remove worktree: %s\n", strings.TrimSpace(string(out)))
    }
    // Clean up workflow if it was instantiated.
    workflow.Remove(opts.World, agent.Name) // best-effort
}
```

The `workflow.Remove` call is best-effort (no-op if no workflow was instantiated). Import `workflow` if not already imported in this file — check first.

---

## Task 4: Log workflow.Remove error in Resolve()

**File:** `internal/dispatch/dispatch.go`, `Resolve` function (around line 650-651)

Currently the workflow cleanup in Resolve silently discards the error:

```go
if _, err := workflow.ReadState(opts.World, opts.AgentName); err == nil {
    workflow.Remove(opts.World, opts.AgentName) // best-effort cleanup
}
```

**Fix:** Log the error to stderr:

```go
if _, err := workflow.ReadState(opts.World, opts.AgentName); err == nil {
    if removeErr := workflow.Remove(opts.World, opts.AgentName); removeErr != nil {
        fmt.Fprintf(os.Stderr, "resolve: failed to clean up workflow: %v\n", removeErr)
    }
}
```

---

## Task 5: Log handoff.Remove error in Prime()

**File:** `internal/dispatch/dispatch.go`, `Prime` function (around line 397)

```go
handoff.Remove(world, agentName) // error ignored
```

**Fix:** Log the error:

```go
if removeErr := handoff.Remove(world, agentName); removeErr != nil {
    fmt.Fprintf(os.Stderr, "prime: failed to remove handoff file: %v\n", removeErr)
}
```

---

## Task 6: Tests

### Test for Advance() duplicate fix

**File:** `internal/workflow/workflow_test.go`

Add a test that exercises the crash-recovery path — calling Advance() when the current step is already complete:

```go
func TestAdvanceIdempotentOnCompletedStep(t *testing.T) {
```

The test should:
1. Set up a formula with at least 2 steps
2. Instantiate the workflow
3. Call `Advance()` to complete step 1 (should succeed, return next step)
4. Read state, verify step 1 appears exactly once in `Completed`
5. Manually rewrite `state.json` to set `CurrentStep` back to step 1 (simulating crash recovery where step file is complete but state wasn't fully committed)
6. Call `Advance()` again
7. Read state, verify step 1 still appears exactly **once** in `Completed` (not duplicated)

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- New test `TestAdvanceIdempotentOnCompletedStep` passes
- Manually inspect that the rollback function in Cast now includes `workflow.Remove`
- Manually inspect that handoff `Exec()` line ~318 wraps `restartErr` not `err`

## Commit

```
fix(workflow,dispatch,handoff): arc 1 review-7 — advance idempotency, rollback completeness, error variables
```
