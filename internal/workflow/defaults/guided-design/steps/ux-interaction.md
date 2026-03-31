# UX / Interaction Exploration

## Design Task

{{target.description}}

---

Explore the user-facing interaction: CLI commands, flags, output format, feedback loops, and developer ergonomics.

Focus: How will users interact with this feature and what does the experience feel like?

**Explore:**
- Mental model: how do users think about this concept? What metaphor fits?
- Workflow integration: where does this fit in the user's daily workflow?
- Progressive disclosure: simple by default, powerful when needed
- Error experience: what happens when things go wrong? Are errors actionable?
- Feedback and progress: how does the user know the system is working?
- Output formatting: human-readable vs. machine-parseable, color, tables
- Discoverability: --help, tab completion, examples, suggested next steps
- Confirmation and safety: destructive operations need guards (--confirm pattern)
- Defaults: what are sensible defaults that minimize required flags?
- Consistency: does this feel like the rest of the tool or like a bolt-on?
- Accessibility: does this work in all terminal environments? Screen readers?
- Undo and recovery: can the user recover from mistakes?

**Questions to answer:**
- What is the user's goal when they reach for this feature?
- What is the minimum viable interaction — fewest flags, simplest invocation?
- How do we handle the spectrum from beginner to power user?
- What would surprise or confuse a user encountering this for the first time?

**Output format:**
```
## Summary
(1-2 paragraphs: the interaction model and key UX choices)

## Key Decisions Identified
For each decision point:
### Decision: <title>
- **Options**: <list the viable approaches>
- **Tradeoffs**: <what you gain/lose with each>
- **Recommendation**: <preferred option and why>

## Risks and Concerns
- ...

## Recommendations
- ...
```
