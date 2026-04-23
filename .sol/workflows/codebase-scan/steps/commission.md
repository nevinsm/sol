# Commission Fix Caravan

Read all validated findings and cross-domain issues, group them into coherent writs, and create a fix caravan in drydock.

## Inputs

1. **`adversarial-triage.md`** from the adversarial triage step — Section 1 (confirmed findings)
2. **`cross-domain.md`** from the cross-domain review step — additional boundary findings

## Process

### 1. Gather all confirmed findings

Combine:
- Confirmed findings from adversarial triage (Section 1)
- Cross-domain findings from cross-domain review
- Systemic pattern findings (if the cross-domain review confirmed them as systemic)

### 2. Cross-reference prior caravan (if provided)

{{prior_caravan}} is the caravan ID from a previous scan run. If non-empty:
- Run `sol caravan status {{prior_caravan}}` to see all writs from the prior scan
- For each finding, check if it was already addressed by a prior writ:
  - Compare file paths and issue descriptions
  - If the prior writ is merged, the issue is likely fixed — **skip it**
  - If the prior writ failed or was dropped, the issue may still exist — **keep it**
- Document all skip decisions in the disposition log

### 3. Group findings into writs

Use judgment to group related findings into coherent writs:
- **Group by fix location**: findings that require changes to the same file(s) belong together
- **Group by theme**: "Fix 3 error-swallowing bugs in forge" is better than 3 separate writs
- **Don't over-group**: a writ that touches 15 files across unrelated packages is too big. Keep writs focused enough for one agent to complete in one session.
- **Systemic patterns**: if the cross-domain review found a systemic issue, create one writ for the fix (e.g., "Add shared error-wrapping helper") rather than N writs per instance

### 4. Assign priority

- **P0 (CRITICAL)**: Bugs with concrete data loss, corruption, or security scenarios
- **P1 (HIGH)**: Bugs with concrete failure scenarios, broken agent behavior, silent error discard on write paths
- **P2 (MEDIUM)**: Missing validation, test coverage gaps for critical paths, incorrect documentation, display correctness bugs, edge case handling
- **P3 (LOW)**: Dead code cleanup, unused parameters/fields, incorrect log levels, wrong help text, missing test coverage for non-critical paths

P3 findings are still valid writs. They get batched (multiple small fixes per writ) rather than skipped. "Small" is a sizing concern, not a rejection criterion.

### 5. Identify file conflicts

If two writs touch the same file, they cannot be dispatched in parallel — they'll create merge conflicts. Identify these pairs and sequence them:
- First writ in an earlier phase
- Second writ in the next phase with a dependency on the first
- Document the conflict in both writ descriptions so the second agent knows to pull the first's changes

### 5b. Re-verify code quotes before writing writ descriptions

You are multiple layers removed from the source code. Before writing any writ description that includes a code quote:

1. Read the actual source file at the cited lines
2. Use the CURRENT code in the writ description, not the code from adversarial-triage.md
3. If the source doesn't match what the triage chain reported, investigate:
   - Was the file modified between triage and commission? (check `git log`)
   - Did context degrade through the triage layers?
   - If the issue no longer exists, skip it and document in the disposition log

Writ descriptions with stale or incorrect code quotes send builders looking for code that doesn't exist. This is the single most common cause of wasted agent cycles.

### 6. Create writs

For each writ, include enough context for an autonomous agent to execute without guessing:
- **What's wrong**: file paths, line numbers, the exact issues (quote the code)
- **Why it matters**: concrete failure scenario or impact
- **What the fix should look like**: approach, not exact code
- **Acceptance criteria**: how to verify the fix is correct
- **Scope boundaries**: "ONLY modify X" and "Do NOT touch Y"
- **Kind**: `code` for source changes, `analysis` for further investigation

Create writs using `sol writ create`:
```
sol writ create --world=<world> --title="..." --description="..." --kind=code
```

### 7. Create caravan in drydock

Group all writs into a caravan:
```
sol caravan create "<descriptive name>" <writ-id-1> <writ-id-2> ...
```

Set phases based on priority and file conflicts:
- Phase 0: P0/P1 writs with no file conflicts
- Phase 1: P1/P2 writs, including conflict partners of phase 0
- Phase 2: P2/P3 writs, including conflict partners of phase 1

### 8. Baseline update

Copy `baseline-candidates.json` from the adversarial triage output into your disposition log (`synthesis.md`) under a "Baseline Candidates" heading. These are proposed baseline entries for the operator to review. Do NOT modify `baseline.json` directly. The operator adds approved entries after reviewing the scan results.

## Output

### `synthesis.md` — Disposition Log

Every finding from adversarial triage and cross-domain review must be dispositioned. The ONLY valid dispositions are:
- **Became writ**: finding → writ ID and title
- **Grouped into writ**: finding merged with related findings → writ ID
- **Batched into cleanup writ**: small fix grouped with other small fixes into a batch writ
- **Skipped: prior caravan**: already addressed by prior writ (cite the writ)
- **Skipped: not actionable**: root cause is in a generator, external tool, or requires new feature work that exceeds scan-fix scope (explain why)

These are the ONLY valid dispositions. There is no "Skipped: too trivial," "Skipped: cosmetic," or "Skipped: below threshold." If a finding survived adversarial triage, it is confirmed as real and MUST result in a writ (standalone, grouped, or batched). The triage step already filtered out false positives. Your job is to turn confirmed findings into well-scoped writs, not to second-guess triage decisions.

Small findings (dead code, wrong log levels, unused parameters, incorrect help text, stale comments, vestigial wrappers, incorrect output values) are batched into cleanup writs. A cleanup writ with 8-12 small independent fixes across different files is a normal and expected output of the commission step.

### `caravan.md` — Caravan Summary

- Caravan name and ID
- Writ count by priority (P0/P1/P2/P3)
- Phase breakdown with writ list
- File conflict pairs with sequencing rationale
- Full writ list: ID, title, priority, phase, files touched

## Constraints

**DO NOT modify any source code.** Your deliverables are writs, a caravan, and disposition logs.

**Every finding must be dispositioned.** No finding should silently disappear between adversarial triage and commission.

**Writ descriptions must be self-contained.** Builder agents don't have our scan context. Include file paths, line numbers, code quotes, and concrete acceptance criteria in every writ.

**Scope writs explicitly.** Include "ONLY modify X" and "Do NOT touch Y" boundaries. Builder agents that wander outside their scope create merge conflicts and wasted work.
