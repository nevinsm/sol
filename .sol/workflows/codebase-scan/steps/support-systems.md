# Support Systems Review

Review the packages listed in **Focus** for correctness in token accounting, message brokering, event handling, and operational utilities.

## What to look for

### Ledger (internal/ledger/)
- **Token accounting accuracy**: Are token counts captured and stored correctly? Any off-by-one or double-counting?
- **OTel OTLP receiver**: Is the receiver correctly parsing incoming telemetry? Error handling for malformed payloads?
- **History tracking**: Is the ensureHistory mechanism race-safe under concurrent agent reporting?
- **Aggregation**: Are roll-ups and summaries computed correctly?

### Broker (internal/broker/)
- **Message delivery**: Are messages reliably delivered? Any lost messages under concurrent access?
- **Path correctness**: Are broker paths constructed correctly for all message types?
- **Ordering**: Are messages ordered correctly? Can reordering cause issues?
- **Cleanup**: Are old messages cleaned up? Any unbounded growth?

### Nudge (internal/nudge/)
- **Notification delivery**: Are nudges delivered to the correct agent session? What if the session is dead?
- **Deduplication**: Can the same notification fire repeatedly?
- **Timing**: Are nudges timely or can they be delayed indefinitely?

### Chronicle (internal/chronicle/)
- **Event logging**: Are events written atomically? Can partial writes corrupt the log?
- **Heartbeat**: Is the heartbeat written to .runtime/ (not SOL_HOME root)?
- **Rotation/cleanup**: Are old chronicles cleaned up? Size bounds?

### Events (internal/events/)
- **Event types**: Are all event types properly defined? Any missing events for important operations?
- **Event emission**: Are events emitted at the right points in the codebase? Any operations that should emit events but don't?

### Quota (internal/quota/)
- **Enforcement**: Are quotas enforced correctly? Can they be bypassed?
- **Dead code**: Any unused quota logic from earlier iterations?

### Doctor (internal/doctor/)
- **Prerequisite checks**: Are all prerequisites correctly validated (tmux, git, claude, SQLite WAL)?
- **Error messages**: Are diagnostic messages actionable? Do they tell the user how to fix the problem?
- **False positives**: Can doctor incorrectly flag a working system?

### Escalation (internal/escalation/)
- **Escalation flow**: Is the escalation path from agent → autarch correct? Any lost escalations?
- **State tracking**: Are escalation states tracked correctly (open → acknowledged → resolved)?

### Inbox (internal/inbox/)
- **Message lifecycle**: Send, read, acknowledge — all clean? Any messages stuck in limbo?
- **Concurrent access**: Multiple agents reading the same inbox?

### Account (internal/account/)
- **Account management**: Create, update, delete — all correct? Any orphaned references?

### Trace (internal/trace/)
- **Trace correctness**: Are spans and traces correctly structured? Any broken parent-child relationships?
- **Performance**: Does tracing add unacceptable overhead?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Lost messages, incorrect token accounting, broken escalation paths, data corruption
- **MEDIUM**: Missing event emissions, deduplication gaps, cleanup issues, dead code
- **LOW**: Convention violations, minor inconsistencies, logging gaps

Each finding must include:
1. One-line summary
2. File path and line range
3. Concrete failure scenario
4. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Be specific.** Name the function, the line, the exact failure sequence.

**Verify claims against code.** Read the actual implementation before writing a finding.
