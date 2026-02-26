# Prompt 02: Loop 3 — Event Feed and Observability

You are extending the `gt` orchestration system with the event feed — an
append-only activity log that provides real-time observability into system
operations. This prompt adds event logging infrastructure, instruments
existing operations to emit events, and provides the `gt feed` CLI command.

**Working directory:** `~/gt-src/`
**Prerequisite:** Loop 3 prompt 01 (mail system) is complete.

Read all existing code first. Understand the store package
(`internal/store/`), the dispatch package (`internal/dispatch/` —
especially `Sling()` and `Done()`), the refinery package
(`internal/refinery/`), and the supervisor package
(`internal/supervisor/`).

Read `docs/target-architecture.md` Section 3.11 (Event Feed) for design
context.

---

## Task 1: Event Logger Package

Create `internal/events/` package with the core event logging
infrastructure.

### Event Structure

```go
// internal/events/events.go
package events

import "time"

// Event represents a single system event.
type Event struct {
    Timestamp  time.Time `json:"ts"`
    Source     string    `json:"source"`      // "gt", agent ID, or component name
    Type      string    `json:"type"`         // event type (see constants)
    Actor     string    `json:"actor"`        // who triggered the event
    Visibility string   `json:"visibility"`   // "feed", "audit", or "both"
    Payload   any       `json:"payload"`      // event-specific data
}
```

### Event Types

```go
// Event type constants
const (
    EventSling          = "sling"           // work dispatched to agent
    EventDone           = "done"            // agent completed work
    EventMergeQueued    = "merge_queued"    // merge request created
    EventMergeClaimed   = "merge_claimed"   // refinery claimed MR
    EventMerged         = "merged"          // merge successful
    EventMergeFailed    = "merge_failed"    // merge failed
    EventSessionStart   = "session_start"   // tmux session started
    EventSessionStop    = "session_stop"    // tmux session stopped
    EventRespawn        = "respawn"         // supervisor respawned agent
    EventMassDeath      = "mass_death"      // mass death detected
    EventDegraded       = "degraded"        // entered degraded mode
    EventRecovered      = "recovered"       // exited degraded mode
    EventPatrol         = "patrol"          // witness patrol completed
    EventStalled        = "stalled"         // agent detected as stalled
    EventMailSent       = "mail_sent"       // message sent
)
```

### Logger

```go
// Logger handles event logging to the JSONL event feed.
type Logger struct {
    path string // path to the events JSONL file
}

// NewLogger creates an event logger.
// The events file is at $GT_HOME/.events.jsonl.
// Creates the file if it doesn't exist.
func NewLogger(gtHome string) *Logger

// Log writes an event to the JSONL file.
// Uses cross-process flock for safe concurrent appending.
// This is best-effort — errors are silently ignored (DEGRADE principle).
// Events must never block primary operations.
func (l *Logger) Log(event Event)

// Emit is a convenience method for logging common events.
// Creates the Event struct and calls Log.
func (l *Logger) Emit(eventType, source, actor, visibility string, payload any)
```

### Implementation Notes

**File path:** `$GT_HOME/.events.jsonl` — append-only, one JSON object
per line.

**Concurrent writes:** Use `syscall.Flock` with `LOCK_EX` (exclusive
lock) around each append. The lock is held only for the duration of the
write (microseconds), so contention is negligible even with 30 agents.

```go
func (l *Logger) Log(event Event) {
    f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return // best-effort, silent failure
    }
    defer f.Close()

    if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
        return
    }
    defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

    event.Timestamp = time.Now().UTC()
    data, err := json.Marshal(event)
    if err != nil {
        return
    }
    f.Write(append(data, '\n'))
}
```

**Visibility levels:**
- `"feed"` — shown in `gt feed` (operator-facing activity stream)
- `"audit"` — logged for audit trail only (not in feed)
- `"both"` — shown in feed AND logged for audit

**Best-effort guarantee:** `Log()` must never return an error or panic.
All failures are silently swallowed. The event feed is observability
infrastructure — it must never interfere with primary operations.

---

## Task 2: Feed Reader

Add a feed reader to the events package for consuming the event log.

```go
// Reader reads events from the JSONL event feed.
type Reader struct {
    path string
}

// NewReader creates an event feed reader.
func NewReader(gtHome string) *Reader

// Read returns events from the feed, with optional filtering.
// Returns events in chronological order.
func (r *Reader) Read(opts ReadOpts) ([]Event, error)

// Follow opens the feed for tailing (like tail -f).
// Sends events to the channel as they appear.
// Blocks until the context is cancelled.
func (r *Reader) Follow(ctx context.Context, opts ReadOpts, ch chan<- Event) error
```

