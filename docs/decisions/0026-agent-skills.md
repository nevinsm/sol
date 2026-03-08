# ADR-0026: Agent Skills — Progressive Disclosure for Tool Education

Date: 2026-03-08

## Context

Agent personas (CLAUDE.local.md) served dual purposes: behavioral identity and command
reference. As the CLI grew, personas became bloated with command syntax that competes
with behavioral constraints for context window space.

The flat `.claude/sol-cli-reference.md` file contained the entire CLI reference
(~400 lines) regardless of agent role. An outpost agent has no use for forge commands,
and the forge has no use for caravan management.

Claude Code's skills system (`.claude/skills/{name}/SKILL.md`) provides a natural
progressive disclosure mechanism — skills are loaded on demand when relevant, rather
than consuming context window upfront.

## Decision

Replace the monolithic CLI reference with role-scoped skills:

1. **New `InstallSkills(dir, ctx)` function** generates `.claude/skills/{name}/SKILL.md`
   files for each role-appropriate skill. `RoleSkills(role)` returns the skill names.

2. **Lean CLAUDE.local.md generators** — remove command syntax, keep identity,
   behavioral constraints, protocol, and session resilience. Command knowledge moves
   to skills.

3. **Role skill counts**:
   - outpost: 2 (resolve-and-handoff, memories)
   - forge: 3 (forge-patrol, forge-toolbox, merge-operations)
   - governor: 5 (writ-dispatch, caravan-management, world-coordination, notification-handling, memories)
   - envoy: 8 (resolve-and-submit, writ-management, dispatch, session-management, status-monitoring, caravan-management, world-operations, memories)
   - senate: 3 (world-queries, writ-planning, memories)

4. **Stale cleanup** — `InstallSkills` removes skill directories not in the current
   role set, handling role changes cleanly.

5. **Remove `InstallCLIReference`** — the flat `.claude/sol-cli-reference.md` file is
   no longer generated or referenced.

## Consequences

- **Smaller personas** — CLAUDE.local.md shrinks by ~40-60% per role, preserving
  context window for actual work.
- **Progressive disclosure** — agents discover commands when they need them, not all
  at once. Claude Code surfaces skills contextually.
- **Role-scoped knowledge** — each role only sees commands relevant to its work.
  No forge commands in outpost agents, no caravan commands in forge.
- **Skill files are sol-managed** — added to `.git/info/exclude` via setup.go,
  not tracked in version control.
- **`sol docs generate` deprecated** — no longer needed since skills replace the
  flat reference. The command prints a deprecation message.
- **Makefile simplified** — build no longer depends on docs generation.
