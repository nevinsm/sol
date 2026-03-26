# Execution Guidelines — Code

Follow these phases in order. Each phase builds on the previous one.

## 1. Understand

- Read the writ description carefully — identify what's being asked and why.
- Explore the relevant parts of the codebase. Identify the files you'll touch.
- Consider edge cases, existing patterns, and constraints.
- Read any dependency outputs referenced in your assignment.

## 2. Design Before Code

- Think about your approach before writing any code.
- Identify trade-offs and decide on the simplest path that satisfies the writ.
- List the files you'll change and what each change does.
- If the scope is large, break it into commits.

## 3. Implement

- Make your changes. Write tests where appropriate.
- **Commit early and often.** Each meaningful unit of work gets a commit.
- **Hard gate:** you MUST have at least one commit before moving to review.
  Run `git log origin/main..HEAD` — if it shows nothing, commit now.
  This is non-negotiable. Context exhaustion before code is saved is the #1 failure mode.

## 4. Review

- Re-read **every changed file** end to end — not just the diff, the full file.
- Check for:
  - Correctness: does the logic do what the writ asks?
  - Style: does it match surrounding code conventions?
  - Safety: error handling, edge cases, nil checks?
  - Scope: did you change only what was needed?
- Fix any issues you find. Commit the fixes.

## 5. Verify

- Run the full test suite. Check for regressions.
- If quality gates are configured, run them all.
- Fix any failures before proceeding.

## 6. Resolve

When all checks pass and you're satisfied with the work:
- `sol resolve`
