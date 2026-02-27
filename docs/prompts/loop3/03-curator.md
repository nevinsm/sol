# Prompt 03: Loop 3 — Event Feed Chronicle

You are extending the `sol` orchestration system with the chronicle — a
background process that consumes the raw event feed, deduplicates and
aggregates events, and produces a curated feed suitable for operator
consumption and agent situational awareness.

**Working directory:** `~/sol-src/`
**Prerequisite:** Loop 3 prompt 02 (event feed) is complete.

Read all existing code first. Understand the events package
(`internal/events/` — Logger, Reader, Event struct, event types).

Read `docs/target-architecture.md` Section 3.11 (Event Feed) for design
context, paying attention to the chronicle's role: filtering by visibility,
deduplication, aggregation, and feed truncation.

---

## Task 1: Chronicle Process

Create `internal/events/chronicle.go` with the chronicle implementation.

### Configuration

```go
// CuratorConfig holds chronicle configuration.
type CuratorConfig struct {
    RawPath      string        // path to raw .events.jsonl
    FeedPath     string        // path to curated .feed.jsonl
    PollInterval time.Duration // how often to check for new events (default: 2s)
    DedupWindow  time.Duration // dedup window for identical events (default: 10s)
    AggWindow    time.Duration // aggregation window for burst events (default: 30s)
    MaxFeedSize  int64         // max curated feed file size in bytes (default: 10MB)
}

// DefaultCuratorConfig returns defaults for the given SOL_HOME.
func DefaultCuratorConfig(gtHome string) CuratorConfig
```

### Chronicle Struct

```go
// Chronicle processes raw events into a curated feed.
type Chronicle struct {
    config CuratorConfig
    offset int64 // file offset — tracks position in raw feed
}

// NewCurator creates a chronicle.
func NewCurator(config CuratorConfig) *Chronicle

// Run starts the chronicle loop. Blocks until context is cancelled.
func (c *Chronicle) Run(ctx context.Context) error
```

### Run Loop

The chronicle uses a simple tail-and-process loop:

1. Open (or create) the curated feed file
2. Determine starting offset in the raw feed (read from a checkpoint
   file or start at current end-of-file if first run)
3. On each poll interval:
   a. Read new lines from raw feed starting at offset
   b. Parse each line as an Event
   c. Apply filters (skip `visibility="audit"`)
   d. Apply dedup (skip events identical to one seen within DedupWindow)
   e. Apply aggregation (batch similar events within AggWindow)
   f. Append surviving events to curated feed (flock-serialized)
   g. Update offset
   h. If curated feed exceeds MaxFeedSize, truncate (keep last 75%)
4. Save checkpoint on clean shutdown

### Checkpoint File

Store the chronicle's read position so it resumes correctly after restart:

```
$SOL_HOME/.chronicle-checkpoint
```

Contents: just the byte offset as a decimal string. Read on startup,
write on each successful processing cycle and on clean shutdown.

---

## Task 2: Deduplication

### Dedup Logic

Two events are duplicates if ALL of the following match within the
`DedupWindow` (default 10s):
- Same `type`
- Same `source`
- Same `actor`

This catches common duplicates like multiple `resolve` events from the
same agent in quick succession (e.g., retry logic).

### Implementation

Maintain a sliding window of recent events. For each new event, check
if a matching event exists within the window. If so, skip it.

```go
type dedupEntry struct {
    Type   string
    Source string
    Actor  string
    SeenAt time.Time
}
```

Clean expired entries from the window on each cycle (entries older than
`DedupWindow`).

---

## Task 3: Aggregation

### Aggregation Logic

When multiple events of the same type arrive within the `AggWindow`
(default 30s), collapse them into a single summary event. This primarily
handles cast bursts (dispatching 10+ work items at once).

**Events that aggregate:**
- `cast` — "Dispatched 10 work items to myworld"
- `respawn` — "Respawned 3 agents in myworld"

