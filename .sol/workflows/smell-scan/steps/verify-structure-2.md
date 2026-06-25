# Verify: Observability, CLI, Test Structural Findings

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first, then read
`steps/verify-structure-1.md` for the full verification process. This step is
identical in method; only the source set differs.

## Source

Read `review.md` from the output of:

- `structure-observability`
- `structure-cli`
- `structure-tests`

## Process

Identical to `verify-structure-1.md`: re-read cited code (check git recency),
apply the four anti-churn gates, confirm smell-not-bug, right-size S1/S2/S3.

Test-specific note: a working test rewritten to a preferred style fails gate 2
(no net complexity reduction) and gate 4 (unstable target, just different). Keep
test findings only where the test actively misleads about coverage, defeats a
mandated isolation rule, or duplicates fixtures in a way that has already drifted
or plainly will.

## Output

Same as `verify-structure-1.md`: `verified.md` with Kept and Rejected sections
per the DOCTRINE schema, plus `baseline-candidates.json` for recurring
false-smells (id `SE-{n}`).

## Constraints

Do not modify source code. Do not create writs or a caravan. Every rejection
names which of the four gates failed.
