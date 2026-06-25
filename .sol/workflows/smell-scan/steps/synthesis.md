# Systemic Synthesis

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first. The verify and triage
steps confirmed individual smells. Your job is to step back and see the *shape*:
the systemic orientation hazards that no single finding captures, and the
small number of structural moves that would most reduce the cost of returning to
this codebase. This is the step that turns a list of smells into a strategy.

## Inputs

Read from the adversarial triage output:

- `adversarial-triage.md` — Confirmed Findings and the Systemic Patterns section.

## What to Produce

### 1. The orientation map

In a few paragraphs, answer the scan's actual question: **if someone returned to
this codebase after six months, where would they get lost, and why?** Synthesize
the confirmed findings into the 3 to 6 themes that dominate orientation cost.
Name them concretely (e.g. "supervision recovery logic is spread across three
packages with no single entry point", "JSON output has four idioms", "the
CLAUDE.md component index has drifted from the runtime layer"). Rank by how much
each theme taxes a returning maintainer.

### 2. Systemic findings (collapse points to patterns)

For each systemic pattern: state the single structural change or convention that
resolves the whole class, the point findings it subsumes, and why the systemic
fix is strictly better than N point fixes (fewer concepts, one place to change,
removes the drift risk). These become convention-establishing or
refactor-the-pattern writs in commission, which are the highest-leverage output.

### 3. Sequencing intelligence

Identify dependencies and conflicts among the prospective fixes so commission can
phase them:

- Which fixes touch the same files and must be sequenced, not parallelized.
- Which systemic fix should land *before* point fixes in the same area (e.g.
  establish the JSON-output convention before cleaning up individual commands, so
  the cleanups target the new standard).
- Which fixes are independent and safely parallel.

### 4. What to leave alone

Explicitly list themes you considered and are *not* recommending action on,
with the reason (defensible trade-off, churn risk exceeds benefit, blocked on a
decision the operator must make). A smell scan earns trust by what it declines to
churn as much as by what it flags.

## Output

Write `synthesis.md`:

- **Orientation map** — the ranked themes and the returning-maintainer narrative.
- **Systemic findings** — each with subsumed point findings, the single fix, and
  the leverage argument.
- **Standalone findings** — confirmed findings that are genuinely individual
  (not part of a systemic pattern), carried forward for commission with their
  DOCTRINE schema intact.
- **Sequencing notes** — conflict/dependency/ordering intelligence for phasing.
- **Deliberately not actioned** — themes declined, with reasons.

## Constraints

Do not modify source code. Do not create writs or a caravan (commission does
that). Every confirmed finding from triage must appear in `synthesis.md` exactly
once: folded into a systemic finding, carried as a standalone, or explicitly
declined with a reason. Nothing disappears silently.
