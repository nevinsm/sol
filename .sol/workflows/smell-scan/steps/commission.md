# Commission Refactor Caravan

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first. Turn the synthesized
findings into well-scoped writs and a drydock caravan. This is the only mutating
step in the scan: it creates writs and a caravan. It still creates no code.

Smell writs are different from bug writs in one critical way: they change working
code to reduce future cost. A botched refactor trades a maintainability smell for
a correctness bug, which is a bad trade. So every code writ here must center on
**behavior preservation** alongside the orientation improvement.

## Inputs

- `synthesis.md` from the synthesis step — orientation map, systemic findings,
  standalone findings, sequencing notes, declined themes.
- `baseline-candidates.json` from adversarial triage.

## Process

### 1. Gather and disposition

Every finding in `synthesis.md` (systemic and standalone) must become a writ or
be explicitly skipped. Declined themes from synthesis are already dispositioned;
record them but do not re-litigate.

### 2. Cross-reference prior caravan (if provided)

`{{prior_caravan}}` is a prior scan caravan ID. If non-empty, run
`sol caravan status {{prior_caravan}}` and skip findings already addressed by a
merged writ there. Document each skip.

### 3. Group into writs

- **Systemic findings become one writ each** ("Establish convention X and
  converge the N sites", "Split god package Y along these two responsibilities").
  These are the highest-value writs; do not shatter them into point fixes.
- **Group standalone findings by fix location and theme.** Findings touching the
  same files belong together.
- **Batch S3 friction** into cleanup writs (8 to 12 small independent fixes per
  writ is normal). Never dispatch an S3 finding alone.
- **Do not over-group.** A writ one agent cannot hold coherently in one session
  is too big. Refactor writs especially: keep the blast radius reviewable.
- **Keep refactor and cleanup separate from doc writs.** Orientation-surface
  fixes (CLAUDE.md index, ADR status, comments, package docs) are low-risk and
  belong in their own writ(s), not mixed with code refactors.

### 4. Assign priority

Smells map to priority by orientation cost. There is no P0 (a smell is not a live
defect; if it were, it would be a bug-scan finding):

- **P1** — S1 reasoning hazards: things that make a maintainer believe something
  false (lying comments/names, silently-diverging sources of truth, misleading
  signatures). Fix the misunderstanding first.
- **P2** — S2 orientation tax: god functions/packages, unearned abstraction,
  systemic inconsistency, documented-convention/principle drift.
- **P3** — S3 friction: small dead code, minor naming, small doc gaps. Batched.

### 5. Sequence into phases

Use the synthesis sequencing notes:

- **Phase 0** — P1 writs and independent systemic writs with no file conflicts.
- **Phase 1** — writs that depend on a phase-0 systemic fix landing first
  (e.g. point cleanups that should target a newly-established convention), plus
  conflict partners of phase-0 writs.
- **Phase 2** — remaining P2/P3 and batched cleanup writs, plus later conflict
  partners.

When a systemic convention writ and the cleanups that adopt it both exist,
the convention writ goes in an earlier phase so cleanups target the new standard.
Two writs touching the same file must not share a phase; add a dependency.

### 6. Re-verify quotes before writing descriptions

You are several layers removed from the source. For every writ that quotes code,
open the file at the cited lines and use the CURRENT code, not the code copied
through the triage chain. Check `git log` if it diverges. Stale quotes send
builders chasing code that does not exist, the top cause of wasted cycles.

### 7. Write writs

Each writ description, self-contained (builders lack scan context):

- **What and where** — file paths, line ranges, current code quoted, the smell
  and its category.
- **Why it matters** — the concrete orientation/reasoning cost (from DOCTRINE
  field 5). Builders need to know this is a deliberate maintainability change.
- **The fix shape** — direction, not exact code. For systemic writs, state the
  target convention and the sites to converge.
- **Behavior preservation (code writs)** — state explicitly that this is a
  refactor/cleanup that must not change behavior. Require that existing tests
  pass unchanged; if behavior would change, that is out of scope and a separate
  concern.
- **Acceptance criteria** — both the orientation improvement (the smell is gone)
  and behavior preservation (`make test` passes; no test was loosened to make it
  pass).
- **Scope boundaries** — "ONLY modify X", "Do NOT touch Y". Refactors that wander
  create conflicts and risk.
- **Kind** — `code` for source/test/refactor changes; `docs` or `code` as
  appropriate for orientation-surface writs (doc-only changes that live in the
  repo are still `code` if they must land via forge; analysis only if further
  investigation is needed).

Create with `sol writ create --world=<world> --title="..." --description="..."
--kind=code` (priority via `--priority=` where 1=high, 2=normal, 3=low).

### 8. Create caravan in drydock

`sol caravan create "<name>" <writ-id>...` then add phases with
`sol caravan add --phase=<n>` (integer). Leave it in drydock; the operator
reviews and commissions. Do not commission.

### 9. Baseline candidates

Copy the consolidated `baseline-candidates.json` into `synthesis-commission.md`
under a "Baseline Candidates" heading for operator review. Do NOT edit
`baseline.json` directly; the operator promotes approved `SE`/`AS` entries.

## Output

### `caravan.md`

Caravan name and ID, writ count by priority, phase breakdown with writ list,
file-conflict pairs with sequencing rationale, and the full writ table (ID,
title, priority, phase, category, files touched).

### `synthesis-commission.md`

The disposition log. Every finding from synthesis dispositioned as: became writ,
grouped into writ, batched into cleanup writ, skipped (prior caravan), or skipped
(declined in synthesis, with reason). Plus the baseline candidates section.

## Constraints

Do not modify source code. Leave the caravan in drydock. Writ descriptions must
be self-contained, must quote current code, and must require behavior
preservation for every code change. A smell fix that risks a behavior change
without saying so is worse than the smell.
