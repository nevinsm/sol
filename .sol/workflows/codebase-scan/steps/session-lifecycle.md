# Session Lifecycle Review

Review the packages listed in **Focus** for correctness in session management, work dispatch, tether durability, and handoff safety.

## What to look for

### Dispatch (internal/dispatch/)
- **Atomicity**: If dispatch fails mid-way (worktree created but session fails to start), is cleanup correct? Are partial states left behind?
- **TOCTOU races**: Checking agent availability then dispatching — can another dispatch claim the same agent between check and claim?
- **Role resolution**: Does role assignment match the dispatch target correctly? Edge cases with unknown roles?

### Session (internal/session/)
- **tmux management**: Session creation, attachment, and teardown — are tmux commands robust to edge cases (session already exists, server not running, stale sockets)?
- **SOL_SESSION_COMMAND**: Is it respected everywhere? Any hardcoded `claude` references?
- **Cleanup**: When sessions die, is cleanup triggered? Orphaned tmux sessions?

### Tether (internal/tether/)
- **Durability**: Can a tether be lost? What happens if the tether file is written partially (crash mid-write)?
- **Read consistency**: Are tether reads atomic? Can a reader see a half-written tether?
- **Multi-writ**: For persistent agents — is the tether model correct for multiple writs?

### Startup (internal/startup/)
- **Prime injection**: Does the prime sequence correctly read the tether and inject execution context?
- **Missing state**: What happens when expected files are missing on startup? Graceful degradation or crash?

### Adapter (internal/adapter/)
- **Interface completeness**: Does the adapter interface cover all runtime operations? Any gaps that force callers to work around the interface?
- **Error handling**: Are adapter errors meaningful? Do they distinguish "not supported" from "failed"?

### Handoff (internal/handoff/)
- **State preservation**: Does handoff preserve everything the successor needs? Brief, tether, worktree state?
- **Crash during handoff**: If the session dies mid-handoff, what state does the successor find? Is it recoverable?
- **Summary propagation**: Is the handoff summary reliably delivered to the successor session?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Bugs with concrete failure scenarios (lost work, orphaned sessions, broken tethers, dispatch races)
- **MEDIUM**: Missing cleanup, inconsistent error handling, edge cases that produce confusing behavior
- **LOW**: Dead code, convention violations, naming inconsistencies

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
