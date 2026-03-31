# Risk Assessment

## Task Context

{{target.description}}

---

Identify technical, operational, and integration risks. Evaluate likelihood and impact. Flag single points of failure, untested assumptions, and rollback gaps.

Focus: What can go wrong and how severe would the impact be?

**Look for:**
- Steps with high uncertainty or novel technology nobody on the team has used
- External dependencies (third-party APIs, services) with reliability or availability risk
- Steps that will be hard to test or verify in a non-production environment
- Changes to shared infrastructure or libraries affecting other consumers
- Database migrations that are difficult or impossible to reverse
- Breaking changes to existing integrations, APIs, or CLI contracts
- Performance risks with no load testing or benchmarking step
- Security surface area expansion without a corresponding security review step
- Vague or hand-wavy descriptions ("we'll figure it out", "TBD")
- Single points of failure in the implementation sequence
- Steps with estimates that seem unrealistic given their described complexity
- Missing contingency plans for the highest-risk steps

**Questions to answer:**
- What is the highest-risk step, and does the plan de-risk it early?
- What assumption in this plan is most likely to be wrong?
- What is the recovery plan if step N fails mid-implementation?
- What external factor could block progress entirely?

**Output format:**
```
## Verdict
PASS / PASS WITH NOTES / FAIL — one sentence rationale

## Must Fix (blocks implementation)
- Issue: <description>
- Why it matters: <impact>
- Suggested fix: <what mitigation or de-risk step to add>

## Should Fix (important but not blocking)
- ...

## Observations (non-blocking notes)
- ...
```
