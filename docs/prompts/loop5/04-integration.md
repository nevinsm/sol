# Prompt 04: Loop 5 — Integration and Acceptance

You are wiring the Loop 5 components (escalation system, handoff,
deacon) into the existing supervision pipeline and verifying the
complete Loop 5 feature set with integration tests. This is the final
prompt — after this, the gt core system is complete.

**Working directory:** `~/gt-src/`
**Prerequisite:** Loop 5 prompts 01–03 are complete.

Read all existing code first. Understand:
- `internal/supervisor/supervisor.go` — current agent monitoring logic,
  respawn with backoff, degraded mode, witness deferral (ADR-0006)
- `internal/deacon/deacon.go` — the patrol loop and heartbeat (prompt 03)
- `internal/handoff/handoff.go` — state capture and session restart
  (prompt 02)
- `internal/escalation/` — notifier routing (prompt 01)
- `internal/dispatch/dispatch.go` — Sling, Prime, Done
- `internal/session/manager.go` — session start/stop/health
- `internal/store/escalations.go` — escalation CRUD
- `cmd/deacon.go`, `cmd/escalate.go`, `cmd/handoff.go` — new CLI

Read `docs/target-architecture.md` Loop 5 definition of done — all
eight acceptance criteria must be met.

---

## Task 1: Supervisor Integration — Deacon Monitoring

Extend the supervisor to monitor the deacon via its heartbeat file and
restart it if the heartbeat goes stale.

### Config Extension

In `internal/supervisor/supervisor.go`, extend `Config`:

```go
type Config struct {
    // ... existing fields ...
    DeaconEnabled       bool          // whether to monitor the deacon (default: false)
    DeaconHeartbeatMax  time.Duration // max heartbeat age before restart (default: 15 minutes)
    DeaconCommand       string        // command to start deacon (default: "gt deacon run")
    DeaconSourceRepo    string        // source repo path for deacon config
}
```

Update `DefaultConfig()` to include the new fields.

### Heartbeat Check

Add a deacon-specific check to the supervisor's heartbeat loop:

```go
// checkDeacon reads the deacon heartbeat and restarts if stale.
// This runs as part of the regular heartbeat loop, not on every tick —
// check every other patrol (e.g., every 6 minutes with 3-minute interval).
func (s *Supervisor) checkDeacon() error
```

Implementation:
1. Read heartbeat file via `deacon.ReadHeartbeat()`
2. If no heartbeat exists and deacon is expected (DeaconEnabled): start it
3. If heartbeat is stale (older than `DeaconHeartbeatMax`):
   - Check if deacon session exists (`gt-town-deacon`)
   - If session exists: stop it (might be hung)
   - Start a new deacon session
   - Log the restart event
4. If heartbeat is fresh: no action

### Deacon Session Management

The supervisor starts the deacon in a tmux session like other agents:
- Session name: `gt-town-deacon`
- Command: the `DeaconCommand` config value (default: `gt deacon run`)
- Workdir: `$GT_HOME`
- Role: `"deacon"`
- Rig: `"town"`
- Env: `GT_HOME`

### Integration with Existing Logic

The deacon check should:
- NOT be affected by degraded mode (deacon is infrastructure, not a
  worker — it should run even when degraded)
- Have its own backoff tracking (reuse existing `backoff` map)
- NOT trigger mass-death detection (deacon death is a single event)

In the `heartbeat` function (the main patrol loop), add the deacon
check after processing regular agents:

```go
func (s *Supervisor) heartbeat() {
    // ... existing agent monitoring ...

    // Check deacon health (only if enabled).
    if s.cfg.DeaconEnabled {
        if err := s.checkDeacon(); err != nil {
            s.logger.Error("deacon health check failed", "error", err)
        }
    }
}
```

---

## Task 2: Supervisor Startup/Shutdown

### Startup

When `gt supervisor run` is called with `--deacon` flag, the supervisor:
1. Sets `DeaconEnabled = true`
2. On first heartbeat: starts the deacon if not already running
3. The deacon registers itself as an agent on startup

### Shutdown

When the supervisor shuts down (context cancelled):
1. Stop all regular agents (existing behavior)
2. If deacon is enabled: stop the deacon session
3. Log the shutdown

### CLI Extension

In `cmd/supervisor.go`, add flag:

```go
supervisorRunCmd.Flags().Bool("deacon", false, "Enable deacon monitoring and auto-start")
supervisorRunCmd.Flags().String("source-repo", "", "Source repository path (for deacon dispatch)")
```

When `--deacon` is set:
- Set `DeaconEnabled = true` in config
- Set `DeaconSourceRepo` if provided
- The supervisor auto-starts and monitors the deacon

---

## Task 3: Full Lifecycle — gt supervisor run

Update `gt supervisor run` to support the complete agent hierarchy:

