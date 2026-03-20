# Loop 6 Acceptance Checklist

## Caravan Lifecycle State Machine

### State Transitions (Store Layer)
- [x] A newly created caravan has status "drydock" (`TestCaravanLifecycleStateMachine`)
- [x] `UpdateCaravanStatus` transitions drydock → open (commission) (`TestCaravanLifecycleStateMachine`)
- [x] `UpdateCaravanStatus` transitions open → drydock (put back in drydock) (`TestCaravanLifecycleStateMachine`)
- [x] `UpdateCaravanStatus` transitions open → closed (force close), setting `closed_at` timestamp (`TestCaravanLifecycleStateMachine`)
- [x] `UpdateCaravanStatus` transitions closed → drydock (reopen), clearing `closed_at` to nil (`TestCaravanLifecycleStateMachine`)
- [x] `DeleteCaravan` permanently removes the caravan; subsequent `GetCaravan` returns an error (`TestCaravanLifecycleStateMachine`)

### Item Management (Store Layer)
- [x] `UpdateCaravanItemPhase` updates the phase of a specific item within a caravan (`TestCaravanLifecycleStateMachine`)
- [x] `ListCaravanItems` reflects the updated phase and leaves other items unchanged (`TestCaravanLifecycleStateMachine`)
- [x] `RemoveCaravanItem` removes a single item; remaining items are unaffected (`TestCaravanLifecycleStateMachine`)

### CLI Lifecycle
- [x] `sol caravan create` creates a caravan in "drydock" state and returns its ID (`TestCLICaravanLifecycle`)
- [x] `sol caravan commission` transitions drydock → open and outputs "open" (`TestCLICaravanLifecycle`)
- [x] `sol caravan drydock` transitions open → drydock and outputs "drydock" (`TestCLICaravanLifecycle`)
- [x] `sol caravan set-phase` updates a writ item's phase and outputs "phase 1" (`TestCLICaravanLifecycle`)
- [x] `sol caravan remove` removes an item and outputs "Removed" (`TestCLICaravanLifecycle`)
- [x] `sol caravan close --force` closes the caravan regardless of item status and outputs "Closed" (`TestCLICaravanLifecycle`)
- [x] `sol caravan reopen` transitions closed → drydock and outputs "drydock" (`TestCLICaravanLifecycle`)
- [x] `sol caravan delete --confirm` permanently removes the caravan and outputs "Deleted" (`TestCLICaravanLifecycle`)
- [x] A second `sol caravan delete --confirm` on the same caravan fails (caravan no longer exists) (`TestCLICaravanLifecycle`)

## Expansion Workflow — Materialize

### Child Writ Creation
- [x] `workflow.Materialize` for an expansion workflow creates one child writ per template entry (`TestExpansionWorkflowMaterialize`)
- [x] Child writ titles have `{target.title}` substituted with the parent writ's title (`TestExpansionWorkflowMaterialize`)
- [x] The parent writ is unchanged after materialization (expansion uses the existing parent, not creates one) (`TestExpansionWorkflowMaterialize`)
- [x] `result.ParentID` equals the pre-existing parent writ ID (`TestExpansionWorkflowMaterialize`)

### Caravan and Phase Assignment
- [x] Materialization creates a caravan in "drydock" state — commissioning is a separate caller step (`TestExpansionWorkflowMaterialize`)
- [x] Templates without `needs` are assigned phase 0; templates with `needs` dependencies are assigned phase 1 (`TestExpansionWorkflowMaterialize`)
- [x] `result.Phases` map reflects the dependency-derived phase for each template (`TestExpansionWorkflowMaterialize`)

## Convoy Workflow — Materialize

### Parent and Child Writ Creation
- [x] `workflow.Materialize` for a convoy workflow auto-creates a parent writ when no `ParentID` is provided (`TestConvoyWorkflowMaterialize`)
- [x] `result.ParentID` is non-empty after convoy materialization (`TestConvoyWorkflowMaterialize`)
- [x] One child writ is created per leg plus one for the synthesis, totalling legs + 1 child writs (`TestConvoyWorkflowMaterialize`)
- [x] `result.ChildIDs` contains entries for each leg ID and for "synthesis" (`TestConvoyWorkflowMaterialize`)

