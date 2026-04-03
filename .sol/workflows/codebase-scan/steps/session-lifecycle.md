# Session Lifecycle Review

Review the packages listed in **Focus** for correctness in session management, work dispatch, tether durability, and handoff safety.

## Focus

Read all `.go` files in these packages:
- `internal/startup/`
- `internal/dispatch/`
- `internal/session/`
- `internal/tether/`
- `internal/adapter/`
- `internal/handoff/`
- `internal/budget/`
- `internal/guidelines/`

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

### Budget (internal/budget/)
- **Estimate accuracy**: Is the context budget estimate correct? Does it account for all injected content (persona, brief, skills, writ description, guidelines)?
- **Overflow handling**: What happens when a writ exceeds the estimated budget? Is the caller informed?
- **Edge cases**: Empty writ descriptions, very large briefs, many skills -- are these handled?

### Guidelines (internal/guidelines/)
- **Three-tier resolution**: Do guidelines resolve correctly across project, user, and embedded tiers? Is the fallback chain correct?
- **Variable substitution**: Is Render() safe against injection via template variables? What if a variable value contains template syntax?
- **Missing templates**: What happens when the requested guidelines template doesn't exist? Is the error clear?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Bugs with concrete failure scenarios (lost work, orphaned sessions, broken tethers, dispatch races)
- **MEDIUM**: Missing cleanup, inconsistent error handling, edge cases that produce confusing behavior
- **LOW**: Dead code, convention violations, naming inconsistencies

Each finding must include:
1. One-line summary
2. File path and line range
3. **The actual code** — quote the specific lines that demonstrate the issue
4. Concrete failure scenario (not hypothetical — describe the sequence of events)
5. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Include the code.** Every finding must quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Be specific.** Name the function, the line, the exact failure sequence.

**Verify claims against code.** Read the actual implementation before writing a finding.