```
gt supervisor run --rig=<rig> [--deacon] [--source-repo=<path>]
                  [--interval=<duration>]
```

The supervisor manages:
- **Polecats** (per-rig workers): monitored via session, respawned
  with backoff
- **Witness** (per-rig health monitor): monitored via session,
  restarted on crash (ADR-0006)
- **Refinery** (per-rig merge processor): monitored via session,
  restarted on crash
- **Deacon** (town-level, optional): monitored via heartbeat, restarted
  on stale

### Full Lifecycle Test

The complete startup sequence should be:

1. Supervisor starts, begins heartbeat loop
2. If `--deacon`: check for deacon, start if missing
3. Deacon starts, begins patrol loop
4. Supervisor monitors all agents (including deacon) each heartbeat

The complete shutdown sequence:

1. Supervisor receives SIGINT/SIGTERM
2. Stop all polecats (set state to stalled)
3. Stop witness, refinery sessions
4. Stop deacon session (if enabled)
5. Write final state, exit

---

## Task 4: Integration Tests

Create `test/integration/loop5_test.go`:

### Escalation Integration Tests

```go
func TestEscalationCreateAndRoute(t *testing.T)
    // 1. Create town store with escalation table
    // 2. Set up DefaultRouter with test webhook (httptest.Server)
    // 3. Create high-severity escalation
    // 4. Route it
    // 5. Verify: escalation in DB
    // 6. Verify: mail sent to "operator"
    // 7. Verify: webhook received POST

func TestEscalationLifecycle(t *testing.T)
    // 1. Create escalation
    // 2. List → appears as open
    // 3. Ack → acknowledged
    // 4. List → appears as acknowledged
    // 5. Resolve → resolved
    // 6. CountOpen → 0

func TestEscalationFromAgent(t *testing.T)
    // Simulate an agent creating an escalation:
    // 1. Create town store
    // 2. Create escalation with source="myrig/Toast"
    // 3. Verify escalation stored correctly
    // 4. Verify mail notification sent
```

### Handoff Integration Tests

```go
func TestHandoffCaptureAndRestore(t *testing.T)
    // 1. Set up GT_HOME with hook file, workflow state, git repo
    // 2. Capture state
    // 3. Write handoff file
    // 4. Verify file on disk
    // 5. Prime() with handoff file → handoff context injected
    // 6. Verify handoff file deleted after prime

func TestHandoffPreservesHook(t *testing.T)
    // 1. Set up agent with hook file
    // 2. Write handoff file
    // 3. Verify hook file still exists (not cleared)
    // 4. Verify work item status unchanged

func TestHandoffWithWorkflow(t *testing.T)
    // 1. Set up agent with hook, active workflow at step 2
    // 2. Capture → state includes workflow step and progress
    // 3. Write handoff → file includes workflow info
    // 4. Prime with handoff → output references workflow step
    // 5. After handoff consumed: subsequent Prime → normal workflow prime

func TestHandoffPrimeOverridesWorkflow(t *testing.T)
    // 1. Set up agent with hook, handoff file, AND active workflow
    // 2. Prime() → returns handoff context (not workflow)
    // 3. Handoff takes priority
```

### Deacon Integration Tests

```go
func TestDeaconStaleHookRecovery(t *testing.T)
    // 1. Set up GT_HOME with:
    //    - Rig "myrig" with work item in "hooked" status
    //    - Agent in "working" state with hook file
    //    - Agent updated_at is 2 hours ago
    //    - No tmux session for the agent
    // 2. Run one deacon Patrol()
    // 3. Verify: work item status back to "open"
    // 4. Verify: agent state is "idle"
    // 5. Verify: hook file cleared

func TestDeaconStaleHookIgnoresRecent(t *testing.T)
    // Same setup but updated_at is 5 minutes ago
    // Patrol → no recovery (too recent)

func TestDeaconStaleHookIgnoresAlive(t *testing.T)
    // Same setup but tmux session exists
    // Patrol → no recovery (session alive)

func TestDeaconConvoyFeeding(t *testing.T)
    // 1. Create work items with dependencies: A (no deps), B→A
    // 2. Create convoy with both items
    // 3. Run deacon Patrol()
    // 4. Verify: CONVOY_NEEDS_FEEDING message sent (A is ready)
    // 5. Mark A as done
    // 6. Run another Patrol()
    // 7. Verify: new CONVOY_NEEDS_FEEDING for B (now ready)

func TestDeaconConvoyFeedingNoDuplicates(t *testing.T)
    // 1. Set up convoy with ready items
    // 2. Run Patrol() → message sent
    // 3. Run Patrol() again → no duplicate message

func TestDeaconHeartbeat(t *testing.T)
    // 1. Run one Patrol()
    // 2. Read heartbeat file
    // 3. Verify: timestamp is recent, patrol_count=1
    // 4. Run another Patrol()
    // 5. Verify: patrol_count=2

func TestDeaconLifecycleShutdown(t *testing.T)
    // 1. Register deacon
    // 2. Send SHUTDOWN protocol message to "town/deacon"
    // 3. processLifecycleRequests → returns shutdown=true
    // 4. Message acknowledged
```

