# Operational Utilities Review

Review the packages listed in **Focus** for correctness in quota enforcement, prerequisite checking, account management, and git operations.

These packages handle the operational mechanics of the system — resource limits, environment validation, account lifecycle, and git plumbing.

## Focus

Read all `.go` files in these packages:
- `internal/quota/`
- `internal/doctor/`
- `internal/account/`
- `internal/git/`

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

### Quota (internal/quota/)
- **Enforcement**: Are quotas enforced correctly? Can they be bypassed?
- **Lock correctness**: Is the quota lock file used consistently? (This was recently fixed — verify the unified lock approach.)
- **State file atomicity**: Is `quota.json` read/written atomically? Race conditions under concurrent access?
- **Dead code**: Any unused quota logic from earlier iterations?

### Doctor (internal/doctor/)
- **Prerequisite checks**: Are all prerequisites correctly validated (tmux, git, claude, SQLite WAL)?
- **Error messages**: Are diagnostic messages actionable? Do they tell the user how to fix the problem?
- **False positives**: Can doctor incorrectly flag a working system?
- **Completeness**: Are there new prerequisites that doctor doesn't check?

### Account (internal/account/)
- **Account management**: Create, update, delete — all correct? Any orphaned references?
- **Credential handling**: Are credentials stored and retrieved correctly? Any plaintext credential leaks?
- **Removal cleanup**: When an account is removed, is all related state (quota, sessions) cleaned up?

### Git (internal/git/)
- **Error handling**: Are git command errors properly surfaced? Any silently discarded errors?
- **Edge cases**: Detached HEAD, dirty worktrees, missing remotes, submodules — handled correctly?
- **Worktree management**: Create, remove, list — any orphaned worktrees on error?
- **Conflict detection**: Is merge conflict detection correct? Can it miss conflicts or false-positive?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Quota bypass, credential leaks, git operations that lose data, doctor false positives that block legitimate use
- **MEDIUM**: Missing cleanup on account removal, git edge cases, incomplete prerequisite checks
- **LOW**: Dead code, convention violations, minor inconsistencies

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
