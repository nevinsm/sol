# Smell-Scan Doctrine

Read this file in full before doing any smell-scan step. It defines the shared
philosophy, severity rubric, smell taxonomy, finding schema, and the anti-churn
discipline that every analysis, verification, and triage step depends on. The
per-step instruction files only describe *which* lens or area you own. The rules
for *how* to judge a smell live here.

## The North Star

A smell scan optimizes for one thing: **the cost of a maintainer returning to
this codebase after six months and needing to orient and reason about it.**

Two costs make that hard:

1. **Reasoning hazard** — the code makes a returning maintainer reason
   *wrongly*. A comment that lies, a name that misleads, two sources of truth
   that silently disagree. These cause active misunderstanding. They are the
   most expensive smells because the maintainer does not know they are wrong.

2. **Orientation tax** — the code makes a returning maintainer reason
   *slowly*. A 400-line function, a package with five responsibilities, the same
   operation done four different ways, an abstraction that adds indirection
   without payoff. These do not mislead, they just cost time on every read.

Every finding must trace back to one of these. If a "smell" does not make
someone misunderstand or waste time, it is not a finding. It is an opinion.

## This Is Not the Bug Scan

The codebase-scan (a sibling workflow) finds defects: things that are *wrong*.
Its triage fights false-negatives. Its rule is "do not reject a finding because
the fix is small."

The smell scan finds *carrying cost*: things that are *correct but expensive to
live with*. Its triage fights **churn**. Its rule is the inverse: **do not
accept a finding because the fix would be marginally nicer.**

If you find an actual bug (wrong output, data loss, a race, a nil deref), it
belongs to the bug scan, not here. Note it in passing if you must, but do not
make it a smell finding. Smells are about maintainability, not correctness.

## The Anti-Churn Discipline

This is the most important section. A naive smell scan produces a hundred
"rename this variable" and "extract this helper" findings. That output is worse
than nothing: it generates churn, merge conflicts, and review fatigue while
moving the codebase sideways. The sol manifesto is explicit that complexity must
earn its keep and that abstractions should not be added beyond what the work
requires. A smell scan that violates those principles in the name of "cleanup"
is self-defeating.

Every finding must pass all four gates. If it fails any one, it is not a finding.

1. **Named cost.** You can state, concretely, what a returning maintainer
   misunderstands or wastes time on. Not "this could be cleaner" but "a reader
   must hold six unrelated responsibilities in their head to follow this
   function." Vague aesthetic discomfort is not a named cost.

2. **Net complexity reduction.** The fix removes more complexity than it adds.
   Proposing a new abstraction, indirection layer, or helper to eliminate a
   small duplication usually *fails* this gate: premature abstraction is itself
   a smell (category OVERBUILD). Three similar lines are cheaper to read than a
   wrong abstraction. If your fix introduces a new concept the maintainer must
   learn, the cost it removes must exceed the cost of that new concept.

3. **Not a defensible choice.** The current form is not a deliberate,
   reasonable trade-off. Much code that looks "off" is locally justified.
   Pragmatism over purity is a stated project value. If a senior developer would
   defend the current form for a good reason, it is not a smell. (Triage will
   test this adversarially; pre-empt it.)

4. **Stable target.** The fix converges on a clearly better state, not merely a
   different one. "Use pattern A everywhere" is stable only if A is genuinely
   better, not just more common. Rewriting working code to match a preference is
   churn.

When in doubt, leave it out. A returning maintainer is better served by fifteen
high-signal findings than by a hundred that bury them.

## Severity

Smell severity measures *orientation/reasoning cost*, not defect impact. Use the
S-scale, never the bug scan's HIGH/MEDIUM/LOW, so the two scans never blur.

- **S1 — Reasoning hazard.** Causes active misunderstanding. A returning
  maintainer will believe something false: a lying comment, a misleading name, a
  function whose behavior contradicts its signature, two sources of truth that
  can silently diverge, hidden coupling where editing A breaks distant B with no
  visible link. These are the smells worth interrupting other work for.

- **S2 — Orientation tax.** Costs significant time on every read. God
  functions/files/packages, deep nesting, unearned abstraction, the same
  operation implemented several ways, documented-convention drift, dead code
  large enough that a reader studies it before realizing it is unreachable.

- **S3 — Friction.** Small, repeated cost. Stuttering names, minor idiom
  inconsistency, a missing package doc comment, a slightly stale comment, small
  dead snippets. Real but low. These get batched, never dispatched alone.

There is no S0. A smell is by definition not a live defect. If something is so
severe it is causing failures, it is a bug and belongs to the bug scan.

## Smell Taxonomy

Tag every finding with exactly one category. This makes findings dedup-able
across steps and maps cleanly to the lenses.

