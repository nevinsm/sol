# Supervision Layer Review

Review the packages listed in **Focus** for correctness in health detection, session supervision, stale resource cleanup, and crash recovery.

These packages form the supervision tree — prefect supervises world processes, sentinel monitors agent health, consul detects orphaned resources. Bugs here can kill healthy sessions, let broken agents run indefinitely, or leak resources.

## Focus

Read all `.go` files in these packages:
- `internal/sentinel/`
- `internal/consul/`
- `internal/prefect/`
- `internal/service/`
- `internal/heartbeat/`

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

### Sentinel (internal/sentinel/)
- **Health detection**: Are stalled/hung agents detected correctly? False positives? False negatives?
- **Output hashing (ADR-0003)**: Is the hash comparison correct for detecting stalled output?
- **AI callout gating**: Are AI assessments only triggered when heuristics detect trouble? Any unnecessary callouts?
- **Wake/sleep**: Does sentinel correctly handle world sleep/wake transitions?

### Consul (internal/consul/)
- **Stale tether detection**: Is the staleness threshold correct? Can it incorrectly flag active work as stale?
- **Stranded caravan detection**: Does it correctly identify caravans with no active agents?
- **Orphan cleanup**: When consul finds orphans, is the cleanup safe? Can it kill active work?
- **Grace periods**: Are grace periods durable across consul restarts?

### Prefect (internal/prefect/)
- **Session respawn**: Is respawn logic correct? Exponential backoff? Max retries?
- **Component supervision**: Does prefect correctly monitor sentinel, consul, forge? Miss any components?
- **Startup ordering**: Are components started in the right order? Dependencies respected?
- **Shutdown**: Is shutdown clean? Are supervised processes terminated gracefully?

### Service (internal/service/)
- **Service lifecycle**: Install, uninstall, start, stop — all clean transitions?
- **Status reporting**: Is the service status accurate? Can it report running when actually dead?

### Heartbeat (internal/heartbeat/)
- **Heartbeat paths**: Are heartbeat files written to the correct location (.runtime/)?
- **Staleness detection**: Is the threshold appropriate? Clock skew handling?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Bugs that cause lost work (false-positive orphan killing, incorrect respawn), supervision failure (component unmonitored), resource leaks
- **MEDIUM**: Detection inaccuracies, missing retry bounds, cleanup gaps, grace period issues
- **LOW**: Dead code, convention violations, logging gaps

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
