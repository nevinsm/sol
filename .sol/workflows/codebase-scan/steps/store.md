# Store Layer Review

Review the store package for correctness in SQLite access, migration safety, and concurrent access patterns.

## Focus

Read all `.go` files in these packages:
- `internal/store/`

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

### SQLite Correctness
- **Connection setup**: WAL mode set on every connection? `busy_timeout` and `foreign_keys` pragmas? Are these set consistently across all connection paths (main store, world store, backup)?
- **Prepared statements**: Are statements closed properly? Any leaked statements on error paths?
- **Transaction scope**: Are transactions held too long? Any risk of blocking concurrent writers?
- **NULL handling**: Are SQL NULL values handled correctly in Go? Missing `sql.NullString` where needed?

### Migration Safety
- **Sequential ordering**: Are migrations numbered sequentially with no gaps?
- **Idempotency**: Can migrations be re-run safely? Any destructive operations (DROP without backup, data deletion)?
- **Schema changes**: Do ALTER TABLE statements handle the case where the column already exists (idempotent)?
- **Data migrations**: Are data migrations correct? Any risk of data loss during migration?

### Concurrency
- **Race conditions**: Multiple goroutines accessing the store simultaneously — are access patterns safe?
- **Lock contention**: With WAL mode, readers shouldn't block writers. But are there patterns that defeat WAL (long-running transactions, multiple connections without WAL)?
- **Deadlock potential**: Are multiple locks acquired in consistent order?

### Error Propagation
- **Context in errors**: Are database errors wrapped with context (`fmt.Errorf("... %q: %w", ...)`)? Can you trace errors back to their source?
- **Silent error discard**: Any places where database errors are ignored or logged-and-discarded?
- **Partial failures**: Multi-step database operations — what happens if step 2 of 3 fails?

### Backup and Recovery
- **Backup correctness**: Does backup produce a valid, usable database? Is the backup atomic?
- **Restore path**: Can a backed-up database be restored and function correctly?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Data corruption risks, race conditions that can lose data, migration bugs, silent error discard on write paths
- **MEDIUM**: Missing error context, leaked statements, suboptimal transaction scope, backup edge cases
- **LOW**: Dead code, convention violations, minor inconsistencies

Each finding must include:
1. One-line summary
2. File path and line range
3. **The actual code** — quote the specific lines that demonstrate the issue
4. Concrete failure scenario (not hypothetical — describe the sequence of events that triggers the bug)
5. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Even trivial fixes — document them and move on.

**Include the code.** Every finding must quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Be specific.** "Error handling could be improved" is not a finding. "OpenWorld returns nil error when world.toml is missing but directory exists (config.go:87), causing downstream nil-pointer panic in LoadAgents" is a finding.

**Be honest about severity.** A doc comment typo is LOW. A race condition that corrupts the database is HIGH. If you're unsure, err toward the lower severity.

**Verify claims against code.** Before writing a finding, confirm the issue exists by reading the actual code. False positives waste everyone's time.