### ReadOpts

```go
// ReadOpts controls event filtering and limiting.
type ReadOpts struct {
    Limit    int       // max events to return (0 = unlimited)
    Since    time.Time // only events after this time (zero = all)
    Type     string    // filter by event type (empty = all)
    Source   string    // filter by source (empty = all)
}
```

### Implementation Notes

**Read:** Open the JSONL file, scan line by line, unmarshal each line
into an Event, apply filters, collect results. If `Limit > 0`, return
only the last N matching events (tail semantics — most recent events are
most useful). Filter events with `visibility="audit"` out of feed reads
(only show "feed" and "both").

**Follow:** Use a file polling approach (check file size every 500ms).
When the file grows, read new lines from the last known offset, parse
and filter events, send matching events to the channel. This is simpler
and more portable than inotify/fsnotify.

**Tail semantics for Read:** When Limit is set, read the entire file but
only keep the last N events. For large files this is O(n), but the event
feed is expected to be reasonable in size. If performance becomes an
issue, the curator (future) will truncate the file.

---

## Task 3: Instrument Existing Operations

Add event emission to key operations throughout the codebase. The logger
is passed as an optional dependency — if nil, no events are emitted
(DEGRADE principle).

### Dispatch Package

Modify `internal/dispatch/dispatch.go`:

**Sling()** — emit after successful dispatch:
```go
// After successful sling:
if logger != nil {
    logger.Emit(events.EventSling, "gt", "operator", "both", map[string]string{
        "work_item_id": workItemID,
        "agent":        agentName,
        "rig":          rig,
    })
}
```

**Done()** — emit after successful done:
```go
// After successful done:
if logger != nil {
    logger.Emit(events.EventDone, "gt", result.AgentName, "both", map[string]string{
        "work_item_id": result.WorkItemID,
        "agent":        result.AgentName,
        "branch":       result.BranchName,
        "merge_request": result.MergeRequestID,
    })
}
```

### Interface Approach

Rather than passing the logger through every function signature, add it
as an optional field on a context struct or pass it directly where
needed. Choose the approach that minimizes changes to existing function
signatures.

**Option A — Pass logger to functions that emit:**
```go
func Sling(opts SlingOpts, logger *events.Logger) (*SlingResult, error)
func Done(opts DoneOpts, logger *events.Logger) (*DoneResult, error)
```

**Option B — Add logger to an options struct:**
```go
type SlingOpts struct {
    // existing fields...
    Logger *events.Logger // optional, nil = no events
}
```

Choose whichever is more consistent with the existing codebase patterns.
The critical constraint is: **nil logger must be safe** — no panics, no
errors. All existing callers that don't pass a logger must continue
working.

### Refinery Package

Modify `internal/refinery/refinery.go` to accept an optional logger:

- On merge claimed: `EventMergeClaimed`
- On merge success: `EventMerged`
- On merge failure: `EventMergeFailed`

### Supervisor Package

Modify `internal/supervisor/supervisor.go` to accept an optional logger:

- On respawn: `EventRespawn`
- On mass death detected: `EventMassDeath`
- On entering degraded mode: `EventDegraded`
- On exiting degraded mode: `EventRecovered`

### CLI Layer

Create the logger in `cmd/` and pass it to dispatch/refinery/supervisor.
The logger is created once per command invocation:

```go
logger := events.NewLogger(config.Home())
```

---

## Task 4: CLI Commands

### gt feed

Create `cmd/feed.go` with the `gt feed` command.

```
gt feed [--follow] [--limit=N] [--since=<duration>] [--type=<type>] [--json]
```

- `--follow` (`-f`): tail mode — stream events as they appear (Ctrl+C
  to stop)
- `--limit` (`-n`): show only the last N events (default 20)
- `--since`: show events from the last duration (e.g., `1h`, `30m`,
  `24h`)
- `--type`: filter by event type (e.g., `sling`, `done`, `patrol`)
- `--json`: output raw JSONL (one JSON object per line)

**Human output format:**
```
[14:23:05] sling     operator    Dispatched gt-a1b2c3d4 → Toast (myrig)
[14:23:08] session   supervisor  Started session gt-myrig-Toast
[14:25:12] done      Toast       Completed gt-a1b2c3d4 (Add login validation)
[14:25:13] merge     refinery    Claimed mr-e5f6a7b8 for merge
[14:26:01] merged    refinery    Merged mr-e5f6a7b8 to main
[14:30:00] patrol    witness     Patrol complete: 3 healthy, 0 stalled (myrig)
```

