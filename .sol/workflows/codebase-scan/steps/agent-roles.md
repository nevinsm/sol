# Agent Roles Review

Review the packages listed in **Focus** for correctness in agent lifecycle management, persona generation, brief handling, and skill registration.

## What to look for

### Envoy (internal/envoy/)
- **Lifecycle**: Create, delete, session start/stop — are all state transitions clean? Any dangling state after delete (worktree, tether, brief, database records)?
- **Brief injection**: Is the brief correctly loaded and injected on session start? Size limits enforced?
- **Concurrent access**: Can two operations on the same envoy race (e.g., handoff while a notification arrives)?

### Governor (internal/governor/)
- **Session management**: Start, stop, query, summary — are these robust to the governor session being dead or unresponsive?
- **World summary**: Is the cached summary vs live query distinction correct? Stale summary detection?
- **Query interface**: Are queries to the governor correctly routed and responses parsed?

### Chancellor (internal/chancellor/)
- **Cross-world coordination**: Does the chancellor correctly handle multi-world state? Any assumptions about single-world that break?
- **Session lifecycle**: Start/stop behavior? Crash recovery?
- **Scope boundaries**: Does the chancellor stay within its sphere-scoped role or leak into world-level concerns?

### Brief (internal/brief/)
- **File operations**: Read/write atomicity? What happens if the brief file is corrupt or missing?
- **Size management**: Is the 200-line cap enforced? What happens when the agent writes beyond it?
- **Injection**: Is the injection hook wired correctly? Does it handle missing brief gracefully (clean start, not error)?

### Skills (internal/skills/)
- **Registration**: Are skills correctly discovered and registered? What happens with duplicate skill names?
- **Skill file format**: Is the YAML frontmatter + markdown body parsed correctly? Malformed files handled?
- **Lifecycle**: Are skills loaded once at startup or refreshed? If cached, is that a ZFC violation?

## Output

Write all findings to `review.md` in your writ output directory. Structure by severity:

- **HIGH**: Bugs that break agent operations (lost state, orphaned resources, broken persona injection)
- **MEDIUM**: Edge cases in lifecycle transitions, missing cleanup, inconsistent error handling
- **LOW**: Dead code, convention violations, documentation gaps in code comments

Each finding must include:
1. One-line summary
2. File path and line range
3. Concrete failure scenario
4. Suggested fix approach

## Constraints

**DO NOT modify any source code.** This is a read-only analysis. Your only deliverable is `review.md`.

**DO NOT fix things you find.** Document and move on.

**Be specific.** Name the function, the line, the exact failure sequence.

**Verify claims against code.** Read the actual implementation before writing a finding.
