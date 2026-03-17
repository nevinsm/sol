# Core Infrastructure Review

Review the packages listed in **Focus** for correctness, robustness, and adherence to project conventions.

## What to look for

### Store (internal/store/)
- **SQLite correctness**: WAL mode set on every connection? `busy_timeout` and `foreign_keys` pragmas? Prepared statements closed properly?
- **Migration safety**: Are migrations sequential and idempotent? Any destructive operations (DROP without backup)?
- **Concurrency**: Race conditions in concurrent access patterns? Transactions held too long?
- **Error propagation**: Are database errors wrapped with context (`fmt.Errorf("... %q: %w", ...)`)? Any silently discarded errors?

### Config (internal/config/)
- **Layering correctness**: Does sol.toml → world.toml layering resolve correctly? Are defaults applied when keys are missing?
- **TOML parsing edge cases**: Missing files handled? Malformed TOML produces clear errors?
- **Zero values**: Are zero values distinguishable from "not set"? Does this cause surprising defaults?

### Setup, fileutil, processutil, logutil, envfile, git, namepool
- **File operations**: Atomic writes where needed? Proper cleanup on error? Race conditions on shared paths?
- **Process management**: Zombie processes? Signal handling? Timeout behavior?
- **Git operations**: Proper error handling for git commands? Edge cases with detached HEAD, dirty worktrees, missing remotes?
- **Name pool**: Exhaustion handling? Collision detection?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Bugs with concrete failure scenarios (data loss, corruption, race conditions, crashes)
- **MEDIUM**: Silent error discard, missing error context, incorrect behavior under edge cases
- **LOW**: Dead code, convention violations, minor inconsistencies

Each finding must include:
1. One-line summary
2. File path and line range
3. Concrete failure scenario (not hypothetical — describe the sequence of events)
4. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Even trivial fixes — document them and move on. Fixing in-leg produces orphaned changes that never reach main.

**Be specific.** "Error handling could be improved" is not a finding. "OpenWorld returns nil error when world.toml is missing but directory exists (config.go:87), causing downstream nil-pointer panic in LoadAgents" is a finding.

**Be honest about severity.** A doc comment typo is LOW. A race condition that corrupts the database is HIGH. If you're unsure, err toward the lower severity.

**Verify claims against code.** Before writing a finding, confirm the issue exists by reading the actual code. False positives waste everyone's time.
