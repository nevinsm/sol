# Cross-Domain Review

Investigate the integration boundaries between packages — the seams that no single analysis step could see.

Individual analysis steps review packages in isolation. They catch bugs within a package. But many of the most important bugs live *between* packages: incorrect assumptions one package makes about another's behavior, error contracts that aren't honored across boundaries, state invariants that hold within a package but break when composed.

## Inputs

Read `adversarial-triage.md` from the adversarial triage step's output directory. Focus on:
- **Section 3: Cross-Cutting Patterns** — these are systemic issues flagged for your investigation
- **Section 1: Confirmed Findings** — scan for findings that touch integration points

## What to Investigate

### 1. Error Contract Violations

Trace error paths across package boundaries. When package A calls package B:
- Does A handle all of B's documented error returns?
- Does A rely on B's error wrapping format? (e.g., does A string-match B's error messages?)
- If B changes its error behavior, does A break silently?

Key boundaries to check:
- dispatch → tether → store (writ lifecycle)
- forge → git → session (merge pipeline)
- prefect → sentinel/consul/forge (supervision)
- startup → protocol → skills → config (agent initialization)
- envoy/governor → session → brief (agent lifecycle)

### 2. State Assumptions Across Packages

When package A reads state that package B writes:
- Does A assume the state is always present? What if B hasn't written it yet?
- Does A assume the state is in a specific format or value range?
- Can B's state transitions leave A in an inconsistent view?

Key state boundaries:
- Tether files: written by dispatch, read by startup, cleaned by consul
- Agent records: written by dispatch, read by prefect/sentinel, updated by envoy/governor
- MR records: written by forge, read by patrol, updated by session results
- Heartbeat files: written by services, read by prefect

### 3. Concurrency at Integration Points

When two packages operate on shared state concurrently:
- Do they use the same locking mechanism? (file locks, database locks, advisory locks)
- Is there a lock ordering convention? Can deadlock occur?
- Are there TOCTOU windows at package boundaries?

### 4. Cross-Domain Patterns from Adversarial Triage

For each pattern flagged in adversarial triage's Section 3:
- Investigate the specific packages cited
- Determine if the pattern is systemic or coincidental
- If systemic: identify the root cause (missing abstraction? undocumented contract? copy-paste?)
- If coincidental: note this so the commission step doesn't over-group

## Output

Write to `cross-domain.md` in your output directory:

For each finding:
1. One-line summary
2. **Packages involved** — which packages interact at this boundary
3. **The call chain** — trace the code path across package boundaries (function A calls function B which calls function C)
4. **The actual code** — quote the specific lines on both sides of the boundary
5. Severity (HIGH / MEDIUM / LOW)
6. Concrete failure scenario — the sequence of events that triggers the bug
7. Suggested fix approach

For each adversarial-triage-flagged pattern investigated:
1. Pattern name (matching adversarial triage's label)
2. Verdict: systemic or coincidental
3. If systemic: root cause and recommended fix approach
4. If coincidental: note for commission step to handle individually

## Constraints

**DO NOT modify any source code.** This is a read-only analysis.

**DO NOT create writs or a caravan.** That happens in the commission step.

**Focus on boundaries, not interiors.** The analysis steps already reviewed each package internally. Your value is seeing what happens when packages interact. Don't re-review code that a step already covered unless the issue is specifically about cross-package behavior.

**Include the code.** Every finding must quote the specific lines from both sides of the boundary. If you can't show both sides, the finding isn't about a cross-domain issue.

**Verify claims against code.** Read the actual implementation before writing a finding.