Use fixed-width columns for timestamp, type, and actor. The description
is derived from the event payload.

**Follow mode:** Uses the Reader's Follow method. Print each event as it
arrives. Handle SIGINT/SIGTERM for graceful exit.

### gt log-event

Create a plumbing command for manual event emission:

```
gt log-event --type=<type> --actor=<actor> [--source=<source>] [--visibility=feed|audit|both] [--payload=<json>]
```

This is a plumbing command for scripts and hooks to emit custom events.

- `--type` (required): event type
- `--actor` (required): who triggered the event
- `--source`: event source (default: "gt")
- `--visibility`: event visibility (default: "both")
- `--payload`: JSON payload (default: "{}")

Output: `Logged: <type> by <actor>`

---

## Task 5: Tests

### Event Logger Tests

Create `internal/events/events_test.go`:

```go
func TestLogEvent(t *testing.T)
    // Create logger with temp dir
    // Log an event
    // Read the JSONL file, verify one line of valid JSON
    // Parse the event, verify all fields

func TestLogMultipleEvents(t *testing.T)
    // Log 5 events
    // Read file, verify 5 lines
    // Verify chronological order (timestamps non-decreasing)

func TestLogBestEffort(t *testing.T)
    // Create logger pointing to non-existent directory
    // Log should not panic or return error
    // (The logger silently swallows errors)

func TestLogConcurrent(t *testing.T)
    // Launch 10 goroutines each logging 10 events
    // Wait for all to complete
    // Read file, verify exactly 100 lines
    // Each line must be valid JSON (no interleaving)
```

### Feed Reader Tests

```go
func TestReadEvents(t *testing.T)
    // Log 10 events of mixed types
    // Read with no filters -> 10 events
    // Read with Limit=5 -> last 5 events
    // Read with Type filter -> only matching events

func TestReadSince(t *testing.T)
    // Log events with timestamps spread over time
    // Read with Since -> only events after cutoff

func TestReadFiltersAuditOnly(t *testing.T)
    // Log events with visibility="audit"
    // Read -> audit-only events excluded
    // Log events with visibility="both"
    // Read -> "both" events included

func TestFollow(t *testing.T)
    // Start following in goroutine
    // Log events in main goroutine
    // Verify events arrive on channel
    // Cancel context -> Follow returns
```

### CLI Smoke Tests

Add to `test/integration/cli_loop3_test.go`:

```go
func TestCLIFeedHelp(t *testing.T)
func TestCLILogEventHelp(t *testing.T)
func TestCLIMailSendHelp(t *testing.T)
func TestCLIMailInboxHelp(t *testing.T)
func TestCLIMailReadHelp(t *testing.T)
func TestCLIMailAckHelp(t *testing.T)
func TestCLIMailCheckHelp(t *testing.T)
```

---

## Task 6: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export GT_HOME=/tmp/gt-test
   # Log some events manually
   bin/gt log-event --type=test --actor=operator --payload='{"msg":"hello"}'
   bin/gt log-event --type=test --actor=operator --payload='{"msg":"world"}'
   # View the feed
   bin/gt feed --limit=5
   bin/gt feed --type=test
   # Check the raw file
   cat /tmp/gt-test/.events.jsonl | jq .
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Guidelines

- Event logging is **best-effort**. It must never block, panic, or
  interfere with primary operations. A nil logger is always safe.
- The JSONL file format is chosen for inspectability — `cat`, `jq`,
  `grep`, and `tail -f` all work naturally (GLASS principle).
- Cross-process flock ensures no interleaved writes even with 30+
  concurrent agents.
- The curator process (dedup, aggregation, truncation) is deferred.
  For now, `gt feed` reads the raw event file directly. The raw file
  will grow unbounded — this is acceptable for the initial
  implementation and can be addressed with log rotation or the curator
  later.
- Event emission sites should be minimal — only log events that are
  useful for the operator. Don't log every function call.
- The `--follow` flag uses polling (not inotify) for portability. A
  500ms poll interval is responsive enough for human consumption.
- All existing tests must continue to pass. Functions that gain a
  logger parameter must accept nil gracefully.
- Commit after tests pass with message:
  `feat(events): add event feed with JSONL logging, reader, and CLI commands`
