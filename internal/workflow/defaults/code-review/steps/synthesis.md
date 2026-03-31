# Review Synthesis

Combine all leg findings into a unified, prioritized review.

Read all 10 analysis leg findings from the writ output directories. Each leg has
written its findings as a prioritized list with severity levels.

**Your task:**
Produce a single consolidated review that synthesizes all findings.

**Structure your output as:**

1. **Executive Summary** — Overall assessment in 2-3 sentences. Is this ready to merge?
   State a clear recommendation: merge as-is, merge with minor fixes, requires changes, or needs rework.

2. **Critical Issues (P0)** — Must fix before merge. Collected from all legs.
   Deduplicate: if multiple legs found the same issue, note which legs flagged it.
   Include file:line references.

3. **Major Issues (P1)** — Should fix before merge. Grouped by theme.
   Deduplicate across legs. Note consensus (flagged by 3+ legs = high confidence).

4. **Minor Issues (P2)** — Nice to fix. Brief list, don't belabor.

5. **Wiring Gaps** — From the wiring leg: dependencies added but not used,
   old implementations that should have been replaced, dead config.

6. **Commit Quality** — From the commit-discipline leg: are commits atomic,
   well-messaged, and following conventional commit format?

7. **Test Quality** — From the test-quality leg: are tests meaningful,
   are negative cases covered, any flaky indicators?

8. **Positive Observations** — What's done well. Good patterns, clear code,
   thorough tests. Be specific — this matters for morale and learning.

9. **Recommendations** — Actionable next steps, prioritized by impact and effort.
   Separate into "before merge" and "follow-up" categories.

**Deduplication rules:**
- Same issue found by multiple legs → list once, note all legs that found it
- Similar issues → group together, note the different angles each leg brought
- Conflicting assessments → present both views, recommend resolution

**Prioritization:**
- Impact × likelihood = priority
- Security and correctness issues outrank style issues
- Wiring gaps often indicate incomplete work — flag prominently
