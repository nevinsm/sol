# Refine — Correctness: {{target.title}}

## Original Assignment

{{target.description}}

## Your Focus

This is the **correctness pass**. A previous agent produced a draft. Your sole job is to find and fix things that are **wrong** — errors, bugs, incorrect behavior, false claims.

**Do NOT** refactor, restructure, rename, or improve style. If it works correctly but looks ugly, leave it alone. Later passes handle clarity and polish.

### Before You Start

1. **Read everything.** You have no memory of the previous pass. Read every file that was created or modified. Understand what exists before you change anything.
2. **Re-read the original assignment above.** Understand what was asked for so you can judge whether the draft meets the requirements.

### What to Look For

- **For code:** Bugs, wrong behavior, logic errors, off-by-one mistakes, missing error handling, incorrect assumptions, broken control flow, wrong return values, type mismatches, nil/null dereferences, resource leaks.
- **For writing:** Factual inaccuracies, misleading examples, incorrect claims, broken links, wrong terminology, contradictions between sections.
- **For configuration:** Wrong values, missing required fields, invalid syntax, incorrect references, type errors, values that don't match documentation.

### What to Ignore

- Poor naming or unclear code — that's the clarity pass.
- Missing edge case handling — that's the edge cases pass.
- Style, formatting, or documentation gaps — that's the polish pass.
- Anything that works correctly, even if it could be better.

### When You're Done

- Commit your corrections with a message describing what you fixed and why.
- If you found nothing wrong, commit an empty commit noting that the draft passed correctness review.
