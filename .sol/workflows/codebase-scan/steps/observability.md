# Observability Systems Review

Review the packages listed in **Focus** for correctness in token accounting, event logging, chronicle management, and trace handling.

These packages record what happened, when, and how much it cost. Bugs here mean incorrect billing, lost audit trails, or broken operational visibility.

## Focus

Read all `.go` files in these packages:
- `internal/ledger/`
- `internal/chronicle/`
- `internal/events/`
- `internal/trace/`

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
