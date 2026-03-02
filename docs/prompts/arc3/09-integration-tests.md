# Prompt 09: Arc 3 — Integration Tests

**Working directory:** ~/gt-src/
**Prerequisite:** Prompts 01–08 complete

## Context

Read these files to understand existing test patterns:

- `test/integration/helpers_test.go` — test helpers (setupTestEnv, runGT, initWorld, mocks)
- `test/integration/arc2_test.go` — Arc 2 integration test patterns
- `test/integration/doctor_test.go` — simple command tests
- `test/integration/init_test.go` — setup flow tests
- `test/integration/status_test.go` — status command tests
- `internal/envoy/envoy.go` — envoy package
- `internal/governor/governor.go` — governor package
- `internal/brief/brief.go` — brief package
- `internal/store/caravans.go` — caravan phase logic

## Task 1: Create `test/integration/arc3_test.go`

Write end-to-end integration tests for all Arc 3 features. Use the existing
test helper infrastructure (`setupTestEnv`, `runGT`, `initWorld`, etc.).

### Schema V7 — Caravan Phase Tests

- `TestCaravanPhaseCreation` — create caravan with items in multiple phases,
  verify phase stored correctly:
  ```
  sol caravan create --name="phased" --item=<id1> --phase=0 --world=myworld
  sol caravan add-items <caravan-id> --item=<id2> --phase=1 --world=myworld
  sol caravan check <caravan-id>
  ```
  Verify phase 0 item is ready, phase 1 item is not ready.

- `TestCaravanPhaseOrdering` — create phased caravan, complete phase 0 items,
  verify phase 1 items become ready:
  ```
  # Phase 0 item: mark as done via store
  # Check readiness: phase 1 item should now be ready
  ```

- `TestCaravanPhaseBackwardCompat` — create caravan without explicit phases,
  verify all items are phase 0 and ready.

### Brief System Tests

- `TestBriefInjectEndToEnd` — create a brief file, run `sol brief inject`,
  verify framed output on stdout, verify `.session_start` created.

- `TestBriefCheckSaveEndToEnd` — write session_start, modify brief, run
  `sol brief check-save`, verify exit 0. Then write session_start again
  (without modifying brief), verify exit 1 with nudge message.

- `TestBriefInjectTruncation` — create 300-line brief, inject with default
  max-lines, verify truncation notice in output.

- `TestBriefInjectMissingFile` — inject on nonexistent path, verify no error.

- `TestBriefStopHookActiveBypass` — set SOL_STOP_HOOK_ACTIVE=true, run
  check-save on un-updated brief, verify exit 0 (bypass).

### Envoy Lifecycle Tests

- `TestEnvoyCreateAndList` — create an envoy, verify it appears in list:
  ```
  sol envoy create scout --world=myworld [--source-repo=...]
  sol envoy list --world=myworld
  ```
  Verify: envoy directory exists, agent record has role=envoy, list shows scout.

- `TestEnvoyStartStop` — start an envoy, verify session running, stop it:
  ```
  sol envoy start scout --world=myworld
  # verify session exists (tmux has-session)
  sol envoy stop scout --world=myworld
  # verify session gone
  ```

- `TestEnvoyBriefAndDebrief` — create envoy, write brief, verify `sol envoy brief`
  shows content, run `sol envoy debrief`, verify archived:
  ```
  sol envoy create scout --world=myworld
  # Write content to .brief/memory.md
  sol envoy brief scout --world=myworld
  # Verify output matches content
  sol envoy debrief scout --world=myworld
  # Verify .brief/archive/ has file, memory.md gone
  ```

- `TestEnvoyHooksInstalled` — create and start envoy, verify
  `.claude/settings.local.json` in worktree contains brief hooks.

### Governor Lifecycle Tests

