# Loop 7 Acceptance Checklist

## Sequential Step Workflow — End-to-End

### Materialize
- [x] `workflow.Materialize` creates one child writ per step in the manifest (`TestStepWorkflowE2E`)
- [x] Steps without `needs` are assigned phase 0; steps with `needs` are assigned phase 1 (`TestStepWorkflowE2E`)
- [x] A caravan is created to group the child writs (`TestStepWorkflowE2E`)

### Phase 0: Cast → Prime → Resolve (analyze step)
- [x] `dispatch.Cast` tethers the analyze writ to the agent (`TestStepWorkflowE2E`)
- [x] Tether exists after cast (`TestStepWorkflowE2E`)
- [x] `dispatch.Prime` output includes the writ ID and target writ title (`TestStepWorkflowE2E`)
- [x] `dispatch.Prime` output includes the step description ("Analyze the target writ") (`TestStepWorkflowE2E`)
- [x] `dispatch.Resolve` sets analyze writ status to "done" (`TestStepWorkflowE2E`)
- [x] Agent record is deleted after resolve (`TestStepWorkflowE2E`)
- [x] Tether is cleared after resolve (`TestStepWorkflowE2E`)

### Phase 1: Cast → Prime → Resolve (implement step)
- [x] `dispatch.Cast` tethers the implement writ after phase 0 completes (`TestStepWorkflowE2E`)
- [x] `dispatch.Prime` output includes the implement step description (`TestStepWorkflowE2E`)
- [x] `dispatch.Resolve` sets implement writ status to "done" (`TestStepWorkflowE2E`)

### Cleanup Verification
- [x] Both child writs are in "done" status after all phases complete (`TestStepWorkflowE2E`)
- [x] Caravan has exactly 2 items (`TestStepWorkflowE2E`)
- [x] All caravan items reference writs with "done" status (`TestStepWorkflowE2E`)

## DAG Workflow — End-to-End

### Materialize
- [x] `workflow.Materialize` without a parent writ leaves `result.ParentID` empty (caravan provides grouping) (`TestDAGWorkflowE2E`)
- [x] Parallel steps (alpha, beta) are assigned phase 0; synthesis step is assigned phase 1 (`TestDAGWorkflowE2E`)
- [x] `worldStore.GetDependencies(synthID)` includes both alpha and beta writ IDs (`TestDAGWorkflowE2E`)

### Phase 0a: Cast → Prime → Resolve (alpha step)
- [x] `dispatch.Cast` tethers the alpha writ (`TestDAGWorkflowE2E`)
- [x] `dispatch.Prime` output includes the alpha step description ("Analyze the alpha dimension") (`TestDAGWorkflowE2E`)
- [x] `dispatch.Prime` output includes "sol resolve" instructions (`TestDAGWorkflowE2E`)
- [x] `dispatch.Resolve` sets alpha writ status to "done" (`TestDAGWorkflowE2E`)

### Phase 0b: Cast → Prime → Resolve (beta step)
- [x] `dispatch.Cast` tethers the beta writ (`TestDAGWorkflowE2E`)
- [x] `dispatch.Prime` output includes the beta step description ("Analyze the beta dimension") (`TestDAGWorkflowE2E`)
- [x] `dispatch.Resolve` sets beta writ status to "done" (`TestDAGWorkflowE2E`)

### Phase 1: Cast → Prime → Resolve (synthesis step)
- [x] `dispatch.Cast` tethers the synthesis writ after both parallel steps complete (`TestDAGWorkflowE2E`)
- [x] `dispatch.Prime` output includes the synthesis description ("Synthesize findings from alpha and beta steps") (`TestDAGWorkflowE2E`)
- [x] `dispatch.Resolve` sets synthesis writ status to "done" (`TestDAGWorkflowE2E`)

### Cleanup Verification
- [x] All 3 child writs (alpha, beta, synthesis) are in "done" status (`TestDAGWorkflowE2E`)
- [x] Caravan has exactly 3 items (`TestDAGWorkflowE2E`)

## Backward Compatibility
- [x] All Loop 0 tests pass
- [x] All Loop 1 tests pass
- [x] All Loop 2 tests pass
- [x] All Loop 3 tests pass
- [x] All Loop 4 tests pass
- [x] All Loop 5 tests pass
- [x] All Loop 6 tests pass

## Overall
- [x] `make test` passes (all loops)
- [x] `make build` succeeds
