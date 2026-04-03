# Orchestration and Presentation Review

Review the packages listed in **Focus** for correctness in workflow execution, world operations, status rendering, and output generation.

## Focus

Read all `.go` files in these packages:
- `internal/workflow/`
- `internal/worldexport/`
- `internal/worldsync/`
- `internal/status/`
- `internal/dash/`
- `internal/style/`
- `internal/docgen/`

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

### Workflow (internal/workflow/)
- **Manifest parsing**: Are workflow manifests parsed correctly? Mode field (inline/manifest)? Edge cases with optional fields?
- **Three-tier resolution (ADR-0021)**: Does project → user → embedded fallback work correctly? Any tier that gets skipped?
- **Variable substitution**: Are template variables resolved correctly? What about undefined variables — error or silent empty string?
- **Step execution**: Are dependencies respected? Can a step execute before its dependencies complete?
- **State tracking**: Is workflow state (state.json) updated atomically? Can concurrent access corrupt it?
- **Embedded workflows**: Are embedded manifests (internal/workflow/defaults/) correctly loaded via embed.FS?

### World Export (internal/worldexport/)
- **Export completeness**: Does export capture all world state? Any components whose state is missed?
- **Import correctness**: Can an exported world be imported and function correctly?
- **Path handling**: Are paths correctly relativized during export and absolutized during import?

### World Sync (internal/worldsync/)
- **Sync correctness**: Does sync correctly update the managed repo from upstream?
- **Conflict handling**: What happens when the managed repo has local changes? Are they preserved or lost?
- **Worktree impact**: Does sync affect existing worktrees? It shouldn't — but verify.

### Status (internal/status/)
- **Data accuracy**: Does the status display accurately reflect actual system state? Any stale data?
- **Component representation**: Are all components represented? Per CLAUDE.md — "new components must have status representation."
- **Edge cases**: What does status show when components are down, missing, or in unusual states?

### Dash (internal/dash/)
- **Dashboard rendering**: Is the dashboard rendering correct and responsive?
- **Data freshness**: Is dashboard data refreshed appropriately?

### Style (internal/style/)
- **Lipgloss usage**: Are styles defined consistently? Any hardcoded ANSI codes instead of lipgloss?
- **Terminal compatibility**: Do styles degrade gracefully on terminals without color support?

### Docgen (internal/docgen/)
- **Generation accuracy**: Does generated documentation match actual CLI commands?
- **Completeness**: Are all commands covered?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Workflow state corruption, incorrect dependency resolution, sync that loses data, export that drops state
- **MEDIUM**: Stale status data, missing component representation, variable substitution edge cases
- **LOW**: Style inconsistencies, minor rendering issues, dead code

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
