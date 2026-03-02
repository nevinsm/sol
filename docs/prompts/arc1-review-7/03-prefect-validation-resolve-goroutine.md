# Prompt 03: Arc 1 Review-7 — Prefect Validation, Resolve Goroutine, Chronicle Backoff

You are fixing a crash bug in prefect, improving the fire-and-forget session stop in resolve, and adding error backoff to chronicle.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 02 of this review is committed (or current main passes `make build && make test`).

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/prefect/prefect.go` — `Run()` method, config validation
- `internal/prefect/prefect_test.go` — existing tests
- `internal/dispatch/dispatch.go` — `Resolve()` function, session stop goroutine (around line 654)
- `internal/events/chronicle.go` — `Run()` loop (around line 105)

---

## Task 1: Validate HeartbeatInterval in prefect Run()

**File:** `internal/prefect/prefect.go`, `Run` method (around line 100)

`time.NewTicker()` panics if the duration is <= 0. A misconfigured or zero-value `HeartbeatInterval` will crash the prefect with no recovery.

**Fix:** Add validation at the top of `Run()`, before the ticker is created:

```go
func (s *Prefect) Run(ctx context.Context) error {
    if s.cfg.HeartbeatInterval <= 0 {
        return fmt.Errorf("invalid heartbeat interval: %v", s.cfg.HeartbeatInterval)
    }
    // ... rest of Run()
```

### Test

**File:** `internal/prefect/prefect_test.go`

Add a test:

```go
func TestRunRejectsZeroHeartbeatInterval(t *testing.T) {
```

Create a Prefect with `HeartbeatInterval: 0` and call `Run()`. Assert it returns an error containing `"invalid heartbeat interval"`.

Also test with a negative duration to be thorough.

---

## Task 2: Improve session stop in Resolve()

**File:** `internal/dispatch/dispatch.go`, `Resolve` function (around line 654-660)

The current code fires a goroutine that sleeps 1 second then stops the session. If the process exits before the goroutine completes, the session isn't stopped:

```go
go func() {
    time.Sleep(1 * time.Second)
    if err := mgr.Stop(sessName, true); err != nil {
        fmt.Fprintf(os.Stderr, "resolve: failed to stop session %s: %v\n", sessName, err)
    }
}()
```

Replace this with a synchronous stop that uses a short context timeout. The 1-second sleep was there to let the agent see the "resolve complete" message, but the session is being force-stopped anyway:

```go
// Stop session after a brief delay to allow final output.
// Use a goroutine with a done channel so callers can optionally wait.
done := make(chan struct{})
go func() {
    defer close(done)
    time.Sleep(1 * time.Second)
    if err := mgr.Stop(sessName, true); err != nil {
        fmt.Fprintf(os.Stderr, "resolve: failed to stop session %s: %v\n", sessName, err)
    }
}()
```

Find the second occurrence of the same pattern in Resolve (there's one around line 655 and another around line 737 — read the function to confirm). Apply the same fix to both.

**Important:** The `done` channel doesn't need to be awaited by Resolve itself (that would block the CLI), but making it a channel instead of a bare goroutine makes it testable and documentable. Add a comment explaining why it's not awaited:

```go
// Session stop runs in background — CLI returns immediately.
// The goroutine is best-effort; consul will recover stale sessions.
```

---

## Task 3: Add error backoff to chronicle Run() loop

**File:** `internal/events/chronicle.go`, `Run` method (around line 105-120)

If `processCycle()` hits a persistent error (disk full, permissions), the loop retries on every tick with no backoff, logging the same error on every cycle and wasting CPU.

**Fix:** Add consecutive error tracking with backoff. When errors occur consecutively, skip cycles:

```go
func (c *Chronicle) Run(ctx context.Context) error {
    // ... existing setup ...

    var consecutiveErrors int

    for {
        select {
        case <-ctx.Done():
            c.saveCheckpoint()
            return nil
        case <-ticker.C:
            // Skip cycles during error backoff.
            if consecutiveErrors > 0 {
                skip := 1 << min(consecutiveErrors-1, 5) // 1, 2, 4, 8, 16, 32 cycles
                consecutiveErrors++
                if consecutiveErrors%skip != 0 {
                    continue
                }
            }

            if err := c.processCycle(); err != nil {
                consecutiveErrors++
                fmt.Fprintf(os.Stderr, "chronicle cycle error: %v\n", err)
                if c.logger != nil {
                    c.logger.Emit("chronicle_error", "chronicle", "chronicle", "audit",
                        map[string]any{"error": err.Error()})
                }
            } else {
                consecutiveErrors = 0
            }
        }
    }
}
```

Wait — that approach is fragile with the modulo. Simpler approach using a skip counter:

```go
var errSkip int

for {
    select {
    case <-ctx.Done():
        c.saveCheckpoint()
        return nil
    case <-ticker.C:
        if errSkip > 0 {
            errSkip--
            continue
        }

        if err := c.processCycle(); err != nil {
            // Exponential backoff: skip 1, 2, 4, 8... up to 32 cycles.
            errSkip = min(errSkip*2+1, 32)
            fmt.Fprintf(os.Stderr, "chronicle cycle error (backoff %d cycles): %v\n", errSkip, err)
            if c.logger != nil {
                c.logger.Emit("chronicle_error", "chronicle", "chronicle", "audit",
                    map[string]any{"error": err.Error(), "backoff_cycles": errSkip})
            }
        } else {
            errSkip = 0
        }
    }
}
```

Wait, that also has an issue — `errSkip` is used to set itself. Let me be more precise.

Add a field or local variable to track consecutive failures:

```go
var errBackoff int // cycles to skip before retrying

for {
    select {
    case <-ctx.Done():
        c.saveCheckpoint()
        return nil
    case <-ticker.C:
        if errBackoff > 0 {
            errBackoff--
            continue
        }

        if err := c.processCycle(); err != nil {
            // Best-effort: log but continue with exponential backoff.
            // Double the backoff each consecutive failure, capped at 32 cycles.
            if errBackoff == 0 {
                errBackoff = 1
            } else {
                errBackoff = min(errBackoff*2, 32)
            }
            fmt.Fprintf(os.Stderr, "chronicle cycle error: %v\n", err)
            if c.logger != nil {
                c.logger.Emit("chronicle_error", "chronicle", "chronicle", "audit",
                    map[string]any{"error": err.Error()})
            }
        }
    }
}
```

Hmm, that still doesn't work because errBackoff is decremented to 0 before we process, then set on error. Let me just be explicit:

Add two local variables before the loop:

```go
var consecutiveErrs int
```

In the loop body, after the ticker fires:

```go
case <-ticker.C:
    // Exponential backoff on persistent errors: skip 2^(n-1) - 1 cycles, capped at 32.
    if consecutiveErrs > 0 {
        backoff := min(1<<(consecutiveErrs-1), 32)
        consecutiveErrs++
        if consecutiveErrs <= backoff {
            continue
        }
        consecutiveErrs = 1 // reset counter for next backoff window
    }

    if err := c.processCycle(); err != nil {
        consecutiveErrs++
        fmt.Fprintf(os.Stderr, "chronicle cycle error: %v\n", err)
        if c.logger != nil {
            c.logger.Emit("chronicle_error", "chronicle", "chronicle", "audit",
                map[string]any{"error": err.Error()})
        }
    } else {
        consecutiveErrs = 0
    }
```

Actually, this is getting over-complicated in prompt form. Use the simplest possible approach:

**Replace the existing error handling block** (the `if err := c.processCycle()` block inside the for loop) with:

```go
if err := c.processCycle(); err != nil {
    consecutiveErrs++
    // Log on first error, then every 32 cycles to avoid spam.
    if consecutiveErrs == 1 || consecutiveErrs%32 == 0 {
        fmt.Fprintf(os.Stderr, "chronicle cycle error (count=%d): %v\n", consecutiveErrs, err)
        if c.logger != nil {
            c.logger.Emit("chronicle_error", "chronicle", "chronicle", "audit",
                map[string]any{"error": err.Error(), "consecutive_count": consecutiveErrs})
        }
    }
} else {
    consecutiveErrs = 0
}
```

Declare `var consecutiveErrs int` before the for loop. This doesn't skip cycles (keeping DEGRADE semantics — always try to make progress), but rate-limits the log spam so operators see the first error and periodic updates rather than thousands of identical lines.

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- New test `TestRunRejectsZeroHeartbeatInterval` passes
- Manually inspect the chronicle error handling adds the consecutive counter and rate-limited logging
- Manually inspect the resolve session stop has explanatory comments

## Commit

```
fix(prefect,dispatch,events): arc 1 review-7 — heartbeat validation, resolve stop comments, chronicle error rate-limit
```
