# Integration Tests Review

Review the integration test suite for coverage gaps, isolation correctness, and test quality.

This is NOT a code review of the test implementations for bugs — it's an assessment of whether the test suite adequately validates the system's behavior and whether tests are correctly isolated.

## Focus

Read all `.go` files in these packages:
- `test/integration/`

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

### Coverage Gaps
- **Untested commands**: Cross-reference cmd/ against test/integration/ — which commands have no integration test coverage?
- **Untested failure modes**: Read docs/failure-modes.md. Are the documented crash recovery paths tested? Which failure modes have no test?
- **Untested workflows**: Are workflow modes (inline, manifest) tested end-to-end?
- **Missing loop coverage**: Are there acceptance tests (LOOP*_ACCEPTANCE.md) for all shipped loops? Do the tests match the acceptance criteria?

### Test Isolation
Per project conventions, integration tests MUST use `setupTestEnv()` or `setupTestEnvWithRepo()` which enforce:
1. **TMUX_TMPDIR** — isolated tmux server socket
2. **TMUX=""** — unsets inherited tmux variable
3. **SOL_SESSION_COMMAND="sleep 300"** — prevents spawning real claude processes

Check that:
- All tests that create tmux sessions use the helpers
- No test directly calls `tmux new-session` without isolation
- No test hardcodes `"claude --dangerously-skip-permissions"` (should use `config.SessionCommand()`)
- The one documented exception (TestWorldDeleteRefusesWithActiveSessions) is still the only exception

### Test Quality
- **Flaky patterns**: Tests that depend on timing (sleep, poll loops), filesystem ordering, or port availability
- **Cleanup**: Do tests clean up after themselves? Any tests that leave behind tmux sessions, worktrees, or database files?
- **Assertions**: Are tests asserting the right things? Any tests that pass trivially (no meaningful assertions)?
- **Hermetic**: Do tests depend on external state (network, specific binaries, environment variables) that might not be present in CI?

### Acceptance Test Documents
- Read each LOOP*_ACCEPTANCE.md
- Compare acceptance criteria against actual test implementations
- Flag any acceptance criteria that have no corresponding test

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Missing isolation (tests that could kill real sessions or spawn real claude processes), acceptance criteria with no tests
- **MEDIUM**: Coverage gaps for important commands or failure modes, flaky patterns, missing cleanup
- **LOW**: Minor test quality issues, redundant tests, unclear test names

Each finding must include:
1. One-line summary
2. File path and line range (or the missing test file/function name)
3. **The actual code** — quote the specific lines that demonstrate the issue (for isolation/quality findings). For coverage gaps, describe the untested code path with file and function references.
4. Concrete risk (what could go wrong because this isn't tested, or what breaks because isolation is missing)
5. Suggested fix approach

## Constraints

**DO NOT modify any test files.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT write new tests.** Document what's missing and move on.

**Include the code.** For test isolation or quality findings, quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Be specific about coverage gaps.** "More tests needed" is not a finding. "sol caravan launch has no integration test — a regression in phase-gate checking would go undetected" is a finding.

**Verify claims against code.** Before claiming a command is untested, search for it in the test files.
