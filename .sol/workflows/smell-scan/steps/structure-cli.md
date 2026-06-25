# Structural Review: CLI and Operational Tooling

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first.

## Focus

Production `.go` files in:

- `cmd/` (all Cobra commands)
- `internal/workflow/`, `internal/worldexport/`, `internal/worldsync/`
- `internal/doctor/`, `internal/migrate/` and `internal/migrate/migrations/`
- `internal/docgen/`, `internal/docvalidate/`
- `internal/cliapi/` and its subpackages

This is the operator-facing surface. The manifesto's "188 commands" rejected
pattern lives here: feature accumulation, near-duplicate commands, and flag
sprawl. `cmd/CONVENTIONS.md` documents the intended command shape.

## Look For (local smells)

- **SPRAWL** — commands that do too much; flag sets that have grown unwieldy;
  a command file mixing parsing, business logic, and rendering.
- **OVERBUILD** — near-duplicate commands that are slight variations of one
  another (extend, do not multiply); flags nobody uses; speculative subcommands.
- **DEADWEIGHT** — dead commands, unreachable flag handling, unused exit-code
  branches.
- **MISLEAD** — help text or `Long` descriptions that misstate behavior;
  documented exit codes that the code does not actually return; a `--force` that
  bypasses confirmation where the convention reserves that for `--confirm`.
- **INCONSIST (within cmd/)** — commands that handle the same concern (JSON
  output, confirmation, world resolution) in different shapes. Note in-area
  inconsistency; the cross-tree pattern is `lens-consistency`'s.
- **OPAQUE** — a command whose behavior cannot be understood from its help +
  code without external knowledge.

## Out of Scope (owned elsewhere)

- "188 commands" / command-bloat as a *rejected pattern* at the architectural
  level: `lens-rejected-patterns`. Report concrete near-duplicates and sprawl
  here; let the lens own the systemic verdict.
- `docs/cli.md` accuracy and ADR currency: `lens-orientation`.
- Convention drift rule-by-rule (`cmd/CONVENTIONS.md`): `lens-conventions`.
- Test code: `structure-tests`.

Write findings to `review.md` per the DOCTRINE schema.
