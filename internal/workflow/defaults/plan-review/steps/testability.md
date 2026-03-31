# Testability Review

## Task Context

{{target.description}}

---

Assess whether each deliverable can be verified. Check for measurable acceptance criteria, test strategies, and observable outcomes.

Focus: How will we know each step succeeded?

**Look for:**
- Steps with no associated test plan or verification strategy
- Features that are hard to test in isolation from the rest of the system
- End-to-end flows with no integration test coverage planned
- No definition of what "passing" looks like for each step
- Missing smoke tests or validation steps post-deployment
- Metrics or observability not planned (how will we know it works in production?)
- Manual verification steps that should be automated
- Tests that can only run in production and not in dev or staging
- No regression test plan for existing behavior that might be affected
- Acceptance criteria that are subjective or unmeasurable ("fast", "clean", "good UX")
- Missing edge-case coverage for error paths, empty states, and boundary conditions
- No plan for how to verify rollback works if needed

**Questions to answer:**
- After all steps are closed, how do we know the feature works end-to-end?
- What is the first signal that something went wrong in production?
- Which steps have acceptance criteria that are verifiable by automation?
- What requires manual testing, and is that testing planned?

**Output format:**
```
## Verdict
PASS / PASS WITH NOTES / FAIL — one sentence rationale

## Must Fix (blocks implementation)
- Issue: <description>
- Why it matters: <impact>
- Suggested fix: <what test or verification to add>

## Should Fix (important but not blocking)
- ...

## Observations (non-blocking notes)
- ...
```
