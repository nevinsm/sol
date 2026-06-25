# Structural Review: Coordination Core

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first. It defines severity
(S1/S2/S3), the taxonomy, the finding schema, and the four anti-churn gates.
This file only tells you what to read and what local smells to weigh.

## Focus

Read the `.go` files (production, not `_test.go` here) in:

- `internal/store/`
- `internal/config/`
- `internal/dispatch/`
- `internal/tether/`
- `internal/session/`
- `internal/startup/`
- `internal/fileutil/`, `internal/processutil/`, `internal/logutil/`,
  `internal/envfile/`, `internal/namepool/`, `internal/flock/`

This is the coordination core: the seam where writs, tethers, sessions, and
agent state are created and mutated. It is the highest-traffic code in the
system, so orientation cost here is paid most often.

## Look For (local smells)

- **SPRAWL** — functions doing too much (dispatch and resolve are sequences of
  atomic steps per the manifesto; has any single function absorbed steps that
  should be separable?). Files or packages that have outgrown one
  responsibility. Deep nesting that obscures control flow.
- **OVERBUILD** — indirection without payoff: wrappers that only forward,
  parameters that are always the same value, config knobs nothing sets,
  interfaces with one implementation and no test seam justifying them.
- **DEADWEIGHT** — unreachable branches, unused struct fields, helpers no caller
  reaches, vestigial error paths.
- **MISLEAD** — a function whose name or signature implies different behavior
  than the body delivers; a returned error that is always nil; a `Read`-named
  function that writes.
- **DIVERGE** — two places that compute the same coordination fact (the spawn
  path is known to hold "which writ is bound" in three places; look for similar
  multi-truth setups in store/dispatch/tether).

## Out of Scope (owned elsewhere, do not duplicate)

- ZFC / CRASH / GLASS adherence as such: `lens-principles` owns named-principle
  drift. (If you spot a blatant ZFC cache here, note it briefly and let the lens
  own the finding.)
- Cross-package coupling and import-graph shape: `lens-rejected-patterns`.
- The same-operation-N-ways pattern *across* packages: `lens-consistency`.
- Documented-convention drift (error-wrapping format, etc.): `lens-conventions`.
- Test code: `structure-tests`.

Report local, in-area structural smells. Leave cross-cutting patterns to the
lenses so findings do not collide at triage.

Write findings to `review.md` per the DOCTRINE schema.
