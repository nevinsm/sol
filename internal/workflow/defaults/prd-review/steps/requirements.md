# Requirements Completeness

Assess whether success criteria are defined and whether the requirements are complete enough to build from.

Look for:
- Missing success criteria — how will we know this is done?
- Undefined acceptance conditions — what does "working" mean?
- Missing non-functional requirements: performance, scale, reliability
- No definition of failure modes or error states
- Happy-path-only thinking — what happens when things go wrong?
- Missing rollback, undo, or recovery requirements
- No mention of monitoring, alerting, or observability

Questions to answer:
- Can someone write a test from this PRD? If not, what's missing?
- Is "done" clearly defined and verifiable?
- Are there implicit requirements that haven't been stated?
