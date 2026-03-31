# Scalability Exploration

## Design Task

{{target.description}}

---

Explore scalability concerns: concurrency, resource usage, growth limits, caching strategy, and performance characteristics.

Focus: What happens as load, data volume, or number of users grows?

**Explore:**
- Scale dimensions: data size, concurrent operations, number of agents/worlds/writs
- Resource usage: memory footprint, CPU cost per operation, disk I/O patterns
- Bottlenecks: what is the first thing to break under load?
- Algorithmic complexity: are there O(n²) or worse operations hiding in the design?
- Caching opportunities: what is expensive to compute but stable enough to cache?
- Connection and file handle management: pools, limits, cleanup
- Degradation modes: what happens at limits — crash, slow down, or shed load?
- Lazy vs. eager: what should be computed on demand vs. precomputed?
- Concurrency control: locks, mutexes, SQLite WAL contention, race conditions
- Growth projections: what does 10x, 100x look like? Where does it break?
- Cleanup and garbage collection: how are old resources reclaimed?
- Monitoring hooks: what metrics should be tracked to detect scaling issues?

**Questions to answer:**
- What is the realistic upper bound for scale in this system's use case?
- What is the first bottleneck that will be hit, and at what scale?
- Where should we invest in optimization now vs. keep simple and optimize later?
- What operations need to be constant-time regardless of data size?

**Output format:**
```
## Summary
(1-2 paragraphs: scalability profile and key concerns)

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
