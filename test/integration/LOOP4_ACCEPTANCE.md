# Loop 4 Acceptance Checklist

## Workflow System

> **Note:** Inline workflow execution (instantiate, advance, skip, fail, current, status)
> has been removed. Workflows are now used exclusively for manifesting — decomposing work
> into child writs via `sol workflow manifest`. Outpost execution guidance is provided by
> the guidelines system.

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
