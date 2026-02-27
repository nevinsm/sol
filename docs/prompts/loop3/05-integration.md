# Prompt 05: Loop 3 — Integration Tests and Acceptance

You are writing the integration tests and acceptance checklist for Loop 3
of the `sol` orchestration system. This prompt verifies that all Loop 3
components (mail system, event feed, chronicle, sentinel) work together
correctly and that backward compatibility with Loops 0–2 is maintained.

**Working directory:** `~/sol-src/`
**Prerequisite:** Loop 3 prompts 01–04 are complete.

Read all existing code first. Study the Loop 2 integration tests in
`test/integration/loop2_test.go` and `test/integration/helpers_test.go`
for patterns. Understand the test helpers for setting up git repos,
stores, and dispatch operations.

Read `docs/target-architecture.md` Section 5 (Loop 3 definition of done)
for acceptance criteria.

---

## Task 1: Fix Any Broken Tests

Before writing new tests, ensure all existing tests pass:

```bash
make test
```

If any tests from prompts 01–04 are failing, fix them first. Common
issues:
- Interface changes that need mock updates in existing test files
- Import path changes
- New function parameters that existing callers need to handle (e.g.,
  logger parameters must accept nil)

Document any fixes made.

---

## Task 2: Integration Test Helpers

Extend `test/integration/helpers_test.go` with helpers needed for
Loop 3 tests.

### Mail Helpers

```go
// sendAndVerifyMessage sends a message and verifies it appears in the
// recipient's inbox. Returns the message ID.
func sendAndVerifyMessage(t *testing.T, sphereStore *store.Store,
    sender, recipient, subject, body string) string

// waitForProtocolMessage polls for a specific protocol message type
// addressed to a recipient. Returns the message when found or fails
// after timeout.
func waitForProtocolMessage(t *testing.T, sphereStore *store.Store,
    recipient, protoType string, timeout time.Duration) *store.Message
```

### Event Helpers

```go
// collectEvents reads all events from the event feed file and returns
// them, optionally filtered by type.
func collectEvents(t *testing.T, gtHome, eventType string) []events.Event

// assertEventEmitted verifies that at least one event of the given type
// exists in the feed.
func assertEventEmitted(t *testing.T, gtHome, eventType string)
```

### Sentinel Helpers

```go
// newTestWitness creates a sentinel configured for testing with short
// intervals and a mock assessment function. Returns the sentinel and
// a cancel function.
func newTestWitness(t *testing.T, world string, sphereStore SphereStore,
    worldStore WorldStore, sessions SessionChecker,
    logger *events.Logger) (*sentinel.Sentinel, context.CancelFunc)
```

---

## Task 3: Integration Tests

Create `test/integration/loop3_test.go` with comprehensive end-to-end
tests.

### Test 1: Mail Send and Receive

```go
func TestMailSendAndReceive(t *testing.T)
```

**Scenario:** Messages flow correctly through the mail system.

**Steps:**
1. Open sphere store
2. Send message from `operator` to `testrig/Toast`
3. Verify: `Inbox("testrig/Toast")` returns the message
4. Verify: `CountUnread("testrig/Toast")` returns 1
5. Read the message — verify content matches, marked as read
6. Ack the message
7. Verify: `Inbox("testrig/Toast")` returns empty
8. Verify: `CountUnread("testrig/Toast")` returns 0

### Test 2: Protocol Message Flow

```go
func TestProtocolMessageFlow(t *testing.T)
```

**Scenario:** Typed protocol messages work correctly.

**Steps:**
1. Open sphere store
2. Send OUTPOST_DONE protocol message from `testrig/Toast` to
   `testrig/sentinel`
3. Verify: `PendingProtocol("testrig/sentinel", "OUTPOST_DONE")`
   returns the message
4. Parse body as PolecatDonePayload — verify fields
5. Ack the message
6. Verify: `PendingProtocol("testrig/sentinel", "OUTPOST_DONE")`
   returns empty

### Test 3: Event Feed End-to-End

```go
func TestEventFeedEndToEnd(t *testing.T)
```

**Scenario:** Events are emitted and readable via the feed reader.

