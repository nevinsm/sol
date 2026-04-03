# Forge Review

Review the forge package for correctness in merge ordering, quality gate enforcement, crash recovery, and retry behavior.

The forge is the merge pipeline — it processes merge requests deterministically, one at a time. It's the critical path for all code reaching the target branch. Bugs here can corrupt the repository, lose work, or create infinite retry loops.

## Focus

Read all `.go` files in these packages:
- `internal/forge/`

## Process

1. **Read every file in the Focus packages end-to-end** before looking for issues. Understand the code as written, not as you imagine it.
2. As you read, note anything that looks wrong. Only record findings where you can point to specific lines you just read.
3. After reading all files, check your notes against the categories in "What to look for" below.
4. Before reporting a finding, check `.sol/workflows/codebase-scan/baseline.json` (if it exists). If the file and function are listed and your finding matches the reviewed pattern, do not report it. See `.sol/workflows/codebase-scan/BASELINE.md` for matching rules.
5. For each potential finding, **verify before reporting**:
   - Copy the ACTUAL code from the file into your finding. Do not paraphrase, summarize, or reconstruct from memory.
   - Confirm the issue exists in the code you just read, not in a hypothetical version of it.
   - Run `git log --oneline -5 -- <file>` for each cited file. If the file was modified in the last 2 weeks, check whether recent commits already addressed this issue. If so, do not report it.
   - Construct the concrete sequence of events that triggers the bug. If you cannot trace a real call path that reaches the faulty code, the finding is theoretical and should not be reported.

A finding with fabricated or approximate code quotes is worse than no finding. It wastes triage time and downstream agent cycles. When in doubt, leave it out.

## What to look for

### Merge Ordering and Queue
- **Queue processing**: Is the queue processed in the correct order (FIFO by claim time)? Can reordering cause conflicts?
- **Claim/release**: Is the claim lifecycle correct? Can an MR be claimed by two patrols simultaneously?
- **Phase transitions**: Are MR phase transitions (ready → claimed → merged/failed) atomic and correct? Any invalid transitions?

### Quality Gates
- **Gate execution**: Are build/test/lint gates applied correctly? Can a gate bypass occur?
- **Gate results**: Are gate pass/fail results correctly interpreted? Any inversions?
- **Session-based assessment**: Is the forge session launched correctly? Result file parsing?

### Crash Recovery
- **Mid-merge crash**: If forge dies mid-merge, what state is left? Is the branch safe? Is the queue consistent?
- **Worktree state**: Is the forge worktree left clean after each merge? What about after a crash?
- **Partial state**: Can a crash leave an MR in claimed state with no active session? Is this recovered?

### Retry and Failure
- **Retry bounds**: Are failed merges retried with appropriate limits? Any infinite retry loops?
- **Permanent failure**: Is permanent failure (max attempts) handled correctly? Exit codes?
- **Conflict handling**: When merge conflicts are detected, is the resolution task created correctly? Error paths?

### Git Operations
- **Merge mechanics**: Is the actual git merge/rebase correct? Edge cases with empty commits, submodules, or large files?
- **Branch management**: Are branches created and cleaned up correctly? Orphaned branches?
- **Push safety**: Is force-push prevented? Is the push target always correct?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Bugs that cause lost work (merge corruption, incorrect conflict detection, push to wrong branch), infinite retry loops, bypass of quality gates
- **MEDIUM**: Missing retry bounds, cleanup gaps, crash recovery edge cases, incorrect phase transitions
- **LOW**: Dead code, convention violations, logging gaps

Each finding must include:
1. One-line summary
2. File path and line range
3. **The actual code** — quote the specific lines that demonstrate the issue
4. Concrete failure scenario
5. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Include the code.** Every finding must quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Be specific.** Name the function, the line, the exact failure sequence.

**Verify claims against code.** Read the actual implementation before writing a finding.
