# Loop 4 Acceptance Checklist

## Workflow Engine
- [x] `sol workflow instantiate` creates .workflow/ with manifest.json,
      state.json, and step files
- [x] `sol workflow current` outputs current step's rendered markdown
- [x] `sol workflow advance` marks step complete and moves to next ready step
- [x] `sol workflow status` shows progress (human and JSON output)
- [x] DAG dependencies work (branching steps, not just linear)
- [x] Variable substitution ({{var}}) works in step instructions
- [x] Cycle detection rejects circular step dependencies
- [x] Default default-work formula extracted from embedded defaults

## Dispatch Integration
- [x] `sol cast --formula=default-work` instantiates workflow during dispatch
- [x] `sol prime` injects workflow step instructions when workflow is active
- [x] `sol prime` falls back to standard output when no workflow exists
- [x] `sol resolve` cleans up .workflow/ directory
- [x] CLAUDE.md includes workflow commands when formula is provided
- [x] Workflow state survives simulated crash (state.json persistence)

## Caravans
- [x] `sol caravan create` creates caravan record in sphere.db
- [x] `sol caravan add` adds items to existing caravan
- [x] `sol caravan check` shows ready vs blocked items
- [x] `sol caravan status` shows summary of all open caravans
- [x] `sol caravan launch` dispatches ready items via cast
- [x] Caravan auto-closes when all items are done/closed
- [x] Multi-world caravans work (items from different worlds)

## Dependencies
- [x] `sol store dep add` creates dependency relationship
- [x] `sol store dep list` shows deps and dependents
- [x] Cycle detection rejects circular dependencies
- [x] IsReady checks dependency status correctly

## Conflict Resolution (existing, verify still works)
- [x] Complex merge conflict → forge creates resolution task
- [x] `sol resolve --force-with-lease` unblocks original MR
- [x] `sol forge check-unblocked` auto-unblocks resolved MRs

## Events
- [x] Workflow events (instantiate, advance, complete) emitted
- [x] Caravan events (created, launched, closed) emitted
- [x] `sol feed` formats new event types correctly

## Build
- [x] `make build` succeeds
- [x] `make test` passes (all packages)
- [x] `go vet ./...` clean
