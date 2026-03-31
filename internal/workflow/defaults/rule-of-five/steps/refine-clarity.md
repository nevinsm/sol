# Refine — Clarity: {{target.title}}

## Original Assignment

{{target.description}}

## Your Focus

This is the **clarity pass**. Previous passes produced a draft and fixed correctness issues. Your sole job is to make the work **obvious to the reader**. Someone unfamiliar with this should understand it on first read.

**Do NOT** fix bugs or change behavior. If something is wrong, it should have been caught in the correctness pass — leave it. **Do NOT** add missing features or handle new edge cases. Only improve how clearly the existing intent is communicated.

### Before You Start

1. **Read everything.** You have no memory of prior passes. Read every file that was created or modified to understand the current state.
2. **Re-read the original assignment above.** Understand the intent so you can judge whether the work communicates that intent clearly.

### What to Look For

- **For code:** Poor variable/function names, unclear structure, missing or misleading comments, functions doing too many things, dead code, confusing control flow, implicit behavior that should be explicit, magic numbers or strings.
- **For writing:** Awkward flow, inconsistent tone, redundant sections, unnecessary jargon, poor paragraph structure, weak transitions, sentences that require re-reading, ambiguous pronouns or references.
- **For configuration:** Poor organization, missing comments on non-obvious values, inconsistent grouping, unclear naming conventions, undocumented sections.

### The Standard

Ask yourself: **could someone unfamiliar with this understand it on first read?** If the answer is no, improve it until the answer is yes.

### What to Ignore

- Bugs or incorrect behavior — the correctness pass handled that.
- Missing edge cases or boundary conditions — that's the next pass.
- Formatting consistency, test coverage, documentation completeness — that's the polish pass.

### When You're Done

- Commit your clarity improvements with a message describing what you changed and why.
- If the work was already clear, commit an empty commit noting that it passed clarity review.
