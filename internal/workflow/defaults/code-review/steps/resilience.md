# Resilience Review

Review the code changes for error handling and failure mode coverage.

Examine the branch diff against main. Think about what happens when things go wrong.

**Look for:**
- Swallowed errors — empty catch blocks, ignored error returns, _ = err patterns
- Missing error propagation — errors logged but not returned to callers
- Unclear error messages — "operation failed" with no context about what or why
- Insufficient retry/backoff logic for transient failures
- Missing timeout handling — HTTP calls, database queries, or exec without deadlines
- Resource cleanup on failure — files, connections, temp dirs left open on error paths
- Partial failure states — half-written data when an operation fails midway
- Missing circuit breakers for external service calls
- Unhelpful panic/crash behavior — panics in library code, panics without recovery
- Recovery path gaps — what happens after a restart?
- Defer misuse — deferred calls that depend on mutable state

**Questions to answer:**
- What happens when external services (database, API, filesystem) fail?
- Can the system recover from partial failures without manual intervention?
- Are errors actionable for operators — can they diagnose from the error message alone?
- Is cleanup guaranteed even on error paths (defer, finally)?
- Are timeouts set for all blocking operations?

Prioritize resilience gaps that could cause data loss or require manual recovery.
