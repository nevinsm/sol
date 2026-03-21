# CLI Commands Review

Review all command definitions in **Focus** for correctness, consistency, and adherence to CLI conventions.

## What to look for

### Command Structure
- **Cobra setup**: Are commands correctly registered? Parent-child relationships correct? Any orphaned commands?
- **Flag definitions**: Required flags marked as required? Flag types correct? Any flags that shadow parent flags?
- **Argument validation**: Are positional arguments validated? What happens with wrong number of args?

### Exit Code Conventions
Per project conventions:
- Exit 0: success
- Exit 1: failure, "not found", or "not running"
- Exit 2: context-specific (blocked by guard, degraded status)

Check that:
- Commands used for scripting (status checks, health probes) document exit codes in their `Long` field
- Exit codes are consistent with the convention — no arbitrary exit codes
- Error paths actually exit non-zero (not just print an error and exit 0)

### Confirmation Pattern
Per project conventions:
- `--confirm` for destructive operations (dry-run without it)
- `--force` for behavioral escalation (e.g., stop sessions before delete), NOT confirmation bypass

Check that:
- Destructive commands require `--confirm`
- Without `--confirm`, commands preview what would happen and exit 1
- `--force` is not used as a synonym for `--confirm`

### Help Text
- **Short/Long descriptions**: Are they accurate? Do they match actual behavior?
- **Examples**: Are provided examples correct and runnable?
- **Flag descriptions**: Are they clear and complete?

### Error Handling
- **User-facing errors**: Are error messages clear and actionable? Do they include context?
- **Silent failures**: Any commands that fail but print nothing?
- **Cleanup on error**: If a multi-step command fails mid-way, is partial state cleaned up?

### CLI Documentation
- **docs/cli.md sync**: Are all commands documented in docs/cli.md? Any new commands missing from docs?
- Cross-reference every command in cmd/ against docs/cli.md entries

### Consistency
- **Naming**: Are similar operations named consistently across commands? (list/status/show patterns)
- **Output format**: Is output consistent across commands? (table formatting, JSON output options)
- **Flag naming**: Similar flags named the same across commands? (--world, --agent, etc.)

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Commands that silently fail, incorrect exit codes on scripting commands, missing confirmation on destructive operations, commands that can corrupt state
- **MEDIUM**: Missing docs/cli.md entries, inconsistent flag naming, unclear error messages, missing argument validation
- **LOW**: Help text typos, minor inconsistencies, cosmetic issues

Each finding must include:
1. One-line summary
2. File path and line range
3. **The actual code** — quote the specific lines that demonstrate the issue
4. Concrete impact (what a user would experience)
5. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Include the code.** Every finding must quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Cross-reference docs/cli.md** against actual command implementations. Every command should be documented; every documented command should exist.

**Verify claims against code.** Read the actual implementation before writing a finding.