- **MISLEAD** — name, comment, signature, or doc that causes active
  misunderstanding. (Usually S1.)
- **DIVERGE** — multiple sources of truth, or duplicated logic, that can drift
  apart silently. (Often S1.)
- **COUPLE** — hidden coupling, layering violation, a god package everyone
  imports, spooky action at a distance.
- **SPRAWL** — god function/file/package, responsibility creep, deep nesting, a
  unit that has outgrown its original job.
- **OVERBUILD** — unearned abstraction, premature generalization, indirection
  without payoff, speculative flexibility, config nobody sets. (The churn the
  manifesto warns against, found *in* the code.)
- **INCONSIST** — the same operation done multiple ways across the tree; idiom
  drift that forces a reader to learn N patterns for one concept.
- **DRIFT** — divergence from a documented principle (ZFC, GLASS, CRASH,
  DEGRADE, EVOLVE, GUPP), a `CONVENTIONS.md` rule, an ADR decision, or a stated
  convention.
- **DEADWEIGHT** — dead code, vestigial wrappers, unused params/fields/config,
  unreachable branches.
- **OPAQUE** — missing or wrong orientation surface: absent package doc comment,
  stale or missing ADR, inaccurate `CLAUDE.md` component index, comment that no
  longer matches the code, naming-glossary drift.

## Finding Schema

Write every finding with these fields, in this order. The "earns its keep" field
is mandatory and is where you discharge the anti-churn gates; a finding without a
credible one will be rejected at triage.

1. **Summary** — one line.
2. **Location** — `file:line-range`, or a package, or an explicit list of files
   for a cross-cutting finding.
3. **Severity** — S1 / S2 / S3.
4. **Category** — one taxonomy tag.
5. **Orientation cost** — what does a returning maintainer misunderstand or
   waste time on? Be concrete and specific to this code.
6. **Evidence** — the actual code (quoted, not paraphrased), or the counts
   ("this 9-line block appears verbatim in 6 files: ..."), or the divergence
   (the two versions side by side). Never reconstruct from memory.
7. **Earns its keep** — discharge the four anti-churn gates: the named cost, why
   the fix is a net complexity *reduction*, why the current form is not a
   defensible deliberate choice, and what stable target the fix converges on.
8. **Suggested direction** — the shape of the fix, not exact code. For systemic
   smells, prefer "establish convention X" over "patch N sites."

## Baseline

Before reporting, read `.sol/workflows/smell-scan/baseline.json`. It records
operator decisions that should not be re-litigated every scan:

- **SE-{n}** (smell exempt) — looks like a smell but is justified. The clearest
  example: per-component caches that ZFC explicitly exempts (`principles.md`
  documents the ledger session map and broker probe cache as legitimate). Do not
  re-flag these.
- **AS-{n}** (accepted smell) — a real smell the operator has reviewed and
  intentionally deferred.

Match on file + (functions or pattern), semantically, not by exact string. If a
finding matches a baseline entry, do not report it. Note in your output how many
findings the baseline suppressed, so triage can confirm it is not
over-suppressing. Schema and rules: `SMELL-BASELINE.md`.

## Ground Truth

Smells are judged against this project's stated standards, not generic style
guides. Read these before judging:

- `docs/principles.md` — the named principles, architectural patterns,
  operational maxims, and the explicit **Rejected Patterns** list (universal bus
  coupling, three-layer supervision, command bloat). A smell scan enforces
  these.
- `docs/manifesto.md` — design philosophy. "Stability is the feature."
  "Composition over monoliths." Complexity must earn its keep.
- `CLAUDE.md` and the `CONVENTIONS.md` files (`cmd/`, `internal/runtime/`,
  `internal/tether/`, `internal/service/`) plus `docs/conventions/*.md`. These
  are the documented norms; drift from them is a DRIFT finding.
- Relevant ADRs in `docs/decisions/` when a finding touches an architectural
  decision.

## Output and Constraints

- Write findings to `review.md` in your writ output directory, grouped by
  severity (S1, then S2, then S3).
- **Read before you judge.** Read the files in your scope end to end and
  understand them as written before looking for smells. A finding must point to
  code you actually read.
- **Do not modify any source code.** Every smell-scan step is read-only. The
  only mutating step is commission, which creates writs and a drydock caravan.
- **Do not fix anything**, however trivial. Document and move on.
- **Check git recency.** Run `git log --oneline -5 -- <file>` for cited files.
  If an area was just refactored, a "smell" may be mid-transition or already
  addressed; weigh that before reporting.
- **Prefer systemic over point findings.** Ten instances of one pattern is one
  finding ("this idiom drift appears in N places"), not ten. Synthesis and
  commission turn systemic findings into a single convention-establishing writ,
  which is far more valuable than N point patches.
