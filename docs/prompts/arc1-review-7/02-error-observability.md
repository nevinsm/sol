# Prompt 02: Arc 1 Review-7 — Error Observability in Best-Effort Paths

You are fixing silent error swallowing in event logging, consul heartbeat, event reader, and store open/close. These are all "best-effort" code paths where errors are currently discarded, making failures invisible to operators.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 01 of this review is committed (or current main passes `make build && make test`).

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/events/events.go` — `Log()` method (around line 70)
- `internal/events/reader.go` — `Follow()` method (around line 85)
- `internal/consul/consul.go` — `patrol()` method, `WriteHeartbeat` call (around line 210)
- `internal/store/store.go` — `OpenWorld()`, `OpenSphere()`

---

## Task 1: Check f.Write() return in event Logger.Log()

**File:** `internal/events/events.go`, `Log` method (around line 94)

The event write silently discards the error:

```go
f.Write(append(data, '\n'))
```

While this is a best-effort path (the function returns no error), the write failure should be observable. Add stderr logging:

```go
if _, err := f.Write(append(data, '\n')); err != nil {
    fmt.Fprintf(os.Stderr, "events: failed to write event: %v\n", err)
}
```

---

## Task 2: Log consul heartbeat write errors

**File:** `internal/consul/consul.go`, `patrol` method (around line 210)

Currently:
```go
_ = WriteHeartbeat(d.config.SolHome, &Heartbeat{...})
```

**Fix:** Log the error via the existing `logInfo` helper:

```go
if err := WriteHeartbeat(d.config.SolHome, &Heartbeat{
    Timestamp:    time.Now().UTC(),
    PatrolCount:  d.patrolCount,
    Status:       status,
    StaleTethers: staleTethers,
    CaravanFeeds: caravanFeeds,
    Escalations:  openEsc,
}); err != nil {
    d.logInfo("consul_error", map[string]any{"action": "write_heartbeat", "error": err.Error()})
}
```

---

## Task 3: Check scanner.Err() in reader Follow()

**File:** `internal/events/reader.go`, `Follow` method

After the `scanner.Scan()` loop (around line 159), `scanner.Err()` is never checked. If the scanner hit an I/O error mid-read, the error is silently ignored and the offset is updated — potentially skipping events.

Find the section that looks like:

```go
scanner := bufio.NewScanner(f)
for scanner.Scan() {
    // ... process events ...
}

// Update offset.
newOffset, err := f.Seek(0, io.SeekCurrent)
```

Add a scanner error check after the loop, before updating the offset:

```go
scanner := bufio.NewScanner(f)
for scanner.Scan() {
    // ... process events ...
}
if err := scanner.Err(); err != nil {
    // Log but continue — DEGRADE pattern. Don't update offset
    // so we re-read on next tick.
    continue
}

// Update offset.
newOffset, err := f.Seek(0, io.SeekCurrent)
```

The key: if the scanner errored, skip the offset update so we retry from the same position on next tick.

---

## Task 4: Fix Follow() closed file descriptor on reopen failure

**File:** `internal/events/reader.go`, `Follow` method (around line 122-130)

When the reader detects a file replacement (chronicle truncation), it closes the old fd and tries to open the new file. If the open fails, `f` is now closed but the loop continues using it:

```go
f.Close()
newF, err := os.Open(r.path)
if err != nil {
    continue // BUG: f is closed, next f.Stat() will error
}
f = newF
```

**Fix:** Only close the old descriptor after successfully opening the new one:

```go
newF, err := os.Open(r.path)
if err != nil {
    continue // file may be temporarily unavailable during rename
}
f.Close()
f = newF
offset = 0
```

---

## Task 5: Log Store.Close() errors on migration failure

**File:** `internal/store/store.go`, `OpenWorld` and `OpenSphere` functions (around lines 29-31, 43-45)

Currently:
```go
if err := s.migrateWorld(); err != nil {
    s.Close()
    return nil, fmt.Errorf("failed to migrate world database %q: %w", world, err)
}
```

The `s.Close()` error is discarded. Log it to stderr:

```go
if err := s.migrateWorld(); err != nil {
    if closeErr := s.Close(); closeErr != nil {
        fmt.Fprintf(os.Stderr, "store: close failed after migration error: %v\n", closeErr)
    }
    return nil, fmt.Errorf("failed to migrate world database %q: %w", world, err)
}
```

Apply the same pattern to `OpenSphere`.

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Manually inspect that `events.go` Log() now checks the Write return
- Manually inspect that `reader.go` Follow() checks scanner.Err() and fixes the closed-fd issue
- Manually inspect that `consul.go` logs heartbeat write errors
- Manually inspect that `store.go` logs Close errors on migration failure

## Commit

```
fix(events,consul,store): arc 1 review-7 — error observability in best-effort paths
```
