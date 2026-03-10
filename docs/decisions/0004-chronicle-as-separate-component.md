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
