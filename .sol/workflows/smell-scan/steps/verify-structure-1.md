# Verify: Core, Agents, Supervision Structural Findings

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first. You apply the four
anti-churn gates to findings from the structural steps and pass through only
those that earn their keep. This is the smell scan's primary churn filter: the
bug scan's verification confirms a bug triggers; yours confirms a smell is worth
the cost of changing working code.

## Source

Read `review.md` from the output of:

- `structure-core`
- `structure-agents`
- `structure-supervision`

## Process

For each finding:

1. **Re-read the cited code.** Open the file at the cited lines. Confirm the
   quoted code matches the current source (run `git log --oneline -5 -- <file>`;
   if it changed since analysis, re-evaluate against current code). A finding
   citing stale code is rejected.

2. **Apply the four gates from DOCTRINE.** Pass only if all four hold:
   - **Named cost** — there is a concrete, specific orientation/reasoning cost,
     not aesthetic discomfort.
   - **Net complexity reduction** — the suggested fix removes more complexity
     than it adds. Reject "fixes" that introduce a new abstraction to eliminate a
     small duplication (that is OVERBUILD wearing a cleanup costume).
   - **Not a defensible choice** — the current form is not a reasonable
     deliberate trade-off. Construct the senior-developer defense; if it holds,
     reject.
   - **Stable target** — the fix converges on a clearly better state, not merely
     a different one.

3. **Confirm it is a smell, not a bug.** If the finding is actually a defect
   (wrong output, race, nil deref), reject it from this scan and note it should
   go to the bug scan.

4. **Right-size severity.** Re-rate S1/S2/S3 against DOCTRINE. Reasoning hazards
   (active misunderstanding) are S1; pure time-tax is S2; small friction is S3.
   Do not inflate.

## Output

Write `verified.md` with two sections.

**Kept** (one block per surviving finding): the full DOCTRINE finding schema,
plus a one-line note on which gate was closest to failing and why it still
passed. Carry forward the source step.

**Rejected** (one block per dropped finding): summary, source step, and the
specific gate it failed (cite the defense argument, the added complexity, or the
unstable target). If the rejection is a recurring false-smell pattern analysis
agents will keep flagging, draft a baseline candidate (schema in
`SMELL-BASELINE.md`, id `SE-{n}`).

Also write the baseline candidates as a JSON array to `baseline-candidates.json`.

## Constraints

Do not modify source code. Do not create writs or a caravan. Reject by gate, not
by vibe: every rejection names which of the four gates failed. When you keep a
finding, you are asserting it is worth changing working code; hold that bar.
