# Messaging Systems Review

Review the packages listed in **Focus** for correctness in message delivery, notification routing, escalation handling, and inbox management.

These packages handle inter-agent and agent-to-operator communication. Bugs here mean lost messages, missed escalations, or incorrect notification routing.

## Focus

Read all `.go` files in these packages:
- `internal/broker/`
- `internal/nudge/`
- `internal/inbox/`
- `internal/escalation/`

## Process

1. **Read every file in the Focus packages end-to-end** before looking for issues. Understand the code as written, not as you imagine it.
2. As you read, note anything that looks wrong. Only record findings where you can point to specific lines you just read.
3. After reading all files, check your notes against the categories in "What to look for" below.
4. Before reporting a finding, check `.sol/workflows/codebase-scan/baseline.json` (if it exists). If the file and function are listed and your finding matches the reviewed pattern, do not report it. See `.sol/workflows/codebase-scan/BASELINE.md` for matching rules.
5. For each potential finding, **verify before reporting**:
   - Copy the ACTUAL code from the file into your finding. Do not paraphrase, summarize, or reconstruct from memory.
   - Confirm the issue exists in the code you just read, not in a hypothetical version of it.
   - Run `git log --oneline -5 -- <file>` for each cited file. If the file was modified in the last 2 weeks, check whether recent commits already addressed this issue. If so, do not report it.
   - Construct the concrete sequence of events that triggers the bug. If you cannot trace a real call path that reaches the faulty code, the finding is theoretical and should not be reported.

A finding with fabricated or approximate code quotes is worse than no finding. It wastes triage time and downstream agent cycles. When in doubt, leave it out.

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
