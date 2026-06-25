# Structural Review: Observability and Presentation

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first.

## Focus

Production `.go` files in:

- `internal/ledger/`, `internal/chronicle/`, `internal/events/`,
  `internal/trace/`
- `internal/status/`, `internal/statusformat/`, `internal/dash/`,
  `internal/style/`

This is the telemetry/event layer plus the rendering surface (status, dash,
styling). GLASS demands these stay inspectable and honest; rendering code is also
where presentation logic tends to accrete.

## Look For (local smells)

- **SPRAWL** — render functions that mix data-gathering, formatting, and layout;
  status/dash code with branching that has outgrown one screen's worth of logic.
- **OVERBUILD** — styling/formatting abstraction layers that add indirection
  without reuse; event/telemetry generality unused by any consumer.
- **DEADWEIGHT** — event types nobody emits or reads, unused style tokens, dead
  trace branches.
- **MISLEAD** — a renderer that silently drops or mislabels data; a status field
  whose label does not match what it computes; comments that misdescribe an
  aggregation window.
- **INCONSIST (within this layer)** — status and dash rendering the same concept
  differently; multiple ad-hoc time/format helpers.
- **DIVERGE** — the same value formatted by two code paths that can disagree.

## Out of Scope (owned elsewhere)

- GLASS adherence as a principle: `lens-principles`.
- Cross-tree formatting inconsistency: `lens-consistency`.
- Convention drift (timestamp format, etc.): `lens-conventions`.
- Test code: `structure-tests`.

Write findings to `review.md` per the DOCTRINE schema.
