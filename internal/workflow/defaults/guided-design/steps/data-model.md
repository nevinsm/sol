# Data Model Exploration

## Design Task

{{target.description}}

---

Explore the data model: entities, relationships, storage strategy, migration path, and query patterns.

Focus: What data structures and storage decisions does this design require?

**Explore:**
- Entities and relationships: what are the core objects and how do they relate?
- Storage format and engine: SQLite, files, in-memory — what fits?
- Schema design: fields, types, constraints, NOT NULL vs nullable, defaults
- Indexing strategy: which queries need to be fast, what indices support them?
- Query patterns: what are the read/write access patterns? Hot paths?
- Denormalization tradeoffs: when is duplicating data worth the query simplicity?
- Migration path: how do we get from current schema to new schema safely?
- Data lifecycle: creation, updates, soft/hard deletion, archival, TTLs
- Consistency guarantees: what level of consistency is required? WAL implications?
- Data volume projections: how much data will accumulate over time?
- Backup and recovery: how is data protected against loss?
- Referential integrity: foreign keys, cascading deletes, orphan prevention

**Questions to answer:**
- What data needs to persist across restarts vs. what can be recomputed?
- How will the data grow over time, and what is the retention strategy?
- What are the most performance-critical query patterns?
- How do we handle schema evolution without breaking existing data?

**Output format:**
```
## Summary
(1-2 paragraphs: data model overview and key storage decisions)

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
