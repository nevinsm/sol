# Design Synthesis

Merge findings from all six exploration legs into a coherent design recommendation.

**Your task:**
1. Read all six leg outputs (api-design, data-model, ux-interaction, scalability, security, integration)
2. Extract key decisions, recommendations, risks, and concerns from each leg
3. Identify tensions between dimensions — common conflicts include:
   - Security vs. UX (more security often means more friction)
   - Scalability vs. simplicity (optimization adds complexity)
   - Flexibility vs. consistency (extension points can fragment behavior)
   - Backward compatibility vs. clean design (legacy constraints limit options)
   - Performance vs. correctness (caching, denormalization introduce staleness risk)
4. Propose trade-off resolutions with explicit rationale
5. Deduplicate cross-leg findings — issues raised by multiple legs are higher-confidence
6. Produce a draft design document or ADR outline suitable for review

**Output format:**
```
## Executive Summary
(2-3 paragraphs: what we're building, the recommended approach, and key trade-offs)

## Problem Statement
(Clear statement of what we're solving and why)

## Proposed Design

### Overview
(High-level approach: architecture, key components, information flow)

### API / Interface
(Consolidated from api-design and ux-interaction legs)

### Data Model
(Consolidated from data-model leg)

### Key Components
(Main pieces and how they fit together)

## Decisions and Trade-offs

### Decided
For each resolved decision:
- **Decision**: <what was decided>
- **Rationale**: <why this option over alternatives>
- **Dimensions involved**: <which legs informed this>

### Tensions Identified
For each cross-dimension tension:
- **Tension**: <e.g., "Security requires confirmation prompts but UX wants minimal friction">
- **Proposed resolution**: <how to balance>
- **Rationale**: <why this balance is right>

### Open Questions (requiring human input)
- **Question**: <what needs to be decided>
- **Options**: <viable choices>
- **Recommendation**: <if one exists>
- **Blocking**: yes/no — can implementation start without this answer?

## Risks and Mitigations
(Consolidated from security and scalability legs, plus integration risks)

## Implementation Sketch
(From integration leg — phased approach)

### Phase 1: MVP
- ...

### Phase 2: Polish
- ...

### Phase 3: Future
- ...

## Cross-Leg Findings
Issues or insights raised by 2+ dimensions (higher confidence):
- <finding>: raised by <leg1>, <leg2>

## Appendix: Dimension Summaries
(One-paragraph summary from each leg for reference)
```
