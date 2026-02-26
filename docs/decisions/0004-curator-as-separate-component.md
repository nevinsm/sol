# ADR-0004: Curator as Separate Component from Event Feed

Status: accepted
Date: 2026-02-26
Loop: 3

## Context

The target architecture (Section 3.11) describes the event feed as having
two parts: a raw event log (`.events.jsonl`) and a curated feed
(`.feed.jsonl`) produced by a curator process. During Loop 3 prompt
design, we needed to decide whether to bundle the curator into the event
feed package/prompt or split it out.

The curator has three responsibilities:
1. **Deduplication** — collapse identical events within a 10s window
2. **Aggregation** — batch sling bursts (10+ dispatches in 30s) into
   summary events
3. **Truncation** — keep the curated feed under 10MB

Each of these has distinct test scenarios and failure modes.

## Decision

Split the curator into its own prompt (Loop 3, prompt 03) and implement
it as a distinct process (`gt curator run/start/stop`) rather than
embedding it in the event logger.

The event feed (prompt 02) handles:
- Event logging (append-only JSONL with flock)
- Event reading (with filtering, limiting, and follow mode)
- Instrumentation of existing operations

The curator (prompt 03) handles:
- Reading raw events from a checkpoint offset
- Dedup, aggregation, and truncation
- Writing the curated feed
- Checkpoint persistence for resume after restart

`gt feed` reads the curated feed by default, falling back to raw if the
curator hasn't run. A `--raw` flag reads the unprocessed feed.

## Consequences

**Benefits:**
- Each component has a focused test surface — event logging tests don't
  need dedup/aggregation logic, curator tests don't need flock
  concurrency tests
- The curator is independently deployable — `gt curator start` runs it
  as a background process, `gt curator stop` stops it
- If the curator crashes, raw event logging continues unaffected
  (DEGRADE principle) and `gt feed --raw` still works
- Cleaner prompt decomposition — the event feed prompt is already
  substantial with instrumentation of sling/done/refinery/supervisor

**Tradeoffs:**
- One more process to manage (curator alongside supervisor, refinery,
  witness)
- Without the curator running, `gt feed` shows raw unfiltered events
  (acceptable — operators can start the curator when they want curation)
- Checkpoint file adds a small piece of state to manage

**Note:** The curator is town-level (one instance, all rigs), matching
the event feed which is also a single file across all rigs.
