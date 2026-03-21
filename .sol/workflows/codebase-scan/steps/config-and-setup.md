# Config, Setup, and Utilities Review

Review the packages listed in **Focus** for correctness, robustness, and adherence to project conventions.

## What to look for

### Config (internal/config/)
- **Layering correctness**: Does sol.toml → world.toml layering resolve correctly? Are defaults applied when keys are missing?
- **TOML parsing edge cases**: Missing files handled? Malformed TOML produces clear errors?
- **Zero values**: Are zero values distinguishable from "not set"? Does this cause surprising defaults?
- **ResolveWorld**: Is the flag → env var → cwd detection chain correct and consistently used?

### Setup (internal/setup/)
- **First-time setup**: Does `sol init` correctly create all required directories and files?
- **Exclude list**: Are all sol-managed paths (`.claude/settings.local.json`, `CLAUDE.local.md`, `.claude/skills/`, `.brief/`, `.workflow/`) in the git exclude list?
- **Idempotency**: Can setup run multiple times without breaking anything?

### Utility Packages (fileutil, processutil, logutil, envfile, namepool)
- **File operations**: Atomic writes where needed? Proper cleanup on error? Race conditions on shared paths?
- **Process management**: Zombie processes? Signal handling? Timeout behavior?
- **Name pool**: Exhaustion handling? Collision detection? What happens when all names are taken?
- **Env file parsing**: Edge cases with quoting, empty values, comments, multi-line values?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Bugs with concrete failure scenarios (data loss, corruption, race conditions, crashes)
- **MEDIUM**: Silent error discard, missing error context, incorrect behavior under edge cases
- **LOW**: Dead code, convention violations, minor inconsistencies

Each finding must include:
1. One-line summary
2. File path and line range
3. **The actual code** — quote the specific lines that demonstrate the issue
4. Concrete failure scenario (not hypothetical — describe the sequence of events)
5. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Even trivial fixes — document them and move on.

**Include the code.** Every finding must quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Be specific.** "Error handling could be improved" is not a finding. Name the function, the line, the exact failure sequence.

**Verify claims against code.** Before writing a finding, confirm the issue exists by reading the actual code. False positives waste everyone's time.
