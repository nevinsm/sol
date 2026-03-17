# Integration Tests Review

Review the integration test suite for coverage gaps, isolation correctness, and test quality.

This is NOT a code review of the test implementations for bugs — it's an assessment of whether the test suite adequately validates the system's behavior and whether tests are correctly isolated.

## What to look for

### Coverage Gaps
- **Untested commands**: Cross-reference cmd/ against test/integration/ — which commands have no integration test coverage?
- **Untested failure modes**: Read docs/failure-modes.md. Are the documented crash recovery paths tested? Which failure modes have no test?
- **Untested workflows**: Are workflow types (workflow, convoy, formula) tested end-to-end?
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
3. Concrete risk (what could go wrong because this isn't tested, or what breaks because isolation is missing)
4. Suggested fix approach

## Constraints

**DO NOT modify any test files.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT write new tests.** Document what's missing and move on.

**Be specific about coverage gaps.** "More tests needed" is not a finding. "sol caravan launch has no integration test — a regression in phase-gate checking would go undetected" is a finding.

**Verify claims against code.** Before claiming a command is untested, search for it in the test files.
