# Synthesis

Read all leg analysis outputs and produce a deduplicated fix caravan.

## Process

1. Read every `review.md` from every leg output directory
2. Deduplicate — the same issue may appear in multiple legs (e.g., a shared utility bug found by the core-infra and session-lifecycle reviewers, or a CLI doc gap found by both the cli and documentation legs)
3. Group related findings into coherent writs (e.g., "Fix 3 exit code violations in status commands" rather than 3 separate writs). Use judgment — group things that a single agent can fix in one coherent change, but don't create mega-writs that touch 15 files across unrelated packages.
4. Assign priority:
   - **P1 (HIGH)**: Bugs with concrete failure scenarios, data loss risks, broken agent behavior
   - **P2 (MEDIUM)**: Silent errors, missing validation, test coverage gaps, stale documentation
   - **P3 (LOW)**: Dead code, convention violations, cosmetic issues
5. Set writ kind appropriately:
   - `code` for writs that require source code changes
   - `analysis` for writs that require further investigation before a fix can be determined
6. Create fix writs with enough context for an autonomous agent to execute:
   - What's wrong (file paths, line numbers, the exact issue)
   - Why it matters (the concrete failure scenario or impact)
   - What the fix should look like (approach, not exact code)
   - Acceptance criteria (how to verify the fix is correct)
7. Group all writs into a caravan in drydock, sequenced by priority (P1 first)

## Constraints

**DO NOT modify any source code.** This is an analysis writ. Your deliverables are writs and a caravan.

**DO NOT mark anything as "already fixed."** Leg agents are read-only — they cannot have fixed anything. If a leg review says it fixed something, treat that finding as unfixed and create a writ for it.

**Every finding must be dispositioned.** Either it becomes a writ, or you document why you dropped it:
- **Dropped: false positive** — the claimed issue doesn't actually exist (explain why)
- **Dropped: too trivial** — the fix is not worth the dispatch overhead
- **Dropped: not actionable** — the finding describes a concern but not a specific fixable issue

Write your disposition log to `synthesis.md` in your output directory.

**Validate findings before creating writs.** Leg agents can make mistakes. Before creating a writ for a finding, spot-check the claim against the actual code. A few minutes of verification prevents dispatching agents to fix non-issues.

## Output

- `synthesis.md` — disposition log showing every finding from every leg: what became a writ, what was dropped, and why
- `caravan.md` — summary of the created caravan: name, ID, writ count by priority, and the full writ list with titles and IDs
