# Go/No-Go Recommendation

Consolidate findings from all five analysis dimensions into a single go/no-go recommendation.

**Your task:**
1. Read all five leg outputs (completeness, sequencing, risk, scope-creep, testability)
2. Extract each leg's verdict (PASS / PASS WITH NOTES / FAIL) and key findings
3. Aggregate per-leg verdicts into a summary table
4. Deduplicate cross-leg findings — issues flagged by multiple legs are higher-confidence problems
5. Determine overall verdict: GO / GO WITH FIXES / NO-GO
   - GO: All legs PASS or PASS WITH NOTES, no must-fix items
   - GO WITH FIXES: No FAIL verdicts, but must-fix items exist that can be addressed without replanning
   - NO-GO: Any leg FAILs, or must-fix items require fundamental replanning
6. List all must-fix items with which legs flagged them

**Output format:**
```
## Overall Verdict
**GO / GO WITH FIXES / NO-GO** — one paragraph rationale

## Leg Verdicts
| Dimension | Verdict | Key Finding |
|-----------|---------|-------------|
| Completeness | PASS/PASS WITH NOTES/FAIL | <one-line summary> |
| Sequencing | PASS/PASS WITH NOTES/FAIL | <one-line summary> |
| Risk | PASS/PASS WITH NOTES/FAIL | <one-line summary> |
| Scope Discipline | PASS/PASS WITH NOTES/FAIL | <one-line summary> |
| Testability | PASS/PASS WITH NOTES/FAIL | <one-line summary> |

## Cross-Leg Findings (flagged by 2+ dimensions)
These are higher-confidence issues because multiple independent reviewers identified them.
- <finding>: flagged by <leg1>, <leg2>

## Must Fix Before Proceeding
(Blocking issues — plan needs revision before implementation starts)

### [Issue Title]
- **Found by**: <which legs>
- **Problem**: <what is wrong>
- **Required fix**: <what needs to change in the plan>

## Should Fix
(Important but not blocking — can be addressed during implementation)
- ...

## Observations
(Non-blocking notes worth considering)
- ...

## Next Steps
- [ ] Address must-fix items in plan (if any)
- [ ] Re-review if NO-GO verdict
- [ ] Proceed with implementation if GO or GO WITH FIXES (after fixes applied)
```
