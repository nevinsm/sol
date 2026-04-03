# Batch Verify 5 — Tests and Build

Verify findings from the **integration-tests**, **documentation**, and **build-and-agent-env** analysis steps.

This is a verification step, not a new analysis. Your job is to confirm or reject each finding by re-reading the cited source code and checking whether the issue actually exists as described.

## Source Steps

- `integration-tests` — Integration tests (test/integration/)
- `documentation` — Documentation (docs/)
- `build-and-agent-env` — Build system and agent environment (Makefile, go.mod, embedded workflows, skills, prompts, defaults)

## Process

Read the `review.md` output from each source step. For every finding reported:

### 1. Re-read the Cited Code

Open the exact file and line range cited in the finding. Read the surrounding context (at least 20 lines before and after) to understand the full picture.

### 2. Compare Quotes

If the finding includes a code quote, compare it character-by-character against the actual source. Look for:
- Lines that were omitted from the quote (hiding context that contradicts the claim)
- Subtle misquotes that change the meaning
- Quotes from an old version of the code that has since been changed

### 3. Verify the Failure Scenario

Each finding should describe a concrete failure scenario. Trace through the code to determine:
- Can the described sequence of events actually occur?
- Are there guards, checks, or other code paths that prevent the scenario?
- Does the failure have the severity claimed? (e.g., "data corruption" vs. "returns wrong error message")

### 4. Check Git History

Run `git log --oneline -10 -- <file>` for each cited file. Check whether:
- The issue was recently fixed (finding is stale)
- The pattern was intentionally introduced (commit message explains why)
- Related code was recently refactored (finding may be based on old structure)

### 5. Disposition

Assign each finding one of:
- **Verified** — The issue exists as described. The code quote is accurate, the failure scenario is plausible, and the severity is appropriate.
- **Verified (severity adjusted)** — The issue exists but the severity should be changed. State the new severity and why.
- **Rejected: misquoted code** — The code quote doesn't match the source. Show the actual code.
- **Rejected: guards exist** — The failure scenario can't occur because of guards the analysis missed. Cite the guard code.
- **Rejected: already fixed** — The issue was fixed in a recent commit. Cite the commit hash.
- **Rejected: incorrect analysis** — The analysis misunderstands the code's behavior. Explain the actual behavior.
- **Rejected: not reproducible** — The described failure scenario requires conditions that cannot occur in practice. Explain why.

## Baseline Check

Before verifying, read `baseline.json` from the workflow directory. If a finding matches a baseline entry (same file, function, and pattern), mark it as **Suppressed (baselined)** and skip verification. Note the baseline ID.

## Output

Write to `batch-verify-5.md` in your output directory.

### Verified Findings
For each verified finding:
- Source step
- One-line summary
- File path and line range
- Severity (original or adjusted)
- Verification notes — what you confirmed and how

### Rejected Findings
For each rejected finding:
- Source step
- One-line summary
- Rejection reason with evidence (actual code quotes, commit hashes, guard logic)

### Suppressed Findings
For each baselined finding:
- Source step
- One-line summary
- Baseline entry ID

### Statistics
- Findings received per source step
- Verified / Rejected / Suppressed counts

## Constraints

**DO NOT modify any source code.** This is a read-only verification.

**DO NOT skip verification.** Every finding must be independently checked. "The analysis agent probably got this right" is not verification.

**Show your work.** For each disposition, include the evidence that led to your conclusion.
