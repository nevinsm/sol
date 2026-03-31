# API Design Exploration

## Design Task

{{target.description}}

---

Explore the public API surface: endpoints, methods, request/response shapes, versioning strategy, and error conventions.

Focus: What should the API look like from a consumer's perspective?

**Explore:**
- Command-line interface: subcommands, flags, argument ordering, shell completion
- Programmatic API: function signatures, return types, error types, context propagation
- Versioning strategy: how will the API evolve without breaking consumers?
- Pagination and streaming: how are large result sets handled?
- Error response format: structured errors, error codes, actionable messages
- Idempotency: which operations are safe to retry?
- Rate limiting and backpressure: how does the API signal overload?
- Input validation: where is validation performed, what are the error messages?
- Naming conventions: consistency with existing commands/functions, discoverability
- Configuration interface: files, environment variables, flags precedence
- Help text and documentation: --help output, examples, man pages
- Backward compatibility: what existing contracts must be preserved?

**Questions to answer:**
- How will users discover and learn this API without reading source code?
- What is the happy path, and what are the most common error paths?
- Does this follow existing CLI/API patterns in the codebase, or does it introduce new conventions?
- What would make this API a joy to use vs. a source of frustration?

**Output format:**
```
## Summary
(1-2 paragraphs: what the API surface looks like and the key design choices)

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
