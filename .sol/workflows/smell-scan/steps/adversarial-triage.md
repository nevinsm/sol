# Adversarial Triage: The Earns-Its-Keep Gate

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first. This is the final
quality gate before synthesis and commission. Your stance is the inverse of the
bug scan's adversarial triage: there, the devil's advocate defends suspect code
against a bug claim. Here, **you assume every surviving finding is churn until it
proves it reduces a returning maintainer's orientation or reasoning cost.**

A smell scan that ships churn is a net negative: it generates merge conflicts,
review load, and risk while moving the codebase sideways. Your job is to make
sure every finding that reaches commission is one the autarch would thank you for
in six months, not one that just rewrote working code to a preference.

## Source

Read `verified.md` and `baseline-candidates.json` from:

- `verify-structure-1`
- `verify-structure-2`
- `verify-lenses`

Collect all findings marked **Kept**.

## Process

### 1. Churn Challenge

For each finding, argue that it is *not worth doing*:

- Is the current code a defensible deliberate trade-off? (Pragmatism over purity
  is a project value. Composition has limits. Three similar lines beat a wrong
  abstraction.)
- Does the proposed fix add a concept, indirection, or abstraction the
  maintainer must then learn? If the cost removed does not clearly exceed the
  cost added, this is churn. Reject.
- Is the "better" target actually better, or just different / more common?
- Would a competent maintainer returning in six months even notice this, or
  would they only notice if told to look?

If you cannot defeat the churn argument with a concrete orientation cost, reject
the finding.

### 2. Severity Reality Check

Re-rate against DOCTRINE. S1 requires active misunderstanding (a maintainer
believes something false). Be strict: most findings are S2 (time tax) or S3
(friction). Inflated severities distort commission priority. A lying comment or
a silently-diverging source of truth is S1; a long function is S2; a stuttering
name is S3.

### 3. Deduplication and Collision

Findings arrive from area steps and from lenses; the same smell often appears in
both (a god function found by `structure-supervision` and by
`lens-rejected-patterns` as monolith creep). Merge them. Keep the framing with
the strongest evidence and broadest accurate scope. Note contributing steps.

### 4. Promote Systemic Patterns

Look across kept findings for the systemic shape: many point findings that share
one root (e.g. "JSON output done four ways" reported as four area findings is one
consistency pattern). Mark these for the synthesis step. A systemic finding that
one convention or one refactor resolves is the highest-value output of this scan;
point fixes that could be a single structural change should be flagged as such,
not shipped as N writs.

### 5. Baseline Candidates

Consolidate `baseline-candidates.json` from the verify steps and add any
false-smells you reject here. Two kinds:

- **SE-{n}** (smell exempt) — looks like a smell, is actually justified; will be
  re-flagged every scan unless baselined.
- **AS-{n}** (accepted smell) — real smell the operator may choose to defer;
  propose only, the operator decides.

Use the next free numbers (check `baseline.json` for the current max). Schema in
`SMELL-BASELINE.md`.

## Output

Write `adversarial-triage.md`:

### Confirmed Findings

Per finding: DOCTRINE schema, final severity, contributing step(s), and the
churn argument you considered and why a concrete orientation cost defeats it.

### Rejected Findings

Per finding: summary, source, and why it is churn (the trade-off that holds, the
abstraction the fix would add, or the merely-different target).

### Systemic Patterns (for synthesis)

Per pattern: name, the point findings it subsumes, the single structural change
or convention that would resolve it, and the reasoning cost it currently imposes.

### Baseline Candidates

Write the consolidated array to `baseline-candidates.json` as well.

### Statistics

Findings received, confirmed, rejected (by reason), merged, systemic patterns
identified, baseline candidates, confirmation rate.

## Constraints

Do not modify source code. Do not create writs or a caravan. Reject by argued
churn, not by vibe. Every rejection states the trade-off or added cost that
makes the change not worth it. The bar is high on purpose: fifteen findings the
autarch acts on beats a hundred they have to wade through.
