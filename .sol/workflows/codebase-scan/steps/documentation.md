# Documentation Review

Review all documentation in **Focus** for accuracy, completeness, and consistency with the actual codebase.

Documentation that's wrong is worse than no documentation — it actively misleads. The goal of this review is to find documentation that has drifted from the implementation.

## Focus

Read all files in:
- `docs/`

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

### CLI Documentation (docs/cli.md)
- **Completeness**: Cross-reference every command in cmd/ against docs/cli.md. Flag any command that's missing from the docs, and any documented command that no longer exists.
- **Flag accuracy**: For each documented command, verify that flags, arguments, and descriptions match the actual cobra command definition.
- **Exit code documentation**: Commands used for scripting should document exit codes. Are they documented correctly?
- **Examples**: Are the documented examples still correct?

### Architectural Decision Records (docs/decisions/)
- **Coverage**: Are all major components covered by ADRs? Cross-reference internal/ packages against ADR subjects. Flag components that have no ADR but should (anything with non-obvious design choices).
- **Accuracy**: For each ADR, does the "Decision" section match what was actually built? ADRs from early design phases may describe plans that were modified during implementation.
- **Status**: Are any ADRs effectively superseded by later decisions but not marked as such?

### Core Documentation
- **docs/manifesto.md**: Is the manifesto still accurate? Does the system as built match what the manifesto describes?
- **docs/principles.md**: Are the principles and patterns still accurate? Any patterns described that aren't followed, or new patterns that aren't documented?
- **docs/failure-modes.md**: Cross-reference against actual crash recovery implementations. Are the documented failure modes and recovery paths still accurate?
- **docs/naming.md**: Are all current concepts in the naming glossary? Any new concepts missing?
- **docs/workflows.md**: Does the workflow documentation match the actual workflow system?
- **docs/integration-api.md**: Does the integration API doc match actual API surface?

### Project-Level Configuration
- **CLAUDE.md**: Are the build commands, conventions, and component index accurate? Any new components missing from the component list?
- **README.md**: Is the project overview current?

### Internal Documentation
- **Acceptance test docs** (test/integration/LOOP*_ACCEPTANCE.md): Do they accurately describe what's tested?
- **Inline prompt templates**: Are they accurate?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Documentation that's actively wrong (describes nonexistent commands, incorrect flags, wrong behavior) — this will mislead users and agents
- **MEDIUM**: Missing documentation for important features, stale ADRs that no longer match implementation, gaps in naming glossary
- **LOW**: Minor inaccuracies, typos, formatting issues, documentation that's correct but could be clearer

Each finding must include:
1. One-line summary
2. File path and line range (or section heading)
3. **The actual text and code** — quote the documentation passage that's wrong AND the code it should match
4. What's wrong (what the doc says vs what the code does)
5. Suggested fix approach

## Constraints

**DO NOT modify any documentation.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Include the evidence.** Every finding must quote both the wrong documentation passage and the code it should match. Side-by-side comparison makes the issue self-evident.

**Cross-reference everything.** Every factual claim in documentation should be verified against the codebase. Don't take the docs at face value — that's the whole point of this review.

**Verify claims against code.** Read the actual implementation before writing a finding.
