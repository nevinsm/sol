# Sequencing & Dependency Analysis

## Task Context

{{target.description}}

---

Evaluate the ordering of steps and identify dependency relationships. Flag implicit dependencies, missing prerequisites, and parallelism opportunities.

Focus: Can the steps execute in the proposed order without blocking or hidden prerequisites?

**Look for:**
- Steps that depend on artifacts not yet built at that point in the sequence
- Schema migrations that must happen before code changes that use them
- API changes needing coordination between producer and consumer
- Steps that could be parallelized but are listed sequentially
- Steps listed as parallel that actually have a hidden dependency
- Missing blocking relationships between steps or deliverables
- Infrastructure that must exist before application code can run
- Feature flag or configuration requirements not sequenced before the feature itself
- Database seed data or initial configuration needed early but planned late
- Test steps that depend on deployment steps not yet complete
- External approvals or reviews that gate subsequent work
- Circular dependencies hiding in the step graph

**Questions to answer:**
- Can every step start immediately when its stated dependencies are done?
- Is there a circular or mutual dependency hiding somewhere?
- What would cause a "we can't proceed" moment mid-implementation?
- Which steps could run in parallel to speed up delivery?

**Output format:**
```
## Verdict
PASS / PASS WITH NOTES / FAIL — one sentence rationale

## Must Fix (blocks implementation)
- Issue: <description>
- Why it matters: <impact>
- Suggested fix: <what to reorder or add>

## Should Fix (important but not blocking)
- ...

## Observations (non-blocking notes)
- ...
```
