# Observability Systems Review

Review the packages listed in **Focus** for correctness in token accounting, event logging, chronicle management, and trace handling.

These packages record what happened, when, and how much it cost. Bugs here mean incorrect billing, lost audit trails, or broken operational visibility.

## What to look for

### Ledger (internal/ledger/)
- **Token accounting accuracy**: Are token counts captured and stored correctly? Any off-by-one or double-counting?
- **OTel OTLP receiver**: Is the receiver correctly parsing incoming telemetry? Error handling for malformed payloads?
- **History tracking**: Is the ensureHistory mechanism race-safe under concurrent agent reporting?
- **Aggregation**: Are roll-ups and summaries computed correctly?

### Chronicle (internal/chronicle/)
- **Event logging**: Are events written atomically? Can partial writes corrupt the log?
- **Heartbeat**: Is the heartbeat written to .runtime/ (not SOL_HOME root)?
- **Rotation/cleanup**: Are old chronicles cleaned up? Size bounds?
- **Truncation safety**: Is log truncation safe? (This was recently fixed — verify the atomic rename approach is sound.)

### Events (internal/events/)
- **Event types**: Are all event types properly defined? Any missing events for important operations?
- **Event emission**: Are events emitted at the right points in the codebase? Any operations that should emit events but don't?
- **Reader correctness**: Is the events reader correct for both snapshot reads and follow mode? Buffer sizing adequate?

### Trace (internal/trace/)
- **Trace correctness**: Are spans and traces correctly structured? Any broken parent-child relationships?
- **Performance**: Does tracing add unacceptable overhead?
- **Integration**: Is the trace integration with the ledger correct?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Incorrect token accounting, event data loss, chronicle corruption, broken trace structures
- **MEDIUM**: Missing event emissions, cleanup issues, race conditions in concurrent reporting
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
