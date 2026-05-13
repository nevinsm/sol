# Error Handling Conventions (CC-6)

Conventions for how sol code handles, surfaces, and categorises errors.
The goal is uniformity so operators can always find failures and so reviewers
can enforce the same patterns across packages.

## 1. Never silently swallow errors

Every non-nil error must be either returned, logged, or emitted — never
discarded without a trace.

**Wrong:**
```go
data, _ := os.ReadFile(path)  // silent discard — operator has no signal
```

**Right — return it:**
```go
data, err := os.ReadFile(path)
if err != nil {
    return fmt.Errorf("failed to read %s: %w", path, err)
}
```

**Right — log and continue (soft failure):**
```go
data, err := os.ReadFile(path)
if softfail.Log(logger, "mycomponent.read_config", err) {
    data = defaultConfig
}
```

The only accepted silent discard is for registration helpers at init time —
see `cmd/CONVENTIONS.md §3` and the `MarkFlagRequired` pattern. Reserve
`_ = someCall()` exclusively for that case.

## 2. ENOENT vs. corruption: distinguish them

`os.IsNotExist` / `errors.Is(err, fs.ErrNotExist)` means the file was never
written — the empty/default state is the correct response.

A parse error, short read, or checksum mismatch on an existing file means
**corruption** — returning the default silently masks data loss.

```go
data, err := os.ReadFile(path)
if err != nil {
    if errors.Is(err, fs.ErrNotExist) {
        return defaultValue, nil   // file was never written — expected
    }
    return zero, fmt.Errorf("failed to read %s: %w", path, err)
}

cfg, err := parseConfig(data)
if err != nil {
    // NOT the same as ENOENT — this is corruption, surface it
    return zero, fmt.Errorf("corrupt config at %s: %w", path, err)
}
```

## 3. `store.ErrNotFound` vs. transient DB errors

`errors.Is(err, store.ErrNotFound)` means the record was never inserted.
Any other error from the store layer is a transient or structural failure
(locked database, I/O error, schema mismatch) and must not be treated as
"record gone".

```go
agent, err := sphereStore.GetAgent(id)
if err != nil {
    if errors.Is(err, store.ErrNotFound) {
        // agent was never registered — take the "new agent" path
        return createNewAgent(...)
    }
    // Not ErrNotFound — could be lock contention, I/O error, etc.
    return fmt.Errorf("failed to look up agent %q: %w", id, err)
}
```

See also: `internal/startup/startup.go` (Launch) for the canonical example
of this pattern with sphere store agent lookups.

## 4. Best-effort sites: use `internal/softfail` consistently

When a failure is intentionally non-fatal (optional file, background scan,
cleanup step), use `internal/softfail` so the error is observable without
aborting the caller.

- **`softfail.Log`** — intra-package sites where `slog` is the only consumer.
- **`softfail.Emit`** — cross-package boundaries; emits a structured
  `soft_failure` event to the event feed so chronicle, `sol feed`, and
  audit can observe it.

```go
// Intra-package: only slog consumers care
if softfail.Log(logger, "cleanup.remove_stale_worktree", err) {
    // continue without the worktree
}

// Cross-package boundary: emit so feed/audit can observe
if softfail.Emit(logger, evLogger, "dispatch.rollback_agent_state", err, nil) {
    // fallback logic
}
```

Do not write ad-hoc `logger.Warn("ignoring error", ...)` without using
`softfail` — the helper adds structured fields and the event emission path.

**See also:** `internal/softfail/softfail.go` (package doc and `Emit` godoc).

## 5. Error message format

Wrap errors with context at every boundary using `%w`:

```go
return fmt.Errorf("failed to open world database %q: %w", path, err)
```

The pattern is: `"failed to <verb> <noun>: %w"`. Include identifiers (agent
name, world, path) so the operator can correlate the log line with the
failing resource without grepping further.
