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
- **MEDIUM findings**: Is there actual user impact, incorrect output, or a real correctness gap? Code quality issues that affect correctness (wrong display output, incorrect documentation, API gaps) are valid MEDIUM findings.
- **LOW findings**: Is this genuinely correct as-is, or is it dead/wrong? Dead code, unused parameters, incorrect log levels, and wrong help text are valid LOW findings. Small fixes get batched in the commission step; do not reject findings because the fix is small.

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

**Challenge whether findings are real, not whether they're impactful enough.** Your job is to verify that the code actually does what the finding claims. A finding is confirmed if the code is genuinely wrong, dead, misleading, or inconsistent. A finding is rejected only if the code is actually correct or the behavior is intentionally designed that way (with evidence: ADR, code comment, commit message).

Do NOT reject findings because:
- The fix is small (small fixes get batched in commission)
- No production caller triggers it today (dead code and API gaps are still wrong)
- The impact is "only" cosmetic (wrong display output is wrong output)
- It's "only" documentation (factually wrong documentation misleads operators)
- It's "only" dead code (dead code is the point of a review)
- A guard test catches divergence (the duplication is the bug; the guard test is a band-aid)
- The pattern "works correctly" at one call site (check for inconsistency across the file)

These are non-negotiable. If you find yourself writing a rejection that says "below finding threshold," "code hygiene, not a defect," "cosmetic," or "no functional impact" for dead code, vestigial wrappers, incorrect output, or inconsistent patterns, you are violating these rules. Confirm the finding at LOW severity and let the commission step batch it.

### Additional confirmation criteria

The following are confirmed findings regardless of impact:
- **Dead code**: unreachable branches, guards that can never fire, unused parameters. Always confirmed. No exceptions.
- **Vestigial wrappers**: deprecated functions that delegate to a canonical implementation while callers still use the deprecated name. The wrapper and its callers are both findings.
- **Code duplication with divergence evidence**: two implementations of the same logic where (a) a prior commit fixed a divergence bug between them, or (b) a guard test exists specifically to catch future divergence, or (c) the two implementations already use different error handling, return types, or validation. The duplication itself is the finding.
- **Intra-file pattern inconsistency**: the same file uses two different patterns for the same operation (e.g., `defer cancel()` at 5 call sites, explicit `cancel()` at 2). Evaluate the pattern across the file, not each call site in isolation.
- **Incorrect output**: code that produces a different value than what is correct (wrong timestamp, extra character, stale data). "The consumer can work around it" or "other signals compensate" does not make the output correct.

DO reject findings when:
- The code is actually correct and the finding misunderstands it
- The behavior is an intentional design choice with documented rationale
- The finding's code quotes don't match the actual source (stale finding)
- The described scenario is structurally impossible (not just unlikely)

**Explain rejections thoroughly.** Every rejection at this stage overturns work from both an analysis agent and a verification agent. Your reasoning must be clear enough that a human reviewer can evaluate your judgment.

**Generate baseline candidates.** Every false positive rejected here is an opportunity to improve future scans. Don't skip this step.
