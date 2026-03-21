# Triage and Validate Findings

Read all analysis outputs from every step and produce a validated, deduplicated findings list.

This is the quality gate. Every finding from every analysis step passes through here. Your job is to separate real issues from false positives, eliminate duplicates, and identify cross-cutting patterns that no single step could see.

## Process

1. **Read every `review.md`** from every analysis step's output directory.

2. **Validate each finding against actual code.** Analysis agents make mistakes. For every finding:
   - Read the file and lines cited
   - Confirm the issue actually exists as described
   - If the finding includes quoted code, verify the quote matches the current source
   - If the finding does NOT include quoted code, read the cited location yourself and determine if the issue is real

3. **Disposition each finding:**
   - **Confirmed** — issue exists as described, code quote matches
   - **Confirmed (modified)** — issue exists but severity or description needs adjustment. State what you changed and why.
   - **Rejected: false positive** — the claimed issue doesn't actually exist. Explain why (quote the actual code that contradicts the finding).
   - **Rejected: already fixed** — the issue was fixed in a recent commit. Cite the commit.
   - **Rejected: not actionable** — the finding describes a concern but not a specific fixable issue.
   - **Rejected: too trivial** — the fix is not worth the dispatch overhead.

4. **Deduplicate.** The same issue may appear in multiple step reviews (e.g., a shared utility bug found by both the store and session-lifecycle reviewers). When you find duplicates:
   - Keep the version with the best description and most accurate severity
   - Note which steps reported it
   - Prefer the step that covers the package where the fix would be made

5. **Identify cross-cutting patterns.** Look for systemic issues:
   - "Three steps found error-swallowing in different packages" — is there a codebase-wide pattern?
   - "Multiple steps found missing cleanup on error paths" — is there a shared helper that should exist?
   - "The same incorrect assumption appears in forge, sentinel, and consul" — is there a shared contract that's undocumented?

   Flag these for the cross-domain review step. Be specific: name the packages, the pattern, and why it matters.

## Output

Write to `triage.md` in your output directory:

### Section 1: Validated Findings
For each confirmed finding:
- Original step that reported it
- One-line summary
- File path and line range
- Severity (HIGH / MEDIUM / LOW) — adjusted if needed
- The quoted code demonstrating the issue
- Concrete failure scenario

### Section 2: Rejected Findings
For each rejected finding:
- Original step that reported it
- One-line summary
- Rejection reason with evidence

### Section 3: Cross-Domain Concerns
For each identified pattern:
- Pattern name (e.g., "Systemic error-swallowing")
- Which steps reported related findings
- Which packages are involved
- Why this needs cross-domain investigation (what a single-step reviewer couldn't see)

### Section 4: Statistics
- Total findings received (by step)
- Confirmed / Rejected breakdown
- Rejection reasons breakdown (false positive, already fixed, not actionable, too trivial)

## Constraints

**DO NOT modify any source code.** This is a read-only analysis.

**DO NOT create writs or a caravan.** That happens in the commission step.

**DO NOT skip validation.** Every finding must be dispositioned. "Looks plausible" is not confirmation — read the actual code.

**Be honest about rejection rates.** A high false-positive rate from a step is valuable information. Don't inflate confirmation counts to be kind to the analysis agents.
