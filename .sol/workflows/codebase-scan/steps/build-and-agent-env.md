# Build System and Agent Environment Review

Review build configuration, embedded workflows, agent skills, and protocol prompts for correctness and consistency.

This leg spans the "meta" layer — the things that configure, build, and instruct the system rather than implement its logic.

## Focus

Review:
- `Makefile`
- `go.mod`
- `main.go`
- `tools.go`
- Embedded defaults in `internal/` subdirectories

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

### Build System
- **Makefile**: Are all targets correct? Do they use the right flags? Are there targets that reference removed packages or binaries?
- **go.mod / go.sum**: Any deprecated or vulnerable dependencies? Unused dependencies that should be cleaned up?
- **Build tags**: Any build constraints that might exclude code on certain platforms?

### Embedded Workflows (internal/workflow/defaults/)
- **Manifest validity**: Are all embedded workflow manifests syntactically valid TOML? Do they parse correctly?
- **Step file references**: Do instruction file paths in manifests actually point to existing files?
- **Description accuracy**: Do workflow descriptions accurately describe what the workflow does?
- **Consistency**: Are similar patterns used consistently across workflows (field naming, structure)?

### Agent Skills (.claude/skills/)
- **CLI accuracy**: For every sol command referenced in a skill file, verify the command exists with the documented flags and behavior. Skills that reference nonexistent commands or wrong flags will cause agent errors.
- **Completeness**: Do skills cover all the operations an envoy needs? Any gaps?
- **Consistency**: Are skill files structured consistently (YAML frontmatter format, section organization)?
- **Triggers**: Are the "when to use" and "when not to use" descriptions accurate and helpful?

### Protocol Prompts (internal/protocol/prompts/)
- **Persona accuracy**: Do agent personas accurately describe the agent's capabilities and available commands?
- **Command references**: Every sol command or flag mentioned in a prompt — does it exist? Is the syntax correct?
- **Behavioral instructions**: Do the behavioral instructions match how the system actually works? (e.g., if a prompt says "run sol resolve to submit work," does sol resolve actually do what the prompt implies?)
- **Role boundaries**: Do prompts correctly scope each agent role? (Outpost vs envoy vs governor)

### Configuration Defaults (internal/config/defaults/)
- **Default values**: Are config defaults sensible? Any defaults that are known to cause problems?
- **Session command template**: Is the session startup template correct?
- **Statusline script**: Does the statusline script work correctly?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Skills or prompts that reference nonexistent commands (agents will fail), broken embedded workflows, build system that produces incorrect binaries
- **MEDIUM**: Stale workflow descriptions, incomplete skill coverage, inconsistent skill formatting, outdated persona descriptions
- **LOW**: Minor Makefile improvements, dependency cleanup, cosmetic inconsistencies

Each finding must include:
1. One-line summary
2. File path and line range
3. **The actual code** — quote the specific lines that demonstrate the issue (e.g., the skill text referencing a nonexistent flag, the Makefile target with the wrong path)
4. Concrete impact (what breaks or confuses because of this issue)
5. Suggested fix approach

## Constraints

**DO NOT modify any files.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Include the code.** Every finding must quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Cross-reference aggressively.** This leg is specifically about consistency between the meta-layer and the implementation. Every command reference in a skill or prompt should be verified against cmd/. Every workflow manifest should be validated against the workflow parser's expectations.

**Verify claims against code.** Read the actual implementation before writing a finding.
