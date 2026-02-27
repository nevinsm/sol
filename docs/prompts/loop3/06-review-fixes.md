# Loop 3 Review Fixes

Read this entire prompt before starting. Fix all 10 issues below, then run
`make test` to confirm nothing broke. Commit with message
`fix: address Loop 3 review findings`.

---

## 1. Add missing event type cases in feed formatter

**File:** `cmd/feed.go`, function `formatEventDescription` (~line 110)

The switch statement handles most event types but is missing cases for sentinel
events and chronicle batch events. They fall through to the default which dumps
raw JSON.

Add these cases before the `default`:

```go
case events.EventAssess:
    return fmt.Sprintf("Assessed %s: %s (%s confidence)", get("agent"), get("status"), get("confidence"))
case events.EventNudge:
    return fmt.Sprintf("Nudged %s: %s", get("agent"), get("message"))
case "sling_batch":
    return fmt.Sprintf("Cast burst: %s dispatches in %s", get("count"), get("world"))
case "respawn_batch":
    return fmt.Sprintf("Respawn burst: %s respawns in %s", get("count"), get("world"))
```

---

## 2. Check `SendProtocolMessage` errors in sentinel

**File:** `internal/sentinel/sentinel.go`

There are 4 calls to `w.sphereStore.SendProtocolMessage(...)` (lines ~411, ~423,
~494, ~549) that discard both return values. The sentinel should not block on
these errors (DEGRADE principle), but it should log them for observability.

For each of the 4 call sites, capture the error and emit an audit event if
it fails. Pattern:

```go
if _, err := w.sphereStore.SendProtocolMessage(
    w.agentID(), "operator",
    store.ProtoRecoveryNeeded,
    store.RecoveryNeededPayload{...},
); err != nil && w.logger != nil {
    w.logger.Emit("mail_error", w.agentID(), agent.ID, "audit",
        map[string]any{"error": err.Error()})
}
```

Do not change the control flow — the sentinel must continue regardless.

---

## 3. Check `os.Rename` error in chronicle `saveCheckpoint`

**File:** `internal/events/chronicle.go`, function `saveCheckpoint` (~line 486)

The line `os.Rename(tmpName, c.checkpointPath())` ignores the error. If the
rename fails, clean up the temp file and log to stderr:

```go
if err := os.Rename(tmpName, c.checkpointPath()); err != nil {
    os.Remove(tmpName)
    fmt.Fprintf(os.Stderr, "chronicle: failed to save checkpoint: %v\n", err)
}
```

---

## 4. Add `rows.Err()` checks after all `for rows.Next()` loops in store

This is a codebase-wide fix. After every `for rows.Next() { ... }` loop,
add a `rows.Err()` check. There are 5 locations:

**a) `internal/store/messages.go` ~line 204** — in `scanMessages()`:
After the loop, before the return, add:
```go
if err := rows.Err(); err != nil {
    return nil, fmt.Errorf("failed iterating messages: %w", err)
}
```

**b) `internal/store/agents.go` ~line 120** — in `ListAgents()`:
After the loop, before the return, add:
```go
if err := rows.Err(); err != nil {
    return nil, fmt.Errorf("failed iterating agents: %w", err)
}
```

**c) `internal/store/workitems.go` ~line 196** — in `GetWorkItem()` label loop:
After the loop, before the return, add:
```go
if err := rows.Err(); err != nil {
    return nil, fmt.Errorf("failed iterating labels for work item %q: %w", id, err)
}
```

**d) `internal/store/workitems.go` ~line 255** — in `ListWorkItems()`:
After the loop, before the return, add:
```go
if err := rows.Err(); err != nil {
    return nil, fmt.Errorf("failed iterating work items: %w", err)
}
```

**e) `internal/store/merge_requests.go` ~line 134** — in `ListMergeRequests()`:
After the loop, before the return, add:
```go
if err := rows.Err(); err != nil {
    return nil, fmt.Errorf("failed iterating merge requests: %w", err)
}
```

The exact line numbers may vary — find each `for rows.Next()` loop and add
the check between the closing brace and the return statement.

---

## 5. Validate priority range in `sol mail send`

