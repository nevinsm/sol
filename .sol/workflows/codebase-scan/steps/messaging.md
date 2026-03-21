# Messaging Systems Review

Review the packages listed in **Focus** for correctness in message delivery, notification routing, escalation handling, and inbox management.

These packages handle inter-agent and agent-to-operator communication. Bugs here mean lost messages, missed escalations, or incorrect notification routing.

## What to look for

### Broker (internal/broker/)
- **Message delivery**: Are messages reliably delivered? Any lost messages under concurrent access?
- **Path correctness**: Are broker paths constructed correctly for all message types?
- **Ordering**: Are messages ordered correctly? Can reordering cause issues?
- **Cleanup**: Are old messages cleaned up? Any unbounded growth?

### Nudge (internal/nudge/)
- **Notification delivery**: Are nudges delivered to the correct agent session? What if the session is dead?
- **Recipient canonicalization**: Are recipients in world/agent format before nudging?
- **Deduplication**: Can the same notification fire repeatedly?
- **Timing**: Are nudges timely or can they be delayed indefinitely?

### Inbox (internal/inbox/)
- **Message lifecycle**: Send, read, acknowledge — all clean? Any messages stuck in limbo?
- **Concurrent access**: Multiple agents reading the same inbox?
- **Priority handling**: Are message priorities correctly ordered and respected?

### Escalation (internal/escalation/)
- **Escalation flow**: Is the escalation path from agent → autarch correct? Any lost escalations?
- **State tracking**: Are escalation states tracked correctly (open → acknowledged → resolved)?
- **Resolution**: When an escalation is resolved, is all related state cleaned up?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Lost messages, broken escalation paths, incorrect notification routing
- **MEDIUM**: Deduplication gaps, cleanup issues, priority ordering bugs, concurrent access problems
- **LOW**: Convention violations, minor inconsistencies, logging gaps

Each finding must include:
1. One-line summary
2. File path and line range
3. **The actual code** — quote the specific lines that demonstrate the issue
4. Concrete failure scenario
5. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Include the code.** Every finding must quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Be specific.** Name the function, the line, the exact failure sequence.

**Verify claims against code.** Read the actual implementation before writing a finding.