**Events that do NOT aggregate** (each one is individually important):
- `resolve`, `merged`, `merge_failed`, `stalled`, `patrol`

### Aggregated Event Format

When events are aggregated, the chronicle emits a synthetic event:

```json
{
    "ts": "<timestamp of last event in batch>",
    "source": "chronicle",
    "type": "sling_batch",
    "actor": "chronicle",
    "visibility": "feed",
    "payload": {
        "type": "cast",
        "count": 10,
        "window_seconds": 30,
        "first_ts": "<timestamp of first event>",
        "last_ts": "<timestamp of last event>"
    }
}
```

### Implementation

Buffer aggregatable events. On each cycle, check if any buffer has
events older than `AggWindow`. If so, flush: if count > 1, emit an
aggregated event; if count == 1, emit the original event unchanged.

---

## Task 4: Feed Truncation

When the curated feed file exceeds `MaxFeedSize` (default 10MB):

1. Read the entire file
2. Find the byte offset at the 25% mark (discard first 25%)
3. Find the next newline after that offset (don't split a JSON line)
4. Write the remaining 75% to a temp file
5. Atomically rename temp file over the curated feed

This keeps the file bounded while preserving recent history. The 75%
retention means truncation happens infrequently (only when 25% of max
size has accumulated since last truncation).

**Safety:** Use flock on the curated feed during truncation to prevent
concurrent readers from seeing a partial file.

---

## Task 5: Feed Reader Update

Update the `Reader` in `internal/events/reader.go` to support reading
from the curated feed as well as the raw feed.

```go
// NewReader creates an event feed reader.
// If curated=true, reads from .feed.jsonl (curated feed).
// If curated=false, reads from .events.jsonl (raw feed).
func NewReader(gtHome string, curated bool) *Reader
```

Update `sol feed` in `cmd/feed.go` to read from the curated feed by
default, with a `--raw` flag for the unprocessed feed:

```
sol feed [--raw] [--follow] [--limit=N] [--since=<duration>] [--type=<type>] [--json]
```

- Default: reads curated feed (`.feed.jsonl`)
- `--raw`: reads raw event log (`.events.jsonl`)

If the curated feed doesn't exist (chronicle hasn't run), fall back to
the raw feed silently.

---

## Task 6: CLI Commands

### sol chronicle

Add chronicle commands to `cmd/chronicle.go`:

**`sol chronicle run`** — Foreground chronicle loop:
```
sol chronicle run
```
- Signal handling (SIGTERM, SIGINT)
- Creates chronicle with default config
- Runs until cancelled
- Output on start: `Chronicle started (raw: .events.jsonl → feed: .feed.jsonl)`
- Output on stop: `Chronicle stopped (offset: NNNNN)`

**`sol chronicle start`** — Background session:
```
sol chronicle start
```
- Starts tmux session `sol-chronicle`
- Runs `sol chronicle run` inside session
- Output: `Chronicle started: sol-chronicle`

**`sol chronicle stop`** — Stop session:
```
sol chronicle stop
```
- Stops the `sol-chronicle` tmux session
- Output: `Chronicle stopped: sol-chronicle`

### Registration

Register the `chronicle` command group under the root command in
`cmd/root.go`.

---

## Task 7: Tests

### Chronicle Unit Tests

Create `internal/events/curator_test.go`:

```go
func TestCuratorProcessesNewEvents(t *testing.T)
    // Write 5 events to raw feed
    // Run one chronicle cycle
    // Verify: 5 events appear in curated feed

func TestCuratorFiltersAuditOnly(t *testing.T)
    // Write events: 2 with visibility="both", 1 with visibility="audit"
    // Run chronicle cycle
    // Verify: curated feed has 2 events (audit-only filtered out)

func TestCuratorDeduplicates(t *testing.T)
    // Write 3 identical events (same type/source/actor) within 10s
    // Run chronicle cycle
    // Verify: curated feed has 1 event

func TestCuratorDeduplicateWindowExpiry(t *testing.T)
    // Write event A
    // Write event A again with timestamp > DedupWindow later
    // Run chronicle cycle
    // Verify: curated feed has 2 events (dedup window expired)

func TestCuratorAggregatesSlingBurst(t *testing.T)
    // Write 10 cast events within 30s
    // Run chronicle cycle (after AggWindow expires)
    // Verify: curated feed has 1 sling_batch event with count=10

func TestCuratorDoesNotAggregateNonBatchable(t *testing.T)
    // Write 3 "done" events within 30s
    // Run chronicle cycle
    // Verify: curated feed has 3 individual events (done is not aggregated)

func TestCuratorTruncatesFeed(t *testing.T)
    // Set MaxFeedSize to a small value (e.g., 1KB)
    // Write enough events to exceed the limit
    // Run chronicle cycle
    // Verify: curated feed size is ~75% of max
    // Verify: remaining events are valid JSON lines
    // Verify: no truncated/partial lines

func TestCuratorCheckpoint(t *testing.T)
    // Write 5 events, run chronicle
    // Stop chronicle, verify checkpoint file exists with offset
    // Write 5 more events, start new chronicle
    // Verify: only the 5 new events are processed (resumes from checkpoint)

func TestCuratorRunLifecycle(t *testing.T)
    // Start chronicle with cancellable context
    // Write events to raw feed
    // Wait for one poll cycle
    // Verify events appear in curated feed
    // Cancel context, verify clean shutdown
```

### CLI Smoke Tests

Add to `test/integration/cli_loop3_test.go`:

```go
func TestCLICuratorRunHelp(t *testing.T)
    // Run: sol chronicle run --help

func TestCLICuratorStartHelp(t *testing.T)
    // Run: sol chronicle start --help

func TestCLICuratorStopHelp(t *testing.T)
    // Run: sol chronicle stop --help
```

---

## Task 8: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   # Generate some raw events
   bin/sol log-event --type=cast --actor=operator --payload='{"world":"test"}'
   bin/sol log-event --type=cast --actor=operator --payload='{"world":"test"}'
   bin/sol log-event --type=done --actor=Toast --payload='{"item":"sol-123"}'
   # Run chronicle once
   bin/sol chronicle run &
   sleep 3
   kill %1
   # Check curated feed
   cat /tmp/sol-test/.feed.jsonl | jq .
   # Should show: deduped cast events, individual done event
   bin/sol feed --limit=5
   bin/sol feed --raw --limit=5
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- The chronicle is a **background process**, not a critical path component.
  If it crashes, the raw event feed continues growing and `sol feed`
  falls back to reading raw events. No primary operations are affected
  (DEGRADE principle).
- Dedup and aggregation windows are intentionally short (10s, 30s).
  Longer windows would delay event delivery to the curated feed.
- Truncation is atomic (write temp file, rename). No reader ever sees
  a partial file.
- The checkpoint file is simple (just a byte offset). If corrupted, the
  chronicle restarts from the current end of the raw file — it misses
  events that arrived while it was down, but doesn't reprocess old ones.
- The chronicle is **sphere-level** (not per-world). One chronicle processes
  events from all worlds.
- All existing tests must continue to pass.
- **Add chronicle to `sol status` output.** The chronicle runs as a
  sphere-level tmux session (`sol-chronicle`). Extend
  `internal/status/status.go` to check for the chronicle session and
  include it in the status output:
  ```
  Chronicle: running (sol-chronicle)
  ```
  Add a `CuratorInfo` struct (similar to `RefineryInfo`) to
  `RigStatus` — or better, add it to a new `TownStatus` if one
  exists, since the chronicle is sphere-level. If `sol status` only
  gathers per-world status, add the chronicle check at the display
  layer in `cmd/status.go`.
- Commit after tests pass with message:
  `feat(events): add chronicle with dedup, aggregation, and feed truncation`