- `TestGovernorStartStop` — start governor, verify session and mirror:
  ```
  sol governor start --world=myworld [--source-repo=...]
  # verify session exists
  # verify mirror directory exists with git repo
  sol governor stop --world=myworld
  # verify session gone, mirror persists
  ```

- `TestGovernorRefreshMirror` — start governor, add a commit to source repo,
  refresh mirror, verify new commit visible:
  ```
  sol governor start --world=myworld
  # Add commit to source repo
  sol governor refresh-mirror --world=myworld
  # Verify mirror has new commit
  ```

- `TestGovernorBriefAndSummary` — write brief and world-summary, verify
  `sol governor brief` and `sol governor summary` output.

- `TestGovernorHooksInstalled` — start governor, verify hooks file contains
  brief inject and refresh-mirror commands.

### Resolve Behavior Tests

- `TestResolveEnvoyKeepsSession` — create envoy, tether to work item, resolve:
  ```
  sol envoy create scout --world=myworld
  sol envoy start scout --world=myworld
  # Create work item, write tether file
  sol resolve --world=myworld --agent=scout
  # Verify: MR created, work item done, session STILL RUNNING
  ```

- `TestResolveAgentKillsSession` — standard agent resolve, verify session killed
  (existing behavior preserved):
  ```
  sol cast --world=myworld --work-item=<id>
  # Wait for session
  sol resolve --world=myworld --agent=<name>
  # Verify: session killed after brief delay
  ```

### Prefect Behavior Tests

- `TestPrefectSkipsEnvoy` — create envoy with dead session, run prefect
  heartbeat, verify envoy NOT respawned. (This may require calling prefect
  internals directly rather than running the full prefect loop.)

- `TestPrefectSkipsGovernor` — same for governor.

### Status Display Tests

- `TestStatusWithEnvoys` — create envoys, run `sol status myworld`, verify
  Envoys section appears with BRIEF column.

- `TestStatusWithGovernor` — start governor, run `sol status myworld`, verify
  governor appears in Processes section.

- `TestStatusMixedRoles` — create outpost agents, envoys, and governor.
  Run `sol status myworld`, verify all three sections present.

- `TestStatusNoEnvoySection` — no envoys exist, verify Envoys section omitted.

- `TestStatusSphereWithNewColumns` — create envoys and governor, run
  `sol status`, verify ENVOYS and GOV columns in sphere overview.

- `TestStatusJSONBackwardCompat` — run `sol status myworld --json`, verify
  JSON includes new fields (envoys, governor) AND all old fields still present.

- `TestStatusCaravanPhases` — create phased caravan, verify phase progress
  in status output.

### Cross-Feature Tests

- `TestEnvoyFullWorkflow` — end-to-end: create envoy → start → create work
  item → tether → work → resolve → session stays → brief updated → debrief

- `TestGovernorDispatchFlow` — governor starts → operator can observe via
  status → stop → brief persists

## Task 2: Update Test Helpers if Needed

If new helpers are needed for Arc 3 tests, add to `test/integration/helpers_test.go`:

- `createEnvoy(t, solHome, world, name string)` — create envoy via CLI
- `startGovernor(t, solHome, world string)` — start governor via CLI
- `writeBrief(t, path, content string)` — write brief file for testing

Follow existing helper patterns (use `t.Helper()`, clean up in `t.Cleanup`).

## Verification

- `make build && make test` passes
- `make test-integration` passes
- No flaky tests — use `pollUntil` for async operations (session start/stop)
- Tests clean up tmux sessions in `t.Cleanup`

## Guidelines

- Follow existing integration test patterns from Arc 2
- Use `setupTestEnv` for environment isolation
- Tests that need git repos should create real temp repos (not mocks)
- Tests that check tmux sessions should skip if tmux unavailable
- Keep each test focused on one feature — cross-feature tests at the end
- Use `--json` output for assertions where possible (easier to parse)

## Commit

```
test(arc3): add integration tests for envoy, governor, brief, and caravan phases
```
