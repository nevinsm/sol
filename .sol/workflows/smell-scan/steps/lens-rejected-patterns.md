# Lens: Rejected Patterns and Coupling Shape

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first, then the
**Rejected Patterns** section of `docs/principles.md`. This lens hunts the
architecture-level smells the project has explicitly decided to avoid, plus the
coupling shape that makes a codebase hard to reason about. Categories: **COUPLE**,
**OVERBUILD**, **SPRAWL**. This is a whole-tree sweep.

These are the highest-leverage findings in the scan: an architectural smell taxes
every future reader and every future change. They are also the easiest to get
wrong, so the anti-churn gates apply with full force. An architectural finding
must come with evidence of real cost, not a diagram preference.

## The Rejected Patterns to Detect

### Universal Bus Coupling

A single state substrate used for everything, creating deep coupling so one
unreliable layer makes everything unreliable. Sol's answer is purpose-specific
schemas within shared DBs. Look for a package, table, or type that has become a
dumping ground that unrelated subsystems all depend on. Look for a "god type"
threaded through many packages.

### Three-Layer Supervision (depth creep)

Supervision is capped at two levels: prefect to sentinel/consul to outposts.
Look for any new monitoring-of-monitors, watchdog-of-watchdog, or triage agent
spawned to watch a watcher. Depth creep here was a documented source of bugs.

### Command Bloat ("188 commands")

Feature accumulation: many commands that are slight variations of others. New
commands must justify existence over extending an existing one. Look across
`cmd/` for near-duplicate command families that should be one command with a
flag, or subcommands that exist only to vary one parameter.

### Monolith Creep

"Composition over monoliths": dispatch is a sequence of atomic, independently
testable steps, not a 2000-line do-everything function. Look for any unit that
has reabsorbed responsibilities composition was meant to separate.

### Premature Abstraction (overbuild)

The manifesto and the autarch both reject abstraction beyond what the work
requires. Look for interfaces with a single implementation and no test seam,
generic machinery with one caller, plugin/registry indirection for a fixed small
set, config flexibility nobody uses.

## Coupling Shape (the import graph)

Survey the dependency structure (e.g. `go list -deps`, import scans):

- **Cycles or near-cycles** between packages that should layer cleanly.
- **God packages** that most others import: is the coupling essential (store,
  config) or accidental (a util grab-bag that accreted unrelated helpers)?
- **Layering violations**: a low-level package reaching up into a high-level one;
  a presentation package imported by core logic.

Map the worst offenders and state the concrete reasoning cost: "to understand
package A you must also load B, C, D because of this avoidable dependency."

## Discipline

Architectural findings are systemic by nature: one finding, many sites. Prefer
"this god package should split along these two responsibilities" over listing
imports. Apply the gates hard: if the current structure is a defensible
pragmatic trade-off (the manifesto values pragmatism over purity), it is not a
finding. Evidence over aesthetics.

## Out of Scope

- Named principles: `lens-principles`.
- Documented conventions: `lens-conventions`.
- Same-operation-N-ways at the function level: `lens-consistency`.

Write findings to `review.md` per the DOCTRINE schema.
