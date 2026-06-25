# Structural Review: Agent and Merge Layer

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first.

## Focus

Production `.go` files in:

- `internal/envoy/`
- `internal/protocol/`, `internal/persona/`
- `internal/runtime/` and `internal/runtime/claude/`, `internal/runtime/codex/`,
  `internal/runtime/loader/`, `internal/runtime/attrutil/`
- `internal/forge/`
- `internal/handoff/`, `internal/guidelines/`
- `skills/` (Go), `.claude/skills/` (skill docs only if structurally relevant)

This is where agents are configured, personas/skills are installed, work is
merged, and sessions are handed off. The runtime layer recently went through a
thin-contract port (ADR-0041); watch for residue from that transition.

## Look For (local smells)

- **SPRAWL** — forge orchestration or runtime adapters that have absorbed
  responsibilities beyond their stated job; long methods with many branches per
  runtime.
- **OVERBUILD** — abstraction that does not earn its keep. The runtime contract
  is deliberately thin (`Descriptor` + a few methods); flag any new generality
  that no second runtime actually exercises. Speculative hooks.
- **INCONSIST (within this layer)** — claude and codex implementations that
  diverge in shape where they should be symmetric, or share code where they
  should not. The runtime CONVENTIONS.md describes the intended symmetry.
- **DEADWEIGHT** — vestigial adapter methods or helpers left over from the
  ADR-0041 port that no longer have callers.
- **MISLEAD** — persona/skill installation whose function names imply broader or
  narrower effect than reality; comments describing the pre-port design.
- **DIVERGE** — persona or config written in two places that can drift.

## Out of Scope (owned elsewhere)

- Named-principle drift as such: `lens-principles`.
- Cross-package coupling: `lens-rejected-patterns`.
- Convention drift (the runtime CONVENTIONS.md rule-by-rule): `lens-conventions`
  owns systemic convention drift; report only blatant in-area structural issues
  here and let the lens own the pattern.
- Test code: `structure-tests`.

Write findings to `review.md` per the DOCTRINE schema.
