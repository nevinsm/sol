# Protocol Layer and Skills Review

Review the packages listed in **Focus** for correctness in persona generation, prompt assembly, skill injection, hook wiring, and skill registration.

The protocol layer defines what agents see when they start — their identity, capabilities, instructions, and tools. Skills extend agent capabilities through discoverable actions. Bugs here cause agents to operate under false assumptions, reference nonexistent commands, or miss available tools.

## Focus

Read all `.go` files in these packages:
- `internal/protocol/`
- `internal/persona/`

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

### Persona Generation
- **Template accuracy**: Do generated personas match the agent role's actual capabilities? Any claims about commands that don't exist or behaviors that aren't implemented?
- **Variable substitution**: Are all template variables resolved? Any raw double-brace tokens leaking into agent prompts?
- **Role-specific content**: Does each role (outpost, envoy, governor, forge-merge) get the correct persona? Any copy-paste errors between roles?

### Prompt Assembly
- **Prompt files** (internal/protocol/prompts/): Do the prompt templates accurately describe the system's current behavior? Check against actual CLI commands and flags — prompts that reference nonexistent flags or deprecated commands will confuse agents.
- **Instruction ordering**: Is context injected in a sensible order? (System → persona → brief → writ context → skills)
- **Size management**: Can assembled prompts exceed context limits? Any unbounded injection?

### Skill System (internal/skills/)
- **Registration**: Are skills correctly discovered and registered? What happens with duplicate skill names?
- **Skill file format**: Is the YAML frontmatter + markdown body parsed correctly? Malformed files handled?
- **Three-tier resolution**: Do skills resolve correctly across project → user → embedded tiers?
- **Role filtering**: Are skills filtered by agent role? Does every agent get only the skills relevant to its role?
- **Lifecycle**: Are skills loaded once at startup or refreshed? If cached, is that a ZFC violation?

### Hook Wiring
- **Hook lifecycle**: Are hooks installed/uninstalled cleanly? What about on crash — are stale hooks left behind?
- **Hook execution**: Are hooks triggered at the correct events? Any missing hooks for important events?
- **Error handling**: If a hook fails, does the agent know? Or does it silently fail?

### Persona Resolution (internal/persona/)
- **Three-tier resolution**: Does Resolve() correctly find persona files across project, user, and embedded tiers? Is the fallback chain consistent with guidelines and skills resolution?
- **Default handling**: What happens when no persona file exists for a role? Is the default adequate?
- **Template consistency**: Are persona templates consistent with what internal/protocol/ expects to receive?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Factual errors in prompts (nonexistent commands, wrong flags, incorrect behavior descriptions), broken injection that causes agent confusion, skills referencing invalid commands
- **MEDIUM**: Missing skills for roles that need them, hook gaps, template variable leaks, role filtering issues
- **LOW**: Stale comments in prompts, minor wording inaccuracies, dead template code

Each finding must include:
1. One-line summary
2. File path and line range
3. **The actual code** — quote the specific lines that demonstrate the issue
4. Concrete impact (what an agent would do wrong because of this issue)
5. Suggested fix approach

## Constraints

**DO NOT modify any source code or prompt files.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Include the code.** Every finding must quote the specific lines from the source. If you cannot point to specific lines, the finding is not concrete enough to report.

**Cross-reference aggressively.** For every command or flag mentioned in a prompt or skill, verify it exists in the actual CLI (cmd/). For every behavior described, verify the implementation matches.

**Verify claims against code.** Read the actual implementation before writing a finding.
