# Protocol Layer and Skills Review

Review the packages listed in **Focus** for correctness in persona generation, prompt assembly, skill injection, hook wiring, and skill registration.

The protocol layer defines what agents see when they start — their identity, capabilities, instructions, and tools. Skills extend agent capabilities through discoverable actions. Bugs here cause agents to operate under false assumptions, reference nonexistent commands, or miss available tools.

## What to look for

### Persona Generation
- **Template accuracy**: Do generated personas match the agent role's actual capabilities? Any claims about commands that don't exist or behaviors that aren't implemented?
- **Variable substitution**: Are all template variables resolved? Any raw double-brace tokens leaking into agent prompts?
- **Role-specific content**: Does each role (outpost, envoy, governor, chancellor, forge-merge) get the correct persona? Any copy-paste errors between roles?

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
