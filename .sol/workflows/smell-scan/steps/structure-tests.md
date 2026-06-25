# Structural Review: Test Suite Smells

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first.

## Focus

- `test/integration/` (all files)
- `*_test.go` across `internal/` and `cmd/`
- Test helpers (`helpers_test.go`, shared fixtures, `TestMain` functions)

Test code is read and modified as often as production code, so its orientation
cost is real. But hold the same anti-churn bar: a working test rewritten to a
preferred style is churn, not a finding. Read `CLAUDE.md`'s Testing section for
the project's mandatory test-isolation rules before judging.

## Look For (test-specific smells)

- **SPRAWL** — helper files that have become grab-bags; a single test function
  asserting many unrelated behaviors; fixture setup duplicated across many files
  instead of shared.
- **DIVERGE / INCONSIST** — multiple copies of near-identical setup that can
  drift; tests gating on `if testing.Short()` inline where a shared helper
  exists; ad-hoc isolation that bypasses the mandated `setupTestEnv` /
  `isolateTmux` helpers.
- **MISLEAD** — a test whose name claims to cover X but asserts Y; a test that
  passes vacuously (asserts nothing meaningful, checks a path that never
  executes); a comment claiming coverage the test does not provide.
- **DEADWEIGHT** — skipped/quarantined tests with no path back to active;
  dead helpers; commented-out test bodies.
- **DRIFT** — raw `time.Sleep` used as a synchronization gate where a poll loop
  is the established pattern (distinguish from legitimate "time must elapse"
  sleeps, which are correct); tests that defeat isolation rules in `CLAUDE.md`.
- **OPAQUE** — a test whose intent cannot be reconstructed from reading it: no
  clue what invariant it protects.

## Scope Note

A recent caravan already addressed a specific batch of `time.Sleep` sites and
the sentinel race-overhead. Run `git log --oneline -10 -- <file>` before
reporting sleep/isolation smells so you do not re-report freshly fixed code.
Report the *systemic* test-quality pattern if it persists broadly, not
individual sites already handled.

## Out of Scope (owned elsewhere)

- Production code smells: the `structure-*` and lens steps.
- Cross-tree consistency beyond tests: `lens-consistency`.

Write findings to `review.md` per the DOCTRINE schema.
