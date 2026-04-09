# Config, Setup, and Utilities Review

Review the packages listed in **Focus** for correctness, robustness, and adherence to project conventions.

## Focus

Read all `.go` files in these packages:
- `internal/config/`
- `internal/setup/`
- `internal/fileutil/`
- `internal/processutil/`
- `internal/logutil/`
- `internal/envfile/`
- `internal/namepool/`

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

### Config (internal/config/)
- **Layering correctness**: Does sol.toml → world.toml layering resolve correctly? Are defaults applied when keys are missing?
- **TOML parsing edge cases**: Missing files handled? Malformed TOML produces clear errors?
- **Zero values**: Are zero values distinguishable from "not set"? Does this cause surprising defaults?
- **ResolveWorld**: Is the flag → env var → cwd detection chain correct and consistently used?

### Setup (internal/setup/)
- **First-time setup**: Does `sol init` correctly create all required directories and files?
- **Exclude list**: Are all sol-managed paths (`.claude/settings.local.json`, `CLAUDE.local.md`, `.claude/skills/`, `.workflow/`) in the git exclude list?
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
