# Adversarial Triage

Final quality gate for the codebase scan. All verified findings from the batch-verify steps pass through here for adversarial challenge before entering the cross-domain review and commission pipeline.

Your role is the devil's advocate. Assume every finding is wrong until you convince yourself otherwise. The goal is zero false positives reaching the commission step.

## Source Steps

Read the output from all batch-verify steps:
- `batch-verify-1` — Data layer (store, config-and-setup, session-lifecycle)
- `batch-verify-2` — Agent infrastructure (agent-roles, protocol-and-skills, forge)
- `batch-verify-3` — Monitoring (supervision, messaging, observability)
- `batch-verify-4` — CLI and status (operational, cli, orchestration)
- `batch-verify-5` — Tests and build (integration-tests, documentation, build-and-agent-env)

Collect all findings marked **Verified** or **Verified (severity adjusted)** from these steps.

## Process

### 1. Devil's Advocate Defense

For each verified finding, actively try to disprove it:
- What would a senior developer say to defend this code?
- Is there a design reason for this pattern that the analysis and verification missed?
- Could this be intentional behavior documented elsewhere (ADRs, commit messages, code comments)?
- Is the "bug" actually a deliberate trade-off?

If you can construct a reasonable defense, the finding needs stronger evidence or should be rejected.

### 2. Severity Reality Check

Challenge every severity rating:
- **HIGH findings**: Does this actually cause data loss, corruption, or security issues in production? Or is it a theoretical risk that requires an unlikely sequence of events? A HIGH finding should have a plausible production trigger, not just "if an attacker could..."
- **MEDIUM findings**: Is there actual user impact? Or is this a code quality concern dressed up as a bug?
- **LOW findings**: Is this worth the cost of a fix? Every writ dispatched has overhead. Would a code comment be more appropriate than a code change?

### 3. Deduplication

Findings that survived batch verification may still be duplicates:
- Same root cause reported through different symptoms in different steps
- Same code pattern in related packages reported separately
- A finding that is a subset of a broader finding

Merge duplicates. Keep the version with the strongest evidence and broadest scope. Note which batch-verify steps contributed.

### 4. Cross-Cutting Pattern Identification

Look across all verified findings for systemic patterns:
- Multiple instances of the same bug class across packages (e.g., "error swallowing on defer cleanup" in 4 packages)
- Shared assumptions that are wrong (e.g., "all callers assume X is non-nil but it can be nil in Y scenario")
- Missing abstractions (e.g., "5 packages each implement their own retry logic with different bugs")

Cross-cutting patterns are more valuable than individual findings because they suggest architectural improvements rather than point fixes.

### 5. Baseline Candidate Generation

For each finding you reject, determine if it should become a baseline entry to prevent the same false positive from recurring:
- Was this a plausible-looking pattern that analysis agents are likely to flag again?
- Is the code intentionally written in a way that looks suspicious but is correct?

If yes, generate a baseline candidate entry following the schema in `BASELINE.md`.

## Output

Write to `adversarial-triage.md` in your output directory.

### Confirmed Findings

For each finding that survives adversarial triage:
- One-line summary
- File path and line range
- Final severity (HIGH / MEDIUM / LOW)
- Source step(s) that reported it
- Why it survived challenge — the devil's advocate argument you considered and why it doesn't hold

### Rejected Findings

For each finding rejected at this stage:
- One-line summary
- Source batch-verify step
- Rejection reason — the defense argument that holds, or the severity deflation that makes it not worth fixing

### Cross-Cutting Patterns

For each identified pattern:
- Pattern name
- Packages and files involved
- Description of the systemic issue
- Why individual fixes are insufficient (what architectural change is needed)
- Suggested approach

### Baseline Candidates

```json
[
  {
    "id": "CS-{n}",
    "file": "path/to/file.go",
    "functions": ["FunctionName"],
    "pattern": "Description of the pattern that triggers false positive",
    "decision": "Why this is not actually a bug",
    "category": "false_positive",
    "added": "YYYY-MM-DD"
  }
]
```

Write this array to `baseline-candidates.json` in your output directory as well, for downstream tooling.

### Statistics

- Total verified findings received (by batch-verify step)
- Confirmed after adversarial triage
- Rejected at this stage (with reason breakdown)
- Duplicates merged
- Cross-cutting patterns identified
- Baseline candidates generated
- Final confirmation rate (confirmed / total received)

## Constraints

**DO NOT modify any source code.** This is a read-only analysis.

**DO NOT create writs or a caravan.** That happens in the commission step.

**Be ruthless.** A finding that "might" be an issue is not confirmed. If you cannot definitively demonstrate the bug with a concrete scenario, reject it.

**Explain rejections thoroughly.** Every rejection at this stage overturns work from both an analysis agent and a verification agent. Your reasoning must be clear enough that a human reviewer can evaluate your judgment.

**Generate baseline candidates.** Every false positive rejected here is an opportunity to improve future scans. Don't skip this step.
