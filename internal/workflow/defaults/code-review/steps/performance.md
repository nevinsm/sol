# Performance Review

Review the code changes for performance issues and efficiency.

Examine the branch diff against main. Focus on hot paths and scalability.

**Look for:**
- O(n²) or worse algorithms where O(n) or O(n log n) is possible
- Unnecessary allocations in hot paths — preallocate slices where size is known
- N+1 query patterns — database or API calls inside loops
- Blocking operations in async or concurrent contexts
- Memory leaks or unbounded growth — maps that grow without eviction, goroutines that never exit
- Excessive string concatenation — use strings.Builder or bytes.Buffer
- Unoptimized regex — compiled once or recompiled on every call?
- Missing caching opportunities for repeated expensive computations
- Unnecessary file I/O or network calls that could be batched
- Large structs passed by value instead of by pointer

**Questions to answer:**
- What happens at 10x, 100x, 1000x the current scale?
- Are there obvious optimizations being missed?
- Is performance being traded for readability appropriately?
- Could any operation block the main path unnecessarily?
- Are database queries using appropriate indices?

Write findings with file:line references and estimated impact.