**Steps:**
1. Set up SOL_HOME, create event logger
2. Emit events of different types (cast, done, patrol)
3. Read with no filter → all present
4. Read with type filter → only matching
5. Read with limit → correct count
6. Verify JSONL file lines are valid JSON

### Test 4: Chronicle Dedup and Aggregation

```go
func TestCuratorDedupAndAggregation(t *testing.T)
```

**Scenario:** Chronicle correctly deduplicates and aggregates events.

**Steps:**
1. Write duplicate events to raw feed (same type/source/actor within
   DedupWindow)
2. Write a burst of cast events within AggWindow
3. Write several unique done events
4. Run chronicle cycle
5. Verify: duplicates collapsed to one event
6. Verify: cast burst collapsed to one sling_batch event
7. Verify: done events preserved individually
8. Read curated feed via reader — verify correct event count

### Test 5: Chronicle Feed Truncation

```go
func TestCuratorFeedTruncation(t *testing.T)
```

**Scenario:** Chronicle truncates the curated feed when it exceeds max size.

**Steps:**
1. Set MaxFeedSize to a small value (e.g., 1KB)
2. Write enough events to raw feed to exceed the limit after curation
3. Run chronicle cycle
4. Verify: curated feed size is at or below MaxFeedSize
5. Verify: remaining events are valid JSON lines (no partial lines)
6. Verify: most recent events are preserved

### Test 6: Sentinel Detects Stalled Agent

```go
func TestWitnessDetectsStalledAgent(t *testing.T)
```

**Scenario:** Sentinel detects a outpost whose session died mid-work.

**Steps:**
1. Set up stores, create outpost with state=`working`, hook_item set
2. Create work item in `tethered` status
3. Write tether file to disk
4. Mock session checker: dead for this outpost
5. Run one sentinel patrol
6. Verify: respawn attempted (session start called)
7. Verify: respawn event emitted

### Test 7: Sentinel Max Respawns Returns Work to Open

```go
func TestWitnessMaxRespawnsReturnsWork(t *testing.T)
```

**Scenario:** After max respawn attempts, sentinel returns work to open.

**Steps:**
1. Set up stores, outpost working, tether set, tether file on disk
2. Mock session checker: always dead
3. Patrol MaxRespawns + 1 times (e.g., 3 patrols with MaxRespawns=2)
4. After first 2: agent still working (respawn attempted each time)
5. After 3rd: agent idle, work item `open`, tether file removed
6. Verify: stalled event with appropriate payload

### Test 8: Sentinel Cleans Up Zombie Sessions

```go
func TestWitnessCleanupZombies(t *testing.T)
```

**Scenario:** Zombie sessions are detected and cleaned up.

**Steps:**
1. Create idle outpost with no hook_item, no tether file on disk
2. Mock session checker: alive
3. Run patrol
4. Verify: session stop called
5. Verify: patrol event shows zombie count > 0

### Test 9: Sentinel AI Assessment — Nudge

```go
func TestWitnessAIAssessmentNudge(t *testing.T)
```

**Scenario:** When output hasn't changed between patrols, the sentinel
runs an AI assessment and nudges a stuck agent.

**Steps:**
1. Create working outpost with live session
2. Mock session capture returns same output on consecutive calls
3. Mock assessment function returns: status=stuck, confidence=high,
   action=nudge, message="Try checking the error output"
4. Patrol once (establishes baseline hash)
5. Patrol again (hash unchanged → triggers assessment)
6. Verify: nudge injected into session with the assessment's message
7. Verify: assess event emitted

### Test 10: Sentinel AI Assessment — Low Confidence Ignored

```go
func TestWitnessAIAssessmentLowConfidence(t *testing.T)
```

**Scenario:** Low-confidence assessments are ignored.

**Steps:**
1. Create working outpost, same output both patrols
2. Mock assessment returns: status=stuck, confidence=low, action=nudge
3. Patrol twice
4. Verify: NO nudge injected (low confidence → no action)

### Test 11: Sentinel AI Assessment Failure Non-Blocking

```go
func TestWitnessAIAssessmentFailure(t *testing.T)
```

