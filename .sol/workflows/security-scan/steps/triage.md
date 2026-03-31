# Triage and Validate Security Findings

Read all analysis outputs from every step, validate against the codebase, cross-reference the baseline, and produce a deduplicated, prioritized findings list.

This is the quality gate. Every finding from every analysis step passes through here. Your job is to separate real vulnerabilities from false positives, eliminate duplicates, check the baseline for accepted risks, and produce a clean set of confirmed findings for the commission step.

## Process

### 1. Read All Analysis Outputs

Read every `review.md` from every analysis step's output directory:
- `gosec-input-handling/review.md`
- `gosec-code-quality/review.md`
- `secrets-scan/review.md`
- `dep-audit/review.md`

### 2. Validate Each Finding Against Actual Code

Each analysis step's `review.md` contains two sections: "Findings (for triage)" and "Filtered (appendix)".

**Findings section** — full validation required. Analysis agents make mistakes. For every finding:
- Read the file and lines cited
- Confirm the issue actually exists as described
- If the finding includes quoted code, verify the quote matches the current source
- If the finding references a gosec rule, verify the rule applies (e.g., G601 in Go 1.22+ is likely a false positive)
- For CVE findings, verify the dependency version in go.mod matches the affected version range

**Filtered appendix** — spot-check only. Validate 2-3 samples from each step's filtered list to calibrate trust in the analysis agent's judgment. If the spot-check reveals poor filtering (real issues classified as FP), escalate by validating the full filtered list for that step.

### 3. Cross-Reference Baseline

Read the baseline file at `.sol/workflows/security-scan/baseline.json`.

The baseline contains previously-triaged findings with dispositions:
- **accepted**: known issue, risk accepted — do NOT re-create a writ for this
- **deferred**: known issue, fix deferred — include in findings but note the deferral
- **false_positive**: confirmed false positive — skip without further validation

For each finding, check if it matches a baseline entry by comparing:
- File path (allow for line number drift — same file + same pattern = match)
- Rule ID or CWE
- Code pattern (the baseline may quote a code snippet for matching)

If a finding matches a baseline entry:
- **accepted**: disposition as "Baseline: accepted risk" — do not include in confirmed findings
- **deferred**: include in confirmed findings with a note that this was previously deferred
- **false_positive**: skip validation — disposition as "Baseline: confirmed false positive"

If the baseline is empty or the file doesn't exist, proceed without baseline filtering.

### 4. Disposition Each Finding

- **Confirmed** — issue exists as described, code quote matches, not in baseline
- **Confirmed (modified)** — issue exists but severity, CWE, or description needs adjustment. State what you changed and why.
- **Confirmed (deferred)** — issue exists and was previously deferred in baseline. Note the original deferral reason.
- **Rejected: false positive** — the claimed issue doesn't actually exist. Explain why.
- **Rejected: not exploitable** — the vulnerability exists in code but cannot be triggered in this application context. Explain the mitigating factors.
- **Rejected: already fixed** — the issue was fixed since the analysis ran (or the code has changed).
- **Rejected: baseline accepted** — risk was previously accepted in baseline.
- **Rejected: baseline false positive** — previously confirmed as false positive in baseline.
- **Rejected: duplicate** — same issue reported by multiple steps. Keep the best description.

### 5. Deduplicate

The same issue may appear across multiple analysis steps:
- gosec-input-handling may report overlapping findings across injection and file-ops rules on the same function
- gosec-code-quality may flag the same function for both error handling and crypto concerns
- secrets-scan patterns may overlap with gosec-code-quality crypto findings

When duplicates are found:
- Keep the version with the most accurate severity and best description
- Note which steps reported it
- Prefer the step whose security domain is the primary concern

### 6. Identify Cross-Cutting Patterns

Look for systemic security issues:
- "Multiple steps found the same class of vulnerability across different packages" — is there a missing security abstraction?
- "Error handling gaps appear in all security-sensitive code paths" — is there a systemic practice issue?
- "The same dependency vulnerability appears in multiple call paths" — single fix point or distributed?

Flag these patterns — they inform how the commission step groups findings into writs.

## Output

Write to `triage.md` in your output directory:

### Section 1: Confirmed Findings

For each confirmed finding:
- Original step that reported it
- One-line summary
- File path and line range
- Severity: CRITICAL / HIGH / MEDIUM / LOW
- CWE ID
- The quoted code demonstrating the issue
- Exploitability assessment
- Whether this was previously deferred (from baseline)

### Section 2: Rejected Findings

For each rejected finding:
- Original step that reported it
- One-line summary
- Rejection reason with evidence

### Section 3: Cross-Cutting Patterns

For each identified pattern:
- Pattern name
- Which steps and findings are related
- Security impact if the pattern is systemic
- Recommended approach (single fix vs. per-instance)

### Section 4: Baseline Recommendations

Findings from this scan that should be added to the baseline (after commission):
- New false positives confirmed during triage — recommend adding to baseline as `false_positive`
- Findings that are real but accepted risk — recommend adding as `accepted` (with justification)

### Section 5: Statistics

- Total findings received (by step and overall)
- Confirmed / Rejected breakdown
- Rejection reasons breakdown
- Baseline match count
- Severity distribution of confirmed findings

## Constraints

**DO NOT modify any source code.** This is a read-only analysis.

**DO NOT create writs or a caravan.** That happens in the commission step.

**DO NOT update the baseline.** Recommend baseline changes in Section 4; the operator decides.

**DO NOT skip validation.** Every finding must be dispositioned. Read the actual code.

**Security severity is contextual.** A CLI tool has a different threat model than a web service. Assess severity based on this project's actual deployment model (local CLI tool + local daemons, single-user).
