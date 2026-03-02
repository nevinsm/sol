# Prompt 03: Arc 1 Review-4 — Atomicity and Resilience Fixes

You are fixing crash-safety, atomicity, and resilience issues found during the fourth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 02 of arc1-review-4 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/tether/tether.go` — tether Write/Read/Clear
- `internal/handoff/handoff.go` — handoff Write, Exec method
- `internal/workflow/workflow.go` — Advance method, writeJSON helper
- `internal/dispatch/dispatch.go` — rollback, push failure, worktree error, fire-and-forget goroutine
- `internal/consul/heartbeat.go` — atomic write reference pattern (temp+rename)

---

## Task 1: Make tether Write atomic

**File:** `internal/tether/tether.go`, lines 32-41

The tether is described as "the durability primitive" in the project docs. Currently
`Write` uses bare `os.WriteFile`, which is not atomic on crash. The consul heartbeat
(`internal/consul/heartbeat.go:40-48`) already demonstrates the correct pattern.

**Fix:** Use write-to-temp-then-rename:

```go
func Write(world, agentName, workItemID string) error {
    path := TetherPath(world, agentName)
    dir := filepath.Dir(path)
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return fmt.Errorf("failed to create tether directory for agent %q in world %q: %w", agentName, world, err)
    }
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, []byte(workItemID), 0o644); err != nil {
        return fmt.Errorf("failed to write tether for agent %q in world %q: %w", agentName, world, err)
    }
    if err := os.Rename(tmp, path); err != nil {
        os.Remove(tmp) // best-effort cleanup
        return fmt.Errorf("failed to commit tether for agent %q in world %q: %w", agentName, world, err)
    }
    return nil
}
```

Also make `Read` defensive against trailing whitespace from manual edits:

```go
// In Read, after os.ReadFile succeeds:
return strings.TrimSpace(string(data)), nil
```

Add `"strings"` to imports if not already present.

Existing tests should continue to pass. No new tests needed.

---

## Task 2: Make handoff Write atomic

**File:** `internal/handoff/handoff.go`, lines 155-170

The handoff file is critical for session recovery. Same non-atomic `os.WriteFile` issue.

**Fix:** Use the same write-to-temp-then-rename pattern:

```go
func Write(world, agentName string, state *State) error {
    path := filePath(world, agentName)
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return fmt.Errorf("failed to create handoff directory: %w", err)
    }
    data, err := json.MarshalIndent(state, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal handoff state: %w", err)
    }
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, 0o644); err != nil {
        return fmt.Errorf("failed to write handoff file: %w", err)
    }
    if err := os.Rename(tmp, path); err != nil {
        os.Remove(tmp)
        return fmt.Errorf("failed to commit handoff file: %w", err)
    }
    return nil
}
```

Existing tests should continue to pass. No new tests needed.

---

## Task 3: Make workflow writeJSON atomic

**File:** `internal/workflow/workflow.go`, lines 515-521

The `writeJSON` helper writes state and step files that must survive crashes. Same issue.

**Fix:**

```go
func writeJSON(path string, v any) error {
    data, err := json.MarshalIndent(v, "", "  ")
    if err != nil {
        return err
    }
    tmp := path + ".tmp"
    if err := os.WriteFile(tmp, data, 0o644); err != nil {
        return fmt.Errorf("failed to write %s: %w", filepath.Base(path), err)
    }
    if err := os.Rename(tmp, path); err != nil {
        os.Remove(tmp)
        return fmt.Errorf("failed to commit %s: %w", filepath.Base(path), err)
    }
    return nil
}
```

Add `"path/filepath"` to imports if not already present.

---

## Task 4: Improve Advance atomicity with ordering

**File:** `internal/workflow/workflow.go`, `Advance` method (lines 419-503)

The `Advance` function writes multiple files in sequence. If it crashes partway through,
state becomes inconsistent. A full transaction is impractical (filesystem + JSON), but we
can improve crash safety by reordering writes so state.json (the source of truth) is always
written last, and by making the recovery path tolerant of the intermediate states.

The current order is:
1. Write current step as "complete" (step file)
2. Read instance / load manifest
3. Write next step as "executing" (step file)
4. Write state.json

If a crash occurs between 1 and 4, the step file says "complete" but state.json still
points to it as `CurrentStep`. On re-entry, `Advance` would try to mark it complete again.

**Fix:** Make the step-file writes idempotent. Before marking a step complete, check if
it is already complete:

At the top of `Advance`, after reading `currentStep` from the step file (around line 439-443),
add:

```go
// If the step is already complete (e.g., from a crash recovery), skip to finding the next step.
if currentStep.Status == "complete" {
    // Fall through to the next-step logic below.
} else {
    currentStep.Status = "complete"
    currentStep.CompletedAt = time.Now().UTC().Format(time.RFC3339)
    if err := writeJSON(stepPath, currentStep); err != nil {
        return nil, false, fmt.Errorf("failed to write step %q: %w", state.CurrentStep, err)
    }
}
```

This makes re-entry safe: if we crash after writing the step file but before writing state.json,
the next `Advance` call will see the step is already complete and skip the redundant write.

Also, move the state.json write to be the absolute last operation — after writing the next
step file. This is already the case in the current code, so just verify the ordering is:
1. Mark current step complete (idempotent)
2. Determine next step
3. Write next step file as "executing"
4. Update and write state.json (commit point)

---

## Task 5: Fix dispatch rollback error logging

**File:** `internal/dispatch/dispatch.go`, lines 183-189

The rollback function swallows all errors silently. If rollback itself fails, the system
is left in an inconsistent state with no diagnostic trail.

**Fix:** Log rollback errors to stderr (the rollback is best-effort, so don't fail the
parent operation, but make failures visible):

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
}
```

