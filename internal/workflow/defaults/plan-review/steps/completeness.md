# Completeness Analysis

## Task Context

{{target.description}}

---

Assess whether the plan covers all required elements: goals, deliverables, acceptance criteria, resource needs, and timeline.

Focus: Are there gaps in what the plan promises to deliver versus what it actually specifies?

**Look for:**
- Requirements from the original request with no corresponding plan step
- Implied work that isn't explicitly planned (migrations, tests, docs, rollout)
- Missing infrastructure or setup steps assumed to "just exist"
- No monitoring or alerting plan for production changes
- No rollback plan for risky or irreversible changes
- Missing error handling or graceful degradation steps
- Tests not mentioned as explicit deliverables
- Post-launch validation or smoke-test steps not planned
- Clean-up, deprecation, or feature-flag removal work not included
- Configuration or environment setup assumed but not planned
- Documentation updates not accounted for (CLI help, READMEs, ADRs)
- Missing coordination steps (notifications, hand-offs, reviews)

**Questions to answer:**
- Which stated requirement has no plan step covering it?
- What will be obviously missing when the last step is closed?
- What will the implementer ask "wasn't this supposed to be part of this?" about?
- If an outsider read only this plan, could they execute it without guessing?

**Output format:**
```
## Verdict
PASS / PASS WITH NOTES / FAIL — one sentence rationale

## Must Fix (blocks implementation)
- Issue: <description>
- Why it matters: <impact>
- Suggested fix: <what to add or change in the plan>

## Should Fix (important but not blocking)
- ...

## Observations (non-blocking notes)
- ...
```