**Scenario:** AI assessment failure doesn't break the patrol.

**Steps:**
1. Create working outpost, same output both patrols
2. Mock assessment function returns error
3. Patrol twice
4. Verify: patrol completes without error
5. Verify: no nudge, no escalation
6. Verify: patrol event still emitted (assessment failure is logged
   but doesn't block the patrol)

### Test 12: Events Emitted During Dispatch

```go
func TestEventsEmittedDuringDispatch(t *testing.T)
```

**Scenario:** Cast and done operations emit events when logger provided.

**Steps:**
1. Set up stores, source repo, logger
2. Create work item, cast it (with logger)
3. Verify: `EventCast` in feed
4. Run done (with logger)
5. Verify: `EventResolve` in feed

### Test 13: Status Shows Sentinel and Chronicle State

```go
func TestStatusShowsWitnessState(t *testing.T)
```

**Scenario:** Status command reports sentinel state.

**Steps:**
1. Set up stores
2. Gather status — sentinel not running
3. Mock sentinel session as alive
4. Gather status — sentinel running with session name

---

## Task 4: CLI Smoke Tests

Create or extend `test/integration/cli_loop3_test.go`:

### Mail CLI

```go
func TestCLIMailSendHelp(t *testing.T)
func TestCLIMailInboxHelp(t *testing.T)
func TestCLIMailReadHelp(t *testing.T)
func TestCLIMailAckHelp(t *testing.T)
func TestCLIMailCheckHelp(t *testing.T)
```

### Feed CLI

```go
func TestCLIFeedHelp(t *testing.T)
func TestCLILogEventHelp(t *testing.T)
```

### Chronicle CLI

```go
func TestCLICuratorRunHelp(t *testing.T)
func TestCLICuratorStartHelp(t *testing.T)
func TestCLICuratorStopHelp(t *testing.T)
```

### Sentinel CLI

```go
func TestCLIWitnessRunHelp(t *testing.T)
func TestCLIWitnessStartHelp(t *testing.T)
func TestCLIWitnessStopHelp(t *testing.T)
func TestCLIWitnessAttachHelp(t *testing.T)
```

---

## Task 5: Backward Compatibility Verification

Ensure all Loop 0, Loop 1, and Loop 2 tests still pass:

```bash
make test
```

Common compatibility issues to check and fix:

- **Dispatch interface changes:** If Cast/Done gained a logger
  parameter, existing tests and mocks must pass nil.
- **Status Gather() changes:** If it gained new parameters (sentinel
  info), existing callers need updating.
- **Prefect changes:** New cases in respawnCommand/worktreeForAgent
  must not break existing outpost/forge handling.
- **Sphere schema V1→V2:** Migration must preserve existing agents table.
- **Store interface extensions:** New methods (SendMessage, etc.) need
  mock implementations in existing test files.

---

## Task 6: Acceptance Checklist

Create `test/integration/LOOP3_ACCEPTANCE.md`:

```markdown
# Loop 3 Acceptance Checklist

## Mail System
- [ ] Messages table created in sphere.db (schema V2)
- [ ] Escalations table created in sphere.db (schema V2)
- [ ] Sphere schema V1→V2 migration preserves existing agents
- [ ] SendMessage creates message with msg- prefix ID
- [ ] Inbox returns pending messages ordered by priority then age
- [ ] ReadMessage fetches content and marks as read
- [ ] AckMessage sets delivery=acked with timestamp
- [ ] CountUnread returns correct unread count
- [ ] Protocol messages sent with type=protocol and JSON body
- [ ] PendingProtocol filters by recipient and protocol type
- [ ] CLI: sol mail send creates message
- [ ] CLI: sol mail inbox lists pending messages
- [ ] CLI: sol mail read displays message content
- [ ] CLI: sol mail ack acknowledges message
- [ ] CLI: sol mail check reports unread count

## Event Feed
- [ ] Events logged to $SOL_HOME/.events.jsonl as valid JSONL
- [ ] Concurrent writes are flock-serialized (no interleaving)
- [ ] Logger is best-effort (nil-safe, errors swallowed silently)
- [ ] Reader filters by type, since, limit
- [ ] Follow mode streams new events via channel
- [ ] Cast emits EventCast when logger provided
- [ ] Done emits EventResolve when logger provided
- [ ] Forge emits merge events when logger provided
- [ ] Prefect emits respawn/degraded events when logger provided
- [ ] CLI: sol feed displays events with human-readable format
- [ ] CLI: sol feed --follow streams events
- [ ] CLI: sol feed --type filters by event type
- [ ] CLI: sol feed --json outputs raw JSONL
- [ ] CLI: sol log-event emits custom events

## Chronicle
- [ ] Chronicle reads raw events and writes curated feed
- [ ] Audit-only events filtered from curated feed
- [ ] Duplicate events deduplicated within 10s window
- [ ] Cast bursts aggregated within 30s window
- [ ] Non-aggregatable events (done, merged) preserved individually
- [ ] Curated feed truncated when exceeding max size
- [ ] Truncation preserves complete JSON lines (no partials)
- [ ] Checkpoint file tracks read position across restarts
- [ ] sol feed reads curated feed by default, raw with --raw
- [ ] CLI: sol chronicle run/start/stop all work

## Sentinel
- [ ] Sentinel registers as {world}/sentinel with role=sentinel
- [ ] Patrol cycle runs every PatrolInterval
- [ ] Dead session + tethered work → stalled detection
- [ ] Stalled agent → respawn attempted (max 2 per work item)
- [ ] After max respawns → work returned to open, tether cleared, agent idle
- [ ] Zombie session (idle + no tether + live session) → session stopped
- [ ] Healthy agents → no action taken
- [ ] Progress heuristic: captures tmux output and hashes between patrols
- [ ] No output change → AI assessment triggered
- [ ] AI assessment nudge → message injected into agent session
- [ ] AI assessment escalate → RECOVERY_NEEDED message to operator
- [ ] Low confidence assessment → no action taken
- [ ] AI assessment failure → patrol continues (non-blocking)
- [ ] Patrol emits EventPatrol with summary counts
- [ ] Assessment emits EventAssess
- [ ] Nudge emits EventNudge
- [ ] Prefect handles sentinel role (respawnCommand, worktreeForAgent)
- [ ] Status shows sentinel running/stopped
- [ ] CLI: sol sentinel run/start/stop/attach all work

## Backward Compatibility
- [ ] All Loop 0 tests pass
- [ ] All Loop 1 tests pass
- [ ] All Loop 2 tests pass
- [ ] Existing dispatch operations work with nil logger
- [ ] Existing mocks updated for new interface methods

## Overall
- [ ] make test passes (all loops)
- [ ] make build succeeds
- [ ] No TODOs or incomplete features
```

Mark each item as checked (`[x]`) as you verify it.

---

## Task 7: Verify

1. `make test` — all tests pass (Loops 0 + 1 + 2 + 3)
2. `make build` — succeeds
3. Review test coverage:
   - Mail: send, inbox, read, ack, check, protocol, filters
   - Events: log, read, concurrent, follow, filter, visibility
   - Chronicle: dedup, aggregation, truncation, checkpoint, lifecycle
   - Sentinel: stalled, max-respawn, zombie, healthy, progress
     detection, AI assessment (nudge, escalate, low-confidence,
     failure), lifecycle
   - CLI: all new commands have help smoke tests
   - Dispatch: events emitted during cast/done
4. Verify the acceptance checklist is complete (all items checked)
5. Clean up any test artifacts

---

## Guidelines

- Integration tests use real stores (SQLite in temp dirs) but mock
  session managers (no real tmux in CI).
- Mock the AI assessment function in sentinel tests — never make real
  AI calls in tests.
- Follow Loop 2 test patterns: `t.TempDir()` for isolation, `SOL_HOME`
  via env var, cleanup after each test.
- Tests should be independent — no shared state between tests.
- Use `t.Parallel()` where safe.
- Test names describe the scenario, not the implementation.
- Each test verifies one behavior.
- The acceptance checklist is the definition of done.
- Commit after tests pass with message:
  `test: add Loop 3 integration tests, CLI smoke tests, and acceptance checklist`
