# Integration Surface Exploration

## Design Task

{{target.description}}

---

Explore integration points: dependencies on existing components, extension hooks, backward compatibility, and migration from current behavior.

Focus: How does this design connect to and affect the rest of the system?

**Explore:**
- Existing components touched: what packages, modules, or files need changes?
- Upstream dependencies: what does this feature need from existing components?
- Downstream dependents: what existing features will depend on or be affected by this?
- Migration path: how do we get from current state to new state without downtime?
- Backward compatibility: what existing behavior, CLI contracts, or data formats must be preserved?
- Feature flagging: can this be rolled out incrementally behind a flag?
- Testing strategy: how do we verify integration without breaking existing tests?
- Extension hooks: does this need to be extensible? What extension points are needed?
- Configuration compatibility: do existing config files need migration or new fields?
- Error propagation: how do errors in this component surface to callers?
- Deployment coordination: does this require synchronized deployment of multiple components?
- Rollback plan: how do we revert if integration causes problems in production?

**Questions to answer:**
- Where does this code live in the codebase, and does it follow existing package boundaries?
- How does this affect existing workflows and user habits?
- What needs to change in dependent or consuming code?
- Can this be deployed and rolled back independently of other changes?

**Output format:**
```
## Summary
(1-2 paragraphs: integration surface and key compatibility considerations)

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
