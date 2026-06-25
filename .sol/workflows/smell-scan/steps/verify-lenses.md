# Verify: Cross-Cutting Lens Findings

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first, then
`steps/verify-structure-1.md` for the verification process. Same method, lens
sources, plus two checks specific to cross-cutting findings.

## Source

Read `review.md` from the output of:

- `lens-principles`
- `lens-conventions`
- `lens-rejected-patterns`
- `lens-consistency`
- `lens-orientation`

## Process

Apply the four anti-churn gates from DOCTRINE exactly as in
`verify-structure-1.md`, plus:

1. **Verify the claimed breadth.** Lens findings assert systemic patterns ("this
   appears in N places", "this principle is violated across these packages").
   Spot-check the cited sites: open at least a sample and confirm they are real
   instances of the claimed pattern, not superficially-similar code lumped
   together. A systemic finding whose instances do not actually share a root
   cause is rejected or narrowed to the real instances.

2. **Check the ground-truth citation.** Principle/convention findings must cite
   the actual rule in `principles.md` / a `CONVENTIONS.md` / an ADR. Confirm the
   cited rule says what the finding claims, and confirm the documented *scope*
   does not already exempt the code (e.g. ZFC explicitly exempts reconstructible
   caches; a flagged ledger/broker cache is a baseline `SE` exemption, not a
   finding). Reject findings that misread the rule or hit an exemption.

The gates bite hardest here: architectural and consistency findings are where
"would be cleaner" churn hides. A coupling or consistency finding survives only
with evidence of concrete reasoning cost and a convergent, complexity-reducing
target. Pragmatism over purity is a project value; a defensible pragmatic
trade-off is not a finding.

## Output

Same as `verify-structure-1.md`: `verified.md` (Kept / Rejected per the DOCTRINE
schema, breadth re-stated for kept systemic findings) plus
`baseline-candidates.json`.

## Constraints

Do not modify source code. Do not create writs or a caravan. Every rejection
names the failed gate or the misread rule.
