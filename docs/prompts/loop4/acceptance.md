# Loop 4 Acceptance Checklist

## Workflow Engine
- [x] `gt workflow instantiate` creates .workflow/ with manifest.json,
      state.json, and step files
- [x] `gt workflow current` outputs current step's rendered markdown
- [x] `gt workflow advance` marks step complete and moves to next ready step
- [x] `gt workflow status` shows progress (human and JSON output)
- [x] DAG dependencies work (branching steps, not just linear)
- [x] Variable substitution ({{var}}) works in step instructions
- [x] Cycle detection rejects circular step dependencies
- [x] Default polecat-work formula extracted from embedded defaults

## Dispatch Integration
- [x] `gt sling --formula=polecat-work` instantiates workflow during dispatch
- [x] `gt prime` injects workflow step instructions when workflow is active
- [x] `gt prime` falls back to standard output when no workflow exists
- [x] `gt done` cleans up .workflow/ directory
- [x] CLAUDE.md includes workflow commands when formula is provided
- [x] Workflow state survives simulated crash (state.json persistence)

## Convoys
- [x] `gt convoy create` creates convoy record in town.db
- [x] `gt convoy add` adds items to existing convoy
- [x] `gt convoy check` shows ready vs blocked items
- [x] `gt convoy status` shows summary of all open convoys
- [x] `gt convoy launch` dispatches ready items via sling
- [x] Convoy auto-closes when all items are done/closed
- [x] Multi-rig convoys work (items from different rigs)

## Dependencies
- [x] `gt store dep add` creates dependency relationship
- [x] `gt store dep list` shows deps and dependents
- [x] Cycle detection rejects circular dependencies
- [x] IsReady checks dependency status correctly

## Conflict Resolution (existing, verify still works)
- [x] Complex merge conflict → refinery creates resolution task
- [x] `gt done --force-with-lease` unblocks original MR
- [x] `gt refinery check-unblocked` auto-unblocks resolved MRs

## Events
- [x] Workflow events (instantiate, advance, complete) emitted
- [x] Convoy events (created, launched, closed) emitted
- [x] `gt feed` formats new event types correctly

## Build
- [x] `make build` succeeds
- [x] `make test` passes (all packages)
- [x] `go vet ./...` clean
