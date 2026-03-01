# Prompt 03: Test & Build Infrastructure

You are fixing test infrastructure issues and build targets found during
the third Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompts 01 and 02 from arc1-review-3 are complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `Makefile`
- `test/integration/helpers_test.go` — test setup helpers, `pollUntil`
- `test/integration/loop1_test.go` — prefect integration tests
- `test/integration/loop3_test.go` — chronicle integration tests
- `internal/store/dependencies.go` — cycle detection BFS

---

## Task 1: Add `-race` to Makefile test target and add `test-short`

**File:** `Makefile`

The `test` target currently runs `go test ./...` without the race
detector. Add `-race` and add a new `test-short` target.

Change:

```makefile
test:
	go test ./...
```

To:

```makefile
test:
	go test -race ./...

test-short:
	go test -short -race ./...
```

Also add `test-short` to the `.PHONY` list at the top.

---

## Task 2: Replace fixed `time.Sleep` in prefect integration tests

**File:** `test/integration/loop1_test.go`

There are three `time.Sleep` calls that should use `pollUntil` instead:

### Line 259: `time.Sleep(500 * time.Millisecond)` — "Give prefect time to start"

This sleep waits for the prefect to run its initial heartbeat. Replace
with a poll that checks if the prefect has begun running. The simplest
signal is that the prefect's heartbeat count is > 0, but since that's
not exposed, poll for the prefect's first observable side effect: the
agent sessions exist and are being monitored. Alternatively, just reduce
the sleep to 200ms and add a comment explaining it's a startup delay —
this is prefect initialization, not an async result.

**Recommended fix:** Keep a short sleep but document why polling isn't
practical here:

```go
// Brief startup delay — prefect's first heartbeat runs on a ticker,
// not an observable event we can poll for.
time.Sleep(200 * time.Millisecond)
```

### Line 351: `time.Sleep(500 * time.Millisecond)` — same pattern

Same fix as above.

### Line 392: `time.Sleep(2 * time.Second)` — "Wait for death times to be fully pruned"

This waits for the prefect's `MassDeathWindow` to expire so death times
get pruned. The test sets `MassDeathWindow` to `1 * time.Second` (check
the test to confirm the exact value). The 2-second sleep is correct but
fragile.

**Replace with `pollUntil`:**

```go
// Wait for death times to be pruned (past MassDeathWindow).
pruned := pollUntil(5*time.Second, 200*time.Millisecond, func() bool {
    // After MassDeathWindow, the prefect prunes death times and can
    // dispatch again. Try dispatching — if it works, we're past the window.
    // Alternatively, just verify degraded mode has been off long enough.
    return !sup.IsDegraded()
})
if !pruned {
    t.Fatal("death times not pruned within 5 seconds")
}
```

Check the test context — the preceding `pollUntil` already waits for
`!sup.IsDegraded()`. If the 2-second sleep is specifically waiting for
the death times array to empty (not just for degraded mode to exit),
then it needs to wait for `MassDeathWindow` to pass *after* degraded
mode exits. In that case, keep a shorter sleep (`MassDeathWindow + 100ms`)
with a comment:

```go
// Wait for MassDeathWindow to fully expire so death times are pruned
// on the next heartbeat. The window is set to 1s in this test.
time.Sleep(1100 * time.Millisecond)
```

Read the test carefully and pick the right approach.

---

## Task 3: Replace 5ms sleeps in chronicle tests

**File:** `test/integration/loop3_test.go`

Lines 276 and 342 use `time.Sleep(5 * time.Millisecond)` with the
comment "Small delay so aggregation window passes." These are
extremely tight and may flake on slow CI.

The aggregation window check depends on chronicle's internal timing.
Look at what `ProcessOnce` actually checks — if it's comparing timestamps,
the 5ms might not be enough on a loaded machine.

**Fix:** Increase to a safer margin:

```go
// Ensure aggregation window has passed before processing.
time.Sleep(50 * time.Millisecond)
```

50ms is still fast but 10x more margin than 5ms. If the aggregation
window is configurable in the chronicle config, consider setting it
to something very small (1ms) for tests and keeping the sleep at 50ms.

---

## Task 4: Log tmux cleanup errors in test helpers

**File:** `test/integration/helpers_test.go` (lines 48-55)

The tmux cleanup closure ignores all errors:

```go
t.Cleanup(func() {
    out, _ := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
    for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
        if strings.HasPrefix(name, "sol-") {
            exec.Command("tmux", "kill-session", "-t", name).Run()
        }
    }
})
```

**Fix:** Log failures so orphaned sessions are visible in test output:

```go
t.Cleanup(func() {
    out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
    if err != nil {
        // tmux server might not be running — not an error.
        return
    }
    for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
        if strings.HasPrefix(name, "sol-") {
            if err := exec.Command("tmux", "kill-session", "-t", name).Run(); err != nil {
                t.Logf("cleanup: failed to kill session %q: %v", name, err)
            }
        }
    }
})
```

---

## Task 5: Document cycle detection scaling limitation

**File:** `internal/store/dependencies.go` — `wouldCreateCycle` (lines 131-153)

The BFS cycle detection does one `GetDependencies` query per node visited.
This is fine at current scale but will degrade with large dependency
graphs. Add a doc comment noting the known limitation:

```go
// wouldCreateCycle checks if adding the edge from→to would create a cycle
// by walking the dependency graph from toID to see if fromID is reachable.
//
// Implementation note: this does a BFS with one GetDependencies query per
// node. For large dependency graphs (100+ nodes), consider replacing with
// a recursive CTE:
//
//   WITH RECURSIVE chain(id) AS (
//       SELECT to_id FROM dependencies WHERE from_id = ?
//       UNION ALL
//       SELECT d.to_id FROM dependencies d JOIN chain c ON d.from_id = c.id
//   )
//   SELECT 1 FROM chain WHERE id = ? LIMIT 1
func (s *Store) wouldCreateCycle(fromID, toID string) (bool, error) {
```

---

## Verification

- `make build && make test` passes (now with `-race`)
- `make test-short` passes and skips integration tests
- Integration tests that touch tmux still pass
- No tmux sessions leaked after test runs:
  `tmux list-sessions 2>/dev/null | grep sol- || echo "clean"`

## Commit

```
fix(test): arc 1 review-3 — race detector, timing fixes, cleanup logging
```
