# Execution Guidelines — Investigation

Follow these phases in order. This is for debugging and root cause analysis.

## 1. Orient

- Read the symptom description in the writ carefully.
- Reproduce the issue if possible — understand what's actually happening vs. what's expected.
- Identify the affected component, module, or subsystem.

## 2. Survey

- Explore the code area around the symptom.
- Read recent changes (git log, git blame) that might have introduced the issue.
- Check related tests — are they passing? Do they cover the failing case?
- Map out the relevant code paths.

## 3. Isolate

- Narrow down to the root cause through targeted exploration, not speculation.
- Gather evidence: specific file:line references, variable states, control flow analysis.
- Distinguish between the symptom and the cause — trace back from the symptom to its origin.
- If the issue spans multiple components, identify the boundary where things go wrong.

## 4. Document

- Write findings to the writ output directory with:
  - **Symptom** — what was observed
  - **Root Cause** — the specific defect with file:line references
  - **Analysis** — how you traced from symptom to cause
  - **Recommended Fix** — concrete changes needed, with enough detail for an implementer

## 5. Chart

- If the fix requires multiple coordinated changes, outline them as potential writs:
  - Scope each change clearly
  - Note sequencing dependencies between changes
  - Estimate relative complexity
- If the fix is simple and self-contained, note that too.

## 6. Resolve

When your investigation is complete:
- `sol resolve`