### Supervisor + Deacon Integration Tests

```go
func TestSupervisorDeaconStartup(t *testing.T)
    // 1. Create supervisor with DeaconEnabled=true
    // 2. Mock session manager
    // 3. Run one heartbeat cycle
    // 4. Verify: deacon session started (gt-town-deacon)

func TestSupervisorDeaconRestart(t *testing.T)
    // 1. Create supervisor with DeaconEnabled=true
    // 2. Write stale heartbeat (15+ minutes old)
    // 3. Mock session manager (deacon session does not exist)
    // 4. Run one heartbeat cycle
    // 5. Verify: deacon session started

func TestSupervisorDeaconHealthy(t *testing.T)
    // 1. Create supervisor with DeaconEnabled=true
    // 2. Write fresh heartbeat (1 minute old)
    // 3. Mock session manager
    // 4. Run one heartbeat cycle
    // 5. Verify: no restart attempted
```

### End-to-End Test

```go
func TestFullOrchestrationCycle(t *testing.T)
    // Simulate the full orchestration cycle:
    // 1. Create rig with work items and dependencies
    // 2. Create convoy spanning the items
    // 3. Run deacon patrol → detects stranded convoy
    // 4. Verify CONVOY_NEEDS_FEEDING message sent
    // 5. Create escalation (simulating stuck agent)
    // 6. Verify escalation routed correctly
    // 7. Simulate handoff: write handoff file, call Prime
    // 8. Verify handoff context injected
    // 9. Simulate stale hook: mark agent working but kill session
    // 10. Run deacon patrol → recovers stale hook
    // 11. Verify work item returned to open
```

### CLI Smoke Tests

Add to `test/integration/cli_loop5_test.go`:

```go
func TestCLISupervisorRunDeaconFlag(t *testing.T)
    // Verify --deacon flag appears in supervisor run help
```

---

## Task 5: Event Instrumentation

Ensure all Loop 5 operations emit events. Verify the following events
are emitted correctly:

| Event | Source | When |
|-------|--------|------|
| `escalation_created` | `gt escalate` CLI / LogNotifier | Escalation created |
| `escalation_acked` | `gt escalation ack` CLI | Escalation acknowledged |
| `escalation_resolved` | `gt escalation resolve` CLI | Escalation resolved |
| `handoff` | `handoff.Exec()` | Agent hands off |
| `deacon_patrol` | Deacon patrol loop | Each patrol cycle |
| `deacon_stale_hook` | Deacon stale hook recovery | Hook recovered |
| `deacon_convoy_feed` | Deacon convoy feeding | Convoy needs feeding |

Verify formatter cases exist in `cmd/feed.go` for all new event types.

---

## Task 6: Acceptance Checklist

Review `docs/prompts/loop5/acceptance.md` and verify each item. Update
the checklist with check marks as items are verified.

---

## Task 7: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. `go vet ./...` — clean
4. Walk through the acceptance checklist manually
5. Verify backwards compatibility:
   - `gt sling` without workflow still works
   - `gt prime` without handoff or workflow still works
   - `gt done` still works for all scenarios
   - `gt supervisor run` without `--deacon` still works (deacon disabled)
   - All Loop 0–4 tests still pass
6. Commit with message:
   `feat: integrate deacon, escalations, and handoff into supervision pipeline`

---

## Guidelines

- **Backwards compatibility is critical.** The supervisor must work
  exactly as before when `--deacon` is not set. All Loop 0–4 tests must
  pass unchanged.
- The deacon is **optional infrastructure** — it enhances the system but
  is not required for basic operation. When the deacon is down, stale
  hooks accumulate and stranded convoys wait. When it comes back, it
  catches up (DEGRADE principle).
- The supervisor monitors the deacon via heartbeat, NOT via session
  liveness. The deacon writes its heartbeat file atomically. The
  supervisor reads it. If stale: restart.
- The deacon is exempt from degraded mode. It should run even when the
  supervisor is degraded (polecats are down). The deacon is
  infrastructure that helps recovery, not a worker that might cause
  more problems.
- The full lifecycle test (Task 4, `TestFullOrchestrationCycle`) is the
  key verification. It exercises all Loop 5 components working together.
- Event instrumentation is important for observability. Every
  significant action should emit an event.
- Error handling follows existing patterns: wrap errors with context
  (`fmt.Errorf("failed to ...: %w", err)`), log non-fatal errors, and
  continue operating.
- All existing tests must continue to pass.
