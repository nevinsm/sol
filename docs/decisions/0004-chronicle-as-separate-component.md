# ADR-0004: Chronicle as Separate Component from Event Feed

Status: accepted
Date: 2026-02-26
Loop: 3

## Context

The target architecture (Section 3.11) describes the event feed as having
two parts: a raw event log (`.events.jsonl`) and a curated feed
(`.feed.jsonl`) produced by a chronicle process. During Loop 3 prompt
design, we needed to decide whether to bundle the chronicle into the event
feed package/prompt or split it out.

The chronicle has three responsibilities:
1. **Deduplication** — collapse identical events within a 10s window
2. **Aggregation** — batch cast bursts (10+ dispatches in 30s) into
   summary events
3. **Truncation** — keep the curated feed under 10MB

Each of these has distinct test scenarios and failure modes.

## Decision

Split the chronicle into its own prompt (Loop 3, prompt 03) and implement
it as a distinct process (`sol chronicle run/start/stop`) rather than
embedding it in the event logger.

The event feed (prompt 02) handles:
- Event logging (append-only JSONL with flock)
- Event reading (with filtering, limiting, and follow mode)
- Instrumentation of existing operations

The chronicle (prompt 03) handles:
- Reading raw events from a checkpoint offset
- Dedup, aggregation, and truncation
- Writing the curated feed
- Checkpoint persistence for resume after restart

`sol feed` reads the curated feed by default, falling back to raw if the
chronicle hasn't run. A `--raw` flag reads the unprocessed feed.

## Consequences

**Benefits:**
- Each component has a focused test surface — event logging tests don't
  need dedup/aggregation logic, chronicle tests don't need flock
  concurrency tests
- The chronicle is independently deployable — `sol chronicle start` runs it
  as a background process, `sol chronicle stop` stops it
- If the chronicle crashes, raw event logging continues unaffected
  (DEGRADE principle) and `sol feed --raw` still works
- Cleaner prompt decomposition — the event feed prompt is already
  substantial with instrumentation of cast/done/forge/prefect

**Tradeoffs:**
- One more process to manage (chronicle alongside prefect, forge,
  sentinel)
- Without the chronicle running, `sol feed` shows raw unfiltered events
  (acceptable — the autarch can start the chronicle when they want curation)
- Checkpoint file adds a small piece of state to manage

**Note:** The chronicle is sphere-level (one instance, all worlds), matching
the event feed which is also a single file across all worlds.

## Addendum (CF-M17 / CF-M18 / CF-L5): no silent event loss

The chronicle is the system's audit-of-record for cross-world activity. To
preserve that property, the chronicle must never silently discard events.
Any code path that would otherwise drop events must surface the loss as an
observable signal.

**Required guarantees:**

1. **Atomic cycle rollback (CF-M17).** A `processCycle` either commits *all*
   of its mutations (offset advance, dedup cache additions, aggregation
   buffer additions, aggregation buffer flushes, curated-feed appends) or
   none of them. If `appendToFeed` fails partway through, the chronicle
   restores the dedup cache and the *full* aggregation buffer state to the
   pre-cycle snapshot before returning. The offset is left unchanged so the
   next cycle re-reads the same events. This prevents inflated counts in
   batch summary events on retry.

2. **Rotation drops are observable (CF-M18).** When the chronicle's own raw
   feed rotation (`logutil.TruncateIfNeeded` → atomic rename) trims bytes
   that the chronicle has not yet processed, the chronicle MUST emit a
   `chronicle_dropped` event recording at minimum
   `{reason: "raw_feed_rotation", dropped_count, dropped_bytes,
   saved_offset, tail_start}`. This event has visibility `"both"` so it
   appears in both the audit log and the curated feed. The dropped-line
   count is computed from a pre-truncation snapshot of the unprocessed tail.

3. **Oversize lines do not stall the reader (CF-L5).** Both the chronicle
   (`readNewEvents`) and `events.Reader` (`Read` and `Follow`) use a
   `bufio.Reader` with `ReadString('\n')` rather than a `bufio.Scanner`,
   so a single line that exceeds any fixed buffer cannot stall the
   read/follow loop. Only complete (newline-terminated) lines advance the
   read offset; partial trailing data is left for the next cycle/tick.

**Acceptable silent skips:** A small number of paths still skip lines
without an audit event because the data was malformed at write time and
no count is meaningful — specifically, lines that fail JSON unmarshal in
`readNewEvents` and in `Reader.Read`/`Follow`. These cases are logged to
stderr; if they become routine, a `chronicle_malformed` event family
should be added in the same shape as `chronicle_dropped`.

**Test coverage:** `internal/events/chronicle_test.go` and
`internal/events/events_test.go` exercise each of the three guarantees:
`TestChronicleAggBufferRollback`, `TestChronicleEmitsDroppedEventOnRotation`
(uses a test-only `testHookBeforeRotate` seam to deterministically trigger
the read-rotation race), and `TestFollowSurvivesOversizeLine`.
