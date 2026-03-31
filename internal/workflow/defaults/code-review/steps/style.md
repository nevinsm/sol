# Style Review

Review the code changes for convention compliance and consistency with the codebase.

Examine the branch diff against main. Compare with existing patterns in the project.

**Look for:**
- Naming convention violations — Go conventions (mixedCaps, not snake_case), acronym casing
- Formatting inconsistencies — should be handled by gofmt but check generated/template code
- Import organization — standard library, external, internal grouping
- Comment quality — missing doc comments on exported types/functions, outdated comments, stating the obvious
- Documentation gaps for public APIs — exported functions without purpose description
- Inconsistent patterns within the codebase — doing the same thing differently than neighbors
- Log message quality and levels — appropriate use of info vs warn vs error
- Test naming and organization — Test prefix, descriptive subtests
- Error message formatting — lowercase, no punctuation, with context (Go convention)
- Consistent use of project idioms — how does the rest of the codebase do this?

**Questions to answer:**
- Does this match the conventions documented in CLAUDE.md?
- Would the existing codebase patterns approve of this style?
- Is the code self-documenting where possible?
- Are comments adding value or just restating code?
- Do error messages follow the "failed to X: %w" convention?

Reference specific codebase conventions when flagging issues.
