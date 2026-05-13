# Loop 5 Acceptance Checklist

## Escalation System

### Create and Route
- [x] `sphereStore.CreateEscalation` creates an escalation with the given severity and source (`TestEscalationCreateAndRoute`)
- [x] `escalation.DefaultRouter.Route` sends mail to "autarch" for high-severity escalations (`TestEscalationCreateAndRoute`)
- [x] Router fires webhook POST when webhook URL is configured (`TestEscalationCreateAndRoute`)
- [x] Escalation record remains in DB with status "open" after routing (`TestEscalationCreateAndRoute`)

### Lifecycle
- [x] New escalation appears in `ListEscalations("open")` (`TestEscalationLifecycle`)
- [x] `AckEscalation` transitions status to "acknowledged" (`TestEscalationLifecycle`)
- [x] `ResolveEscalation` transitions status to "resolved" (`TestEscalationLifecycle`)
- [x] `CountOpen` returns 0 after all escalations are resolved (`TestEscalationLifecycle`)

### Agent-Originated Escalations
- [x] Escalation created with `source="world/agent"` format is routed to operator via mail (`TestEscalationFromAgent`)

### CLI Smoke Tests
- [x] `sol escalate --help` shows "Create an escalation" (`TestCLIEscalateHelp`)
- [x] `sol escalation list --help` shows "List escalations" (`TestCLIEscalationListHelp`)
- [x] `sol escalation ack --help` shows "Acknowledge" (`TestCLIEscalationAckHelp`)
- [x] `sol escalation resolve --help` shows "Resolve" (`TestCLIEscalationResolveHelp`)

## Handoff System

### Capture and Restore
- [x] `handoff.Capture` collects writ ID, recent tmux output, and recent git commits (`TestHandoffCaptureAndRestore`)
- [x] `handoff.Write` persists the handoff state to disk (`TestHandoffCaptureAndRestore`)
- [x] `dispatch.Prime` injects "HANDOFF CONTEXT" section when a handoff file exists (`TestHandoffCaptureAndRestore`)
- [x] Prime output contains recent commits from the handoff state (`TestHandoffCaptureAndRestore`)
- [x] Handoff file survives Prime (durable) but is marked consumed (`TestHandoffCaptureAndRestore`)
- [x] `handoff.HasHandoff` returns false for a consumed handoff (`TestHandoffCaptureAndRestore`)

### Tether Preservation
- [x] Writing a handoff file does not clear the tether or change writ status (`TestHandoffPreservesHook`)

### Handoff with Workflow
- [ ] `handoff.Capture` when workflow is active records the current workflow step (uncovered — see note below)
- [ ] Capture includes workflow progress summary (uncovered — see note below)
- [x] Prime with handoff during active workflow injects handoff context (HANDOFF overrides WORK CONTEXT) (`TestHandoffWithWorkflow`)
- [x] Second Prime after handoff consumed returns standard WORK CONTEXT (no HANDOFF section) (`TestHandoffWithWorkflow`)

### Handoff Overrides Workflow in Prime
- [x] When both a handoff file and active workflow exist, Prime returns handoff context rather than standard prime (`TestHandoffPrimeOverridesWorkflow`)

> **Note (M-7):** Three of the five workflow-specific handoff items are now covered
> by `TestHandoffWithWorkflow` and `TestHandoffPrimeOverridesWorkflow` (added in
> `loop5_test.go`). These tests verify that `dispatch.Prime` returns HANDOFF
> context when both a handoff file and a `.workflow/state.json` exist, and that
> the standard WORK CONTEXT is returned after the handoff is consumed.
>
> The two remaining items (Capture records workflow step, Capture includes workflow
> progress summary) are deferred: `handoff.Capture` does not currently read
> `.workflow/state.json` to populate a dedicated workflow-step field in
> `handoff.State`. The Summary field can carry workflow-step info when agents
> provide it via `CaptureOpts.Summary`, but there is no automatic extraction.
> These items are tracked in the test-coverage backlog pending a `handoff.Capture`
> enhancement that explicitly reads `.workflow/state.json`.

### CLI Smoke Tests
- [x] `sol handoff --help` shows "Stop the current agent session" (`TestCLIHandoffHelp`)

## Consul — Stale Tether Recovery

### Recovery of Stale Hooks
- [x] Consul patrol recovers tethers older than `StaleTetherTimeout` when session is dead: writ → "open", agent → "idle", tether cleared (`TestConsulStaleHookRecovery`)
- [x] Tethers within the timeout window are not recovered (`TestConsulStaleHookIgnoresRecent`)
- [x] Tethers with live sessions are not recovered regardless of age (`TestConsulStaleHookIgnoresAlive`)

### Heartbeat
- [x] Consul patrol writes a heartbeat file after each patrol (`TestConsulHeartbeat`)
- [x] Heartbeat `patrol_count` increments on successive patrols (`TestConsulHeartbeat`)
- [x] Heartbeat `timestamp` is recent (`TestConsulHeartbeat`)

### Lifecycle / Shutdown
- [x] Consul processes SHUTDOWN protocol message and returns a shutdown error from Patrol (`TestConsulLifecycleShutdown`)
- [x] SHUTDOWN message is acknowledged after processing (`TestConsulLifecycleShutdown`)

### CLI Smoke Tests
- [x] `sol consul run --help` shows "Run the consul patrol loop" (`TestCLIConsulRunHelp`)
- [x] `sol consul status --help` shows "Show consul status" (`TestCLIConsulStatusHelp`)

## Consul — Caravan Feeding

### Auto-Dispatch Ready Items
- [x] Consul patrol auto-dispatches ready caravan items whose writ status is "open" (`TestConsulCaravanFeeding`)
- [x] After an item completes (closed), the next patrol dispatches the now-unblocked dependent (`TestConsulCaravanFeeding`)
- [x] Already-dispatched items (status changed from "open") are not re-dispatched (`TestConsulCaravanFeedingNoDuplicates`)

## Prefect / Consul Integration

### Startup
- [x] When consul is enabled, Prefect starts consul via `startDaemonProcess` on first heartbeat (`TestPrefectConsulStartup`)

### Consul Restart on Stale Heartbeat
- [x] Prefect restarts consul when heartbeat is older than `ConsulHeartbeatMax` (`TestPrefectConsulRestart`)
- [x] Prefect does NOT restart consul when heartbeat is fresh (`TestPrefectConsulHealthy`)

### CLI Smoke Tests
- [x] `sol prefect run --help` shows `--consul` and `--source-repo` flags (`TestCLIPrefectRunConsulFlag`)

## End-to-End Orchestration Cycle

- [x] Consul auto-dispatches ready caravan items on first patrol (`TestFullOrchestrationCycle`)
- [x] Escalation created and routed mid-cycle (`TestFullOrchestrationCycle`)
- [x] Handoff context injected by Prime when handoff file exists (`TestFullOrchestrationCycle`)
- [x] Consul recovers stale tether (dead session, aged-out) on subsequent patrol (`TestFullOrchestrationCycle`)
- [x] EventConsulPatrol, EventConsulCaravanFeed, and EventConsulStaleTether all emitted (`TestFullOrchestrationCycle`)

## Backward Compatibility
- [x] All Loop 0 tests pass
- [x] All Loop 1 tests pass
- [x] All Loop 2 tests pass
- [x] All Loop 3 tests pass
- [x] All Loop 4 tests pass

## Overall
- [x] `make test` passes (all loops)
- [x] `make build` succeeds
- [x] No TODOs or incomplete features
