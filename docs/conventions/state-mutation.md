# State Mutation Conventions (CC-7, CC-8)

Conventions for multi-step state mutations — sequences that touch more than
one resource (database row, filesystem path, tmux session, tether file, …).
The goal is that a crash or error at any step leaves the system in a known,
recoverable state rather than an inconsistent half-committed one.

## 1. Multi-step mutations must be transactional OR have an explicit rollback

A mutation that modifies two or more independent resources without a shared
transaction must capture pre-state before the first write and restore it on
failure.

**Wrong — no rollback:**
```go
if err := sphereStore.UpdateAgentState(id, "working", writ); err != nil {
    return err
}
// If the tmux call below fails, the agent is stuck "working" forever.
if err := tmux.NewSession(sessionName, command, env); err != nil {
    return err
}
```

**Right — capture pre-state and restore on failure:**
```go
prevState := existing.State
activeWrit := existing.ActiveWrit

if err := sphereStore.UpdateAgentState(id, "working", writ); err != nil {
    return err
}
defer func() {
    if retErr != nil {
        if rbErr := sphereStore.UpdateAgentState(id, prevState, activeWrit); rbErr != nil {
            slog.Warn("failed to rollback agent state", "agent", id, "error", rbErr)
        }
    }
}()

if err := tmux.NewSession(sessionName, command, env); err != nil {
    return err   // defer fires, restoring prevState
}
```

**Reference implementation:** `internal/startup/startup.go`, `Launch` function
(rollback at ~lines 296-338). This is the canonical capture-and-restore pattern
in the codebase.

## 2. Context-aware release for long-running operations

In long-running goroutines, distinguish between "the operation failed" and
"the context was cancelled before we could tell". When a context cancellation
races with a state write, prefer **deferring** the state update so the next
startup can re-verify rather than marking a potentially-successful operation
as failed.

**Reference implementation:** `internal/forge/orchestrator.go`,
`DeferMergeRequestVerification` call in the push-verification goroutine.
When `ctx.Err() != nil`, the code releases the merge request back to "ready"
(decrementing attempt count so the next patrol can reclaim it) instead of
calling `MarkFailed`.

## 3. New multi-step operations require a failure-during-step-2 test

Any new sequence that follows this pattern must include an integration or unit
test that:

1. Completes step 1 (write DB row / write file / start process).
2. Injects a failure before step 2 completes.
3. Asserts that the rollback restored the pre-mutation state.

This prevents regressions where a future refactor removes the rollback defer.

## 4. Prefer SQLite transactions for pure-DB multi-step mutations

If all mutated state lives in the same SQLite database, use a single
`BEGIN … COMMIT` transaction instead of the capture-and-restore pattern.
The capture-and-restore pattern is for **cross-domain** mutations (DB + FS,
DB + tmux, etc.) where a single transaction cannot span both resources.

## 5. Rollback failures must be logged, never ignored

If the rollback itself fails, log the error with `slog.Warn` so the operator
has a signal. Do not swallow rollback errors — the system is already in a
degraded state, and a silent second failure makes diagnosis impossible.

```go
if rbErr := sphereStore.UpdateAgentState(id, prevState, activeWrit); rbErr != nil {
    slog.Warn("startup: failed to rollback agent state after launch error",
        "agent", id, "error", rbErr)
}
```
