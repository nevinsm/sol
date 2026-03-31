# Refine — Edge Cases: {{target.title}}

## Original Assignment

{{target.description}}

## Your Focus

This is the **edge cases pass**. Previous passes drafted the work, fixed correctness issues, and improved clarity. Your sole job is to find **gaps** — things that are missing, not things that are wrong with what exists.

The question to keep asking: **"What will someone discover is missing when they actually use this?"**

**Do NOT** redo correctness work (fixing bugs in existing logic) or clarity work (renaming, restructuring). Only add what's missing.

### Before You Start

1. **Read everything.** You have no memory of prior passes. Read every file that was created or modified to understand the current state.
2. **Re-read the original assignment above.** Look specifically for requirements or scenarios that aren't covered by the current implementation.

### What to Look For

- **For code:** Boundary conditions (empty inputs, zero values, nil/null, maximum sizes), error paths that aren't handled, missing input validation, concurrent access issues, missing cleanup or resource release, assumptions that aren't enforced, failure modes that silently corrupt state.
- **For writing:** Missing sections the reader will need, undocumented assumptions, unclear prerequisites, gaps in the narrative ("how do I get from step 3 to step 5?"), missing examples for non-obvious concepts, audiences or use cases not addressed.
- **For configuration:** Missing default values, undocumented options that users will need, missing examples or templates, environment-specific variations not covered, missing validation or constraints.

### What to Ignore

- Bugs in existing logic — the correctness pass handled that.
- Unclear naming or structure — the clarity pass handled that.
- Style, formatting, or documentation polish — that's the next pass.

### When You're Done

- Commit your additions with a message describing what gaps you filled and why they matter.
- If you found no gaps, commit an empty commit noting that the work passed edge case review.
