# Agent Roles Review

Review the packages listed in **Focus** for correctness in agent lifecycle management, brief handling, and role-specific behavior.

## Focus

Read all `.go` files in these packages:
- `internal/envoy/`
- `internal/brief/`

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

### Envoy (internal/envoy/)
- **Lifecycle**: Create, delete, session start/stop — are all state transitions clean? Any dangling state after delete (worktree, tether, brief, database records)?
- **Brief injection**: Is the brief correctly loaded and injected on session start? Size limits enforced?
- **Concurrent access**: Can two operations on the same envoy race (e.g., handoff while a notification arrives)?
- **Lock acquisition**: Are agent locks acquired before state mutations? Any TOCTOU gaps?

### Brief (internal/brief/)
- **File operations**: Read/write atomicity? What happens if the brief file is corrupt or missing?
- **Size management**: Is the 200-line cap enforced? What happens when the agent writes beyond it?
- **Injection**: Is the injection hook wired correctly? Does it handle missing brief gracefully (clean start, not error)?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Bugs that break agent operations (lost state, orphaned resources, broken persona injection)
- **MEDIUM**: Edge cases in lifecycle transitions, missing cleanup, inconsistent error handling
- **LOW**: Dead code, convention violations, documentation gaps in code comments

Each finding must include:
1. One-line summary
2. File path and line range
3. **The actual code** — quote the specific lines that demonstrate the issue
4. Concrete failure scenario (not hypothetical — describe the sequence of events)
5. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Include the code.** Every finding must quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Be specific.** Name the function, the line, the exact failure sequence.

**Verify claims against code.** Read the actual implementation before writing a finding.
