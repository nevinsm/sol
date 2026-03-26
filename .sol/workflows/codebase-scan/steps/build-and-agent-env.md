# Build System and Agent Environment Review

Review build configuration, embedded workflows, agent skills, and protocol prompts for correctness and consistency.

This leg spans the "meta" layer — the things that configure, build, and instruct the system rather than implement its logic.

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
