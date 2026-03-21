# Commission Fix Caravan

Read all validated findings and cross-domain issues, group them into coherent writs, and create a fix caravan in drydock.

## Inputs

1. **`triage.md`** from the triage step — Section 1 (validated findings)
2. **`cross-domain.md`** from the cross-domain review step — additional boundary findings

## Process

### 1. Gather all confirmed findings

Combine:
- Validated per-step findings from triage (Section 1)
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
- **P2 (MEDIUM)**: Missing validation, test coverage gaps, stale documentation, edge case handling
- **P3 (LOW)**: Dead code, convention violations, cosmetic issues

### 5. Identify file conflicts

If two writs touch the same file, they cannot be dispatched in parallel — they'll create merge conflicts. Identify these pairs and sequence them:
- First writ in an earlier phase
- Second writ in the next phase with a dependency on the first
- Document the conflict in both writ descriptions so the second agent knows to pull the first's changes

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

## Output

### `synthesis.md` — Disposition Log

Every finding from triage and cross-domain review must be dispositioned:
- **Became writ**: finding → writ ID and title
- **Grouped into writ**: finding merged with related findings → writ ID
- **Skipped: prior caravan**: already addressed by prior writ (cite the writ)
- **Skipped: too trivial**: not worth dispatch overhead
- **Skipped: not actionable**: needs more investigation (create an analysis writ if warranted)

### `caravan.md` — Caravan Summary

- Caravan name and ID
- Writ count by priority (P0/P1/P2/P3)
- Phase breakdown with writ list
- File conflict pairs with sequencing rationale
- Full writ list: ID, title, priority, phase, files touched

## Constraints

**DO NOT modify any source code.** Your deliverables are writs, a caravan, and disposition logs.

**Every finding must be dispositioned.** No finding should silently disappear between triage and commission.

**Writ descriptions must be self-contained.** Builder agents don't have our scan context. Include file paths, line numbers, code quotes, and concrete acceptance criteria in every writ.

**Scope writs explicitly.** Include "ONLY modify X" and "Do NOT touch Y" boundaries. Builder agents that wander outside their scope create merge conflicts and wasted work.