**File:** `cmd/mail.go`, in the `mailSendCmd` RunE function (~line 27)

After reading the priority flag, add a range check:

```go
priority, _ := cmd.Flags().GetInt("priority")
if priority < 1 || priority > 3 {
    return fmt.Errorf("priority must be 1 (urgent), 2 (normal), or 3 (low)")
}
```

---

## 6. Emit audit event on AI assessment failure in sentinel

**File:** `internal/sentinel/sentinel.go`, in `assessAgent()` (~line 283)

Currently when the assessment call fails, the error is silently swallowed:

```go
if err != nil {
    // AI call failed — log and move on, don't block patrol.
    return nil
}
```

Add an audit event emission before the return:

```go
if err != nil {
    // AI call failed — log and move on, don't block patrol.
    if w.logger != nil {
        w.logger.Emit("assess_error", w.agentID(), agent.ID, "audit",
            map[string]any{"error": err.Error()})
    }
    return nil
}
```

---

## 7. Emit audit event on chronicle cycle error

**File:** `internal/events/chronicle.go`, in `Run()` (~line 96)

Currently cycle errors go to stderr only. Also emit a structured event:

```go
if err := c.processCycle(); err != nil {
    // Best-effort: log but continue.
    fmt.Fprintf(os.Stderr, "chronicle cycle error: %v\n", err)
    if c.logger != nil {
        c.logger.Emit("curator_error", "chronicle", "chronicle", "audit",
            map[string]any{"error": err.Error()})
    }
}
```

This requires the Chronicle to have access to the logger. If `c.logger` doesn't
exist on the Chronicle struct, check whether the Chronicle already has access to
a `*events.Logger` (it likely does via `c.config` or directly). If not, add a
`logger *events.Logger` field to the Chronicle struct and accept it in the
constructor. Wire it in from `cmd/chronicle.go` the same way `cmd/sentinel.go`
creates one with `events.NewLogger(config.Home())`.

---

## 8. Tighten dedup test assertion

**File:** `test/integration/loop3_test.go` (~line 306)

Change the vague assertion:

```go
if typeCounts["done"] < 1 {
    t.Errorf("expected at least 1 done event, got %d", typeCounts["done"])
}
```

To an exact count. The test emits 3 done events for Toast (duplicates) plus
1 each for Jasper and Sage (unique actors). Dedup keys on type+source+actor,
so Toast's 3 collapse to 1, giving 3 total:

```go
if typeCounts["done"] != 3 {
    t.Errorf("expected 3 done events (Toast deduped, Jasper+Sage unique), got %d", typeCounts["done"])
}
```

If the actual dedup logic produces a different count, match the assertion to
reality — but make it exact, not `< 1`.

---

## 9. Add `sync.Mutex` to integration test `mockSessionChecker`

**File:** `test/integration/helpers_test.go` (~line 263)

The `mockSessionChecker` struct has no mutex, unlike its unit test counterpart
in `internal/sentinel/witness_test.go`. Add a `sync.Mutex` and protect all
methods:

```go
type mockSessionChecker struct {
    mu       sync.Mutex
    alive    map[string]bool
    captures map[string]string
    started  []string
    stopped  []string
    injected []mockInjectCall
}
```

Then add `m.mu.Lock()` / `m.mu.Unlock()` (or `defer m.mu.Unlock()`) at the
top of `Exists`, `Capture`, `Start`, `Stop`, and `Inject`. Add the
`"sync"` import if not already present.

---

## 10. Check `RowsAffected` errors in messages.go

**File:** `internal/store/messages.go`, lines ~87 and ~128

Two places discard the error from `result.RowsAffected()`:

```go
n, _ := result.RowsAffected()
```

Change both to:

```go
n, err := result.RowsAffected()
if err != nil {
    return ..., fmt.Errorf("failed to check rows affected for message %q: %w", id, err)
}
```

In `ReadMessage` (line ~87) the return type is `(*Message, error)` so return
`nil, err`. In `AckMessage` (line ~128) the return type is `error` so return
`err` directly.

---

## Verification

After all changes:

```bash
make build && make test
```

All existing tests must pass. No new test files are needed — the changes are
defensive hardening, not new behavior.