---

## Task 6: Fix wrong error variable in worktree fallback

**File:** `internal/dispatch/dispatch.go`, line 177

The worktree creation fallback wraps the wrong error:

```go
// Before (line 177):
return nil, fmt.Errorf("failed to create worktree: %s: %w", strings.TrimSpace(string(out2)), err)
// After:
return nil, fmt.Errorf("failed to create worktree: %s: %w", strings.TrimSpace(string(out2)), err2)
```

---

## Task 7: Prevent MR creation when push fails

**File:** `internal/dispatch/dispatch.go`, lines 560-568

Currently, a push failure is logged as a warning and the resolve continues to create a
merge request. The forge will then try to merge a branch that doesn't exist on the remote.

**Fix:** Make push failure fatal for the merge request path. The work item can still be
marked done (the work is complete), but don't create an MR that can never be processed:

```go
// git push origin HEAD
pushCmd := exec.Command("git", "-C", worktreeDir, "push", "origin", "HEAD")
pushFailed := false
if out, err := pushCmd.CombinedOutput(); err != nil {
    fmt.Fprintf(os.Stderr, "Warning: git push failed: %s\n", strings.TrimSpace(string(out)))
    pushFailed = true
}

// ... (existing work item update to "done" stays) ...

// 5. Create merge request (only if push succeeded).
if !pushFailed {
    _, err = worldStore.CreateMergeRequest(workItemID, branchName, opts.World)
    if err != nil {
        return nil, fmt.Errorf("failed to create merge request: %w", err)
    }
}
```

Apply the same pattern to `resolveConflictResolution` (around line 654) if it has a
similar push-then-MR sequence.

---

## Task 8: Log handoff Exec errors instead of swallowing

**File:** `internal/handoff/handoff.go`, lines 272 and 279

Two errors are silently discarded:

**Fix for SendMessage (line 272):**
```go
// Before:
sphereStore.SendMessage(agentID, agentID, subject, body, 2, "notification")
// After:
if _, err := sphereStore.SendMessage(agentID, agentID, subject, body, 2, "notification"); err != nil {
    fmt.Fprintf(os.Stderr, "handoff: failed to send self-notification: %v\n", err)
}
```

**Fix for Stop (line 279):**
```go
// Before:
sessionMgr.Stop(sessionName, false)
// After:
if err := sessionMgr.Stop(sessionName, false); err != nil {
    fmt.Fprintf(os.Stderr, "handoff: failed to stop session %s: %v\n", sessionName, err)
}
```

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Verify atomic writes work by inspecting that no `.tmp` files are left behind after tests:
  `find /tmp -name "*.tmp" -path "*/sol-test*" 2>/dev/null` should return nothing

## Commit

```
fix(tether,handoff,workflow,dispatch): arc 1 review-4 — atomic writes, error logging, push safety
```
