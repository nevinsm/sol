# Loop 4 Acceptance Checklist

## Workflow System

### Instantiate and Advance
- [x] `workflow.Instantiate` creates a workflow instance with status "running" and current_step set to the first step (`TestWorkflowInstantiateAndAdvance`)
- [x] `workflow.ReadCurrentStep` returns the current step with variable substitution applied (`TestWorkflowInstantiateAndAdvance`)
- [x] `workflow.Advance` moves to the next step in dependency order (`TestWorkflowInstantiateAndAdvance`)
- [x] `workflow.Advance` returns done=true after the final step is completed (`TestWorkflowInstantiateAndAdvance`)
- [x] `workflow.ReadState` returns status="done" and all steps in completed list after final advance (`TestWorkflowInstantiateAndAdvance`)

### Crash Recovery
- [x] Workflow state persisted to disk — ReadState after simulated crash returns correct current step (`TestWorkflowCrashRecovery`)
- [x] Advance after crash continues from the persisted step, not from the beginning (`TestWorkflowCrashRecovery`)

### Cast with Workflow
- [x] `dispatch.Cast` with `--workflow` instantiates the workflow and creates `.workflow/` directory in outpost dir (`TestCastWithWorkflow`)
- [x] `workflow.ReadState` returns non-nil state with correct current_step after cast with workflow (`TestCastWithWorkflow`)
- [x] CLAUDE.local.md contains workflow step reference and advance instruction when cast with workflow (`TestCastWithWorkflow`)
- [x] Cast with workflow emits `EventWorkflowInstantiate` (`TestCastWithWorkflow`)

### Prime with Workflow
- [x] `dispatch.Prime` with active workflow returns current step instructions in output (`TestPrimeWithWorkflow`)
- [x] Prime output contains `sol workflow advance` command (`TestPrimeWithWorkflow`)
- [x] Prime output contains `sol resolve` command (`TestPrimeWithWorkflow`)
- [x] Prime output contains current step marker `[>]` and workflow name (`TestPrimeWithWorkflow`)
- [x] Prime without workflow does not include workflow section or `sol workflow advance` (`TestPrimeWithoutWorkflow`)
- [x] Prime without workflow includes standard instructions and writ ID (`TestPrimeWithoutWorkflow`)

### Resolve Cleanup
- [x] `dispatch.Resolve` removes `.workflow/` directory after completion (`TestDoneWithWorkflowCleanup`)

### End-to-End Propulsion Loop
- [x] Cast → Prime → Advance × N → Resolve flows end-to-end (`TestWorkflowPropulsionLoop`)
- [x] Prime after each advance returns instructions for the current step (`TestWorkflowPropulsionLoop`)
- [x] Resolve marks writ as "done" and cleans up `.workflow/` (`TestWorkflowPropulsionLoop`)
- [x] All three events emitted: EventCast, EventWorkflowInstantiate, EventResolve (`TestWorkflowPropulsionLoop`)

### CLI Smoke Tests
- [x] `sol workflow instantiate --help` shows "Instantiate a workflow" (`TestCLIWorkflowInstantiateHelp`)
- [x] `sol workflow current --help` shows "current step" (`TestCLIWorkflowCurrentHelp`)
- [x] `sol workflow advance --help` shows "Advance" (`TestCLIWorkflowAdvanceHelp`)
- [x] `sol workflow status --help` shows "workflow status" (`TestCLIWorkflowStatusHelp`)
- [x] `sol cast --help` shows `--workflow` and `--var` flags (`TestCLICastWorkflowHelp`)

## Caravan System

### Create, Add, and Check Readiness
- [x] `sphereStore.CreateCaravan` creates a caravan and returns an ID (`TestCaravanCreateAndCheck`)
- [x] `sphereStore.CreateCaravanItem` adds writs to a caravan (`TestCaravanCreateAndCheck`)
- [x] `sphereStore.CheckCaravanReadiness` reports items with no unmet dependencies as ready (`TestCaravanCreateAndCheck`)
- [x] Items with unmet dependencies are reported as blocked (`TestCaravanCreateAndCheck`)
- [x] Closing a dependency writ unblocks the dependent item on the next readiness check (`TestCaravanCreateAndCheck`)
- [x] Item only becomes ready when all its dependencies are closed (merged) (`TestCaravanCreateAndCheck`)

### Auto-Close
- [x] `sphereStore.TryCloseCaravan` returns true and sets status="closed" when all items are closed (`TestCaravanAutoClose`)
- [x] `closed_at` timestamp is set on caravan auto-close (`TestCaravanAutoClose`)

### Multi-World Caravans
- [x] Caravan can span multiple worlds (`TestCaravanMultiWorld`)
- [x] Readiness check works across worlds using per-world stores (`TestCaravanMultiWorld`)
- [x] Intra-world dependency graph respected across world boundaries (`TestCaravanMultiWorld`)
- [x] Multi-world caravan auto-closes when all items across all worlds are closed (`TestCaravanMultiWorld`)

### Caravan Launch Phase-Gate
- [x] `CheckCaravanReadiness` returns exactly the ready items when some are blocked by dependencies (`TestCaravanLaunchDispatch`)
- [x] Only ready items are dispatched; blocked items remain open (`TestCaravanLaunchDispatch`)
- [x] Dispatched items transition to "tethered" status; blocked items stay "open" (`TestCaravanLaunchDispatch`)
- [x] Cast emits EventCast for each dispatched item (`TestCaravanLaunchDispatch`)

### CLI Smoke Tests
- [x] `sol caravan create --help` shows "Create a caravan" (`TestCLICaravanCreateHelp`)
- [x] `sol caravan add --help` shows "Add items" (`TestCLICaravanAddHelp`)
- [x] `sol caravan check --help` shows "readiness" (`TestCLICaravanCheckHelp`)
- [x] `sol caravan status --help` shows "caravan status" (`TestCLICaravanStatusHelp`)
- [x] `sol caravan launch --help` shows "Check readiness of all items" (`TestCLICaravanLaunchHelp`)
- [x] `sol writ dep add --help` shows "dependency" (`TestCLIWritDepAddHelp`)
- [x] `sol writ dep list --help` shows "dependencies" (`TestCLIWritDepListHelp`)

## Backward Compatibility
- [x] All Loop 0 tests pass
- [x] All Loop 1 tests pass
- [x] All Loop 2 tests pass
- [x] All Loop 3 tests pass

## Overall
- [x] `make test` passes (all loops)
- [x] `make build` succeeds
- [x] No TODOs or incomplete features
