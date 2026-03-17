# Service Components Review

Review the packages listed in **Focus** for correctness in long-running system processes, crash recovery, and supervision behavior.

## What to look for

### Forge (internal/forge/)
- **Merge ordering**: Is the queue processed in the correct order? Can reordering cause conflicts?
- **Quality gates**: Are build/test/lint gates applied correctly? Can a gate bypass occur?
- **Crash recovery**: If forge dies mid-merge, what state is left? Is the branch safe? Is the queue consistent?
- **Retry behavior**: Failed merges — are retries safe and idempotent? Any infinite retry loops?
- **Branch cleanup**: Are branches cleaned up after successful merge? What about after permanent failure?

### Sentinel (internal/sentinel/)
- **Health detection**: Are stalled/hung agents detected correctly? False positives? False negatives?
- **Output hashing (ADR-0003)**: Is the hash comparison correct for detecting stalled output?
- **AI callout gating**: Are AI assessments only triggered when heuristics detect trouble? Any unnecessary callouts?
- **Wake/sleep**: Does sentinel correctly handle world sleep/wake transitions? Any monitoring of sleeping worlds?

### Consul (internal/consul/)
- **Stale tether detection**: Is the staleness threshold correct? Can it incorrectly flag active work as stale?
- **Stranded caravan detection**: Does it correctly identify caravans with no active agents?
- **Supervision boundary**: Does consul stay sphere-scoped? Any world-level assumptions?
- **Orphan cleanup**: When consul finds orphans, is the cleanup safe? Can it kill active work?

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
- **Cleanup**: Are stale heartbeat files cleaned up?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Bugs that cause lost work (merge corruption, false-positive orphan killing, incorrect respawn), data loss, or supervision failure
- **MEDIUM**: Detection inaccuracies, missing retry bounds, cleanup gaps
- **LOW**: Dead code, convention violations, logging gaps

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
