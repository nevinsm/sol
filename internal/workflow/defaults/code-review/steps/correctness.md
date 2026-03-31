# Correctness Review

Review the code changes for logical correctness and edge case handling.

Examine the branch diff against main. Read every changed file carefully.

**Look for:**
- Logic errors and bugs — does the code do what it claims?
- Off-by-one errors in loops, slices, and indices
- Null/nil handling — are pointers checked before dereference?
- Unhandled edge cases — empty inputs, zero values, max bounds
- Race conditions in concurrent code — shared state without synchronization
- Dead code or unreachable branches introduced by the change
- Incorrect assumptions — comments that contradict the code
- Integer overflow/underflow potential in arithmetic
- Floating point comparison issues (== on floats)
- Type assertion failures without ok-check in Go

**Questions to answer:**
- Does the code do what the writ description says it should?
- What inputs could cause unexpected behavior or panics?
- Are all code paths tested or obviously correct?
- Are error returns checked consistently?
- Could any goroutine leak or deadlock?

Write findings as a prioritized list with file:line references.
Use P0 (must fix), P1 (should fix), P2 (nice to fix) severity levels.
