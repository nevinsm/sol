# Scope-Creep Detection

## Task Context

{{target.description}}

---

Compare stated objectives against proposed work. Identify gold-plating, feature additions beyond requirements, and unnecessary complexity.

Focus: Does any proposed work exceed what was actually requested or needed?

**Look for:**
- Steps that are not necessary for the stated goal or MVP
- Gold-plating: doing it "properly" when "good enough" would ship faster
- Refactors bundled in that are not required for the feature to work
- Future-proofing that adds complexity without delivering current benefit
- Steps solving problems not mentioned in the original request
- Premature abstraction or generalization beyond what's needed now
- "While we're in there" cleanup that is not blocking the feature
- Polishing steps for internal-only or low-traffic code paths
- Testing coverage disproportionate to the risk of the code being tested
- Documentation for things that do not need external-facing documentation
- Infrastructure upgrades bundled in that could be separate work items
- Steps that only make sense if a future feature is built (speculative work)

**Questions to answer:**
- What is the minimum set of steps for a working, shippable result?
- What in this plan is for future requirements, not current ones?
- Which steps could be filed as follow-up work items without blocking launch?
- What would you cut if the timeline were halved?

**Output format:**
```
## Verdict
PASS / PASS WITH NOTES / FAIL — one sentence rationale

## Must Fix (blocks implementation)
- Issue: <description>
- Why it matters: <impact>
- Suggested fix: <what to cut or defer>

## Should Fix (important but not blocking)
- ...

## Observations (non-blocking notes)
- ...
```
