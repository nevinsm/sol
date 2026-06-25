# Lens: Consistency and Divergence

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first. This lens finds where
the codebase does one conceptual thing several different ways, forcing a
returning maintainer to learn N patterns for one idea. The autarch's stated
value is "consistency is king." Categories: **INCONSIST**, **DIVERGE**. This is
a whole-tree sweep across package boundaries (single-area inconsistency is owned
by the `structure-*` steps; you own the cross-tree pattern).

Consistency is double-edged. The goal is to reduce the number of distinct
patterns a reader must hold, not to enforce uniformity for its own sake. A
second pattern that exists for a genuine reason is fine. Apply the anti-churn
gates: only flag inconsistency that actually taxes comprehension and converges
on a clearly-better single pattern.

## What to Look For

- **Same operation, multiple shapes.** One concept implemented several ways
  across packages: JSON output, error wrapping, world resolution, time
  formatting, pidfile/heartbeat handling, confirmation prompting, context
  timeout handling, store-open patterns. Count the variants and name the cost.

- **Divergent duplication (DIVERGE).** Two or more implementations of the same
  logic that can drift apart. This is a finding when there is real drift risk:
  a prior commit already fixed a divergence between them, a guard test exists to
  catch divergence, or the copies already differ in error handling / validation
  / return shape. The duplication is the smell; the guard test is a band-aid
  over it.

- **Idiom drift.** The same small task done with different idioms in different
  places with no reason (e.g. mixed approaches to optional values, mixed
  slice-building styles) where one idiom is clearly the house style.

## How to Judge

1. Establish which pattern is the house standard (most common AND best on the
   merits, not merely most common).
2. Count and locate the deviations.
3. Decide: is convergence a net reduction in concepts-to-learn, or would forcing
   uniformity erase a meaningful distinction? Only the former is a finding.
4. For divergent duplication, prefer "unify into one implementation" over "keep
   both in sync"; a guard test that keeps two copies aligned is itself evidence
   the copies should be one.

State the finding as: "Concept X is implemented as A (n sites), B (m sites),
C (k sites). House standard is A. B and C cost a reader an extra two mental
models for no reason. Converge on A." That single systemic finding is worth far
more than one-per-site.

## Out of Scope

- Documented conventions (the rule exists in a doc): `lens-conventions`.
- Single-package internal inconsistency: the relevant `structure-*` step.

Write findings to `review.md` per the DOCTRINE schema.