### Phase and Dependency Assignment
- [x] All leg writs are assigned phase 0 (run in parallel) (`TestConvoyWorkflowMaterialize`)
- [x] The synthesis writ is assigned phase 1 (runs after legs complete) (`TestConvoyWorkflowMaterialize`)
- [x] `worldStore.GetDependencies(synthID)` includes all leg writ IDs as dependencies of the synthesis writ (`TestConvoyWorkflowMaterialize`)

## Workflow Type Validation

### Expansion Manifest Validation
- [x] An expansion manifest that uses `[[steps]]` instead of `[[template]]` is rejected by `workflow.Validate` (`TestWorkflowTypeValidation`)
- [x] An expansion manifest with valid `[[template]]` entries passes validation (`TestWorkflowTypeValidation`)

### Convoy Manifest Validation
- [x] A convoy manifest with no `[[legs]]` or `[synthesis]` is rejected by `workflow.Validate` (`TestWorkflowTypeValidation`)
- [x] A convoy manifest with both `[[legs]]` and `[synthesis]` passes validation (`TestWorkflowTypeValidation`)

## `writ activate` — Context Switching

### Switching the Active Writ
- [x] `dispatch.ActivateWrit` updates the agent's `active_writ` in the DB to the newly activated writ (`TestWritActivateSwitchesWrit`)
- [x] `result.WritID` equals the newly activated writ ID (`TestWritActivateSwitchesWrit`)
- [x] `result.PreviousWrit` equals the previously active writ ID (`TestWritActivateSwitchesWrit`)
- [x] `result.AlreadyActive` is false when a real context switch occurs (`TestWritActivateSwitchesWrit`)
- [x] For outpost-role agents, a `.resume_state.json` file is written to `$SOL_HOME/{world}/outposts/{agent}/` (`TestWritActivateSwitchesWrit`)
- [x] The resume state has `reason = "writ-switch"`, `previous_active_writ` = old writ, `new_active_writ` = new writ (`TestWritActivateSwitchesWrit`)

### Idempotency (Already-Active Writ)
- [x] `dispatch.ActivateWrit` when the target writ is already active returns `result.AlreadyActive = true` (`TestWritActivateAlreadyActive`)
- [x] No `.resume_state.json` is written when the activate is a no-op (`TestWritActivateAlreadyActive`)

## Agent History and Stats

### History — CLI Smoke Tests
- [x] `sol agent history --world=<world>` succeeds with no history recorded (`TestAgentHistoryCLI`)
- [x] `sol agent history --world=<world> --json` succeeds; if output starts with `[`, it is valid JSON (`TestAgentHistoryCLI`)
- [x] `sol agent history <agent> --world=<world>` succeeds with no entries for a new agent (`TestAgentHistoryCLI`)

### History — Round Trip
- [x] `dispatch.Cast` writes a history entry for the casting action (`TestAgentHistoryRoundTrip`)
- [x] `sol agent history <agent> --world=<world> --json` returns a non-empty JSON array after a cast (`TestAgentHistoryRoundTrip`)
- [x] Each history entry includes `agent_name` and `action` fields; the cast entry has `action = "cast"` (`TestAgentHistoryRoundTrip`)

### Stats — CLI Smoke Tests
- [x] `sol agent stats --world=<world>` (leaderboard mode) succeeds with no casts recorded (`TestAgentStatsCLI`)
- [x] `sol agent stats <agent> --world=<world>` output contains the agent name and "Casts:" label (`TestAgentStatsCLI`)
- [x] `sol agent stats <agent> --world=<world> --json` emits valid JSON (`TestAgentStatsCLI`)
- [x] The stats JSON object contains `name` and `total_casts` fields (`TestAgentStatsCLI`)

## Backward Compatibility
- [x] All Loop 0 tests pass
- [x] All Loop 1 tests pass
- [x] All Loop 2 tests pass
- [x] All Loop 3 tests pass
- [x] All Loop 4 tests pass
- [x] All Loop 5 tests pass

## Overall
- [x] `make test` passes (all loops)
- [x] `make build` succeeds
- [x] No TODOs or incomplete features
