# Prompt 03: Loop 5 — Deacon

You are extending the `gt` orchestration system with the deacon — a
town-level patrol process that handles coordination tasks requiring
judgment. The deacon continuously monitors the system for stale hooks,
stranded convoys, and other issues that need cross-rig intervention.
It is a Go process (like the witness) with targeted `claude -p`
call-outs for judgment decisions.

**Working directory:** `~/gt-src/`
**Prerequisite:** Loop 5 prompts 01 (escalation) and 02 (handoff) are
complete.

Read all existing code first. Understand:
- `internal/supervisor/supervisor.go` — how agents are monitored and
  respawned, degraded mode, witness deferral (ADR-0006)
- `internal/witness/witness.go` — per-rig patrol loop pattern, AI
  assessment, respawn logic
- `internal/store/agents.go` — agent CRUD, roles, states
- `internal/store/convoys.go` — convoy readiness checking
- `internal/store/escalations.go` — escalation CRUD (prompt 01)
- `internal/store/messages.go` — message/mail system
- `internal/hook/hook.go` — hook file operations
- `internal/session/manager.go` — session health, capture
- `internal/dispatch/dispatch.go` — Sling for dispatching work
- `internal/escalation/` — notifier routing (prompt 01)
- `internal/config/config.go` — GT_HOME resolution

Read `docs/target-architecture.md` Section 3.7 (Deacon) and Loop 5
definition of done.

---

## Task 1: Deacon Package

Create `internal/deacon/` package with the core patrol loop.

### Config

```go
// internal/deacon/deacon.go
package deacon

import "time"

// Config holds deacon patrol configuration.
type Config struct {
    PatrolInterval    time.Duration // time between patrols (default: 5 minutes)
    StaleHookTimeout  time.Duration // how long a hook can be stale (default: 1 hour)
    HeartbeatDir      string        // path to heartbeat directory (default: $GT_HOME/deacon)
    GTHome            string        // $GT_HOME path
    SourceRepo        string        // path to source git repo (for dispatch)
    EscalationWebhook string        // webhook URL for escalation routing (optional)
}

func DefaultConfig() Config {
    return Config{
        PatrolInterval:   5 * time.Minute,
        StaleHookTimeout: 1 * time.Hour,
    }
}
```

### Heartbeat

```go
// internal/deacon/heartbeat.go

// Heartbeat records the deacon's liveness state.
type Heartbeat struct {
    Timestamp    time.Time `json:"timestamp"`
    PatrolCount  int       `json:"patrol_count"`
    Status       string    `json:"status"` // "running", "stopping"
    StaleHooks   int       `json:"stale_hooks"`   // recovered this patrol
    ConvoyFeeds  int       `json:"convoy_feeds"`  // dispatched this patrol
    Escalations  int       `json:"escalations"`   // open escalation count
}

// HeartbeatPath returns the path to the heartbeat file.
// $GT_HOME/deacon/heartbeat.json
func HeartbeatPath(gtHome string) string

// WriteHeartbeat writes the heartbeat file atomically.
// Creates the deacon directory if needed.
func WriteHeartbeat(gtHome string, hb *Heartbeat) error

// ReadHeartbeat reads the current heartbeat file.
// Returns nil, nil if no heartbeat file exists.
func ReadHeartbeat(gtHome string) (*Heartbeat, error)

// IsStale returns true if the heartbeat is older than maxAge.
func (hb *Heartbeat) IsStale(maxAge time.Duration) bool
```

Write the heartbeat file atomically: write to a temp file, then rename.
This prevents partial reads.

### Deacon Type

```go
// Deacon is the town-level patrol process.
type Deacon struct {
    config     Config
    townStore  TownStore
    sessions   SessionChecker
    logger     *events.Logger
    router     *escalation.Router

    patrolCount int
}
```

### Interfaces

```go
// TownStore is the subset of store.Store used by the deacon.
type TownStore interface {
    // Agents
    ListAgents(rig string, state string) ([]store.Agent, error)
    UpdateAgentState(id, state, hookItem string) error

    // Convoys
    ListConvoys(status string) ([]store.Convoy, error)
    CheckConvoyReadiness(convoyID string, rigOpener func(string) (*store.Store, error)) ([]store.ConvoyItemStatus, error)

    // Escalations
    CreateEscalation(severity, source, description string) (string, error)
    CountOpen() (int, error)

    // Messages
    PendingProtocol(recipient, protoType string) ([]store.Message, error)
    AckMessage(id string) error
    SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)
    SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)
}

// SessionChecker is the subset of session.Manager used by the deacon.
type SessionChecker interface {
    Exists(name string) bool
    List() ([]session.SessionInfo, error)
}

// RigOpener opens a rig store by name.
type RigOpener func(rig string) (*store.Store, error)
```

### Constructor

```go
func New(cfg Config, townStore TownStore, sessions SessionChecker,
    router *escalation.Router, logger *events.Logger) *Deacon
```

---

## Task 2: Patrol Loop

### Run

```go
// Run starts the deacon patrol loop. Blocks until ctx is cancelled.
// 1. Register as agent (role="deacon", rig="town")
// 2. Write initial heartbeat
// 3. Loop: patrol → sleep → repeat
//
// On context cancellation:
// - Write final heartbeat with status="stopping"
// - Set agent state to "idle"
func (d *Deacon) Run(ctx context.Context) error

// Register creates or updates the deacon's agent record.
// Agent ID: "town/deacon", role: "deacon", state: "working".
func (d *Deacon) Register() error
```

### Patrol

```go
// Patrol runs a single patrol cycle:
// 1. Write heartbeat
// 2. Recover stale hooks
// 3. Feed stranded convoys
// 4. Process lifecycle requests
// 5. Emit patrol event
//
// Errors in individual patrol steps are logged but do not stop the
// patrol cycle. The deacon continues to the next step (DEGRADE).
func (d *Deacon) Patrol() error
```

---

## Task 3: Stale Hook Recovery

A hook is "stale" when:
- An agent's state is `"working"` or `"hooked"` (has work assigned)
- The agent's tmux session is dead (does not exist)
- The work item's status is `"hooked"` and the agent's `updated_at` is
  older than `StaleHookTimeout` (default: 1 hour)

The timeout prevents recovering hooks that are just slow to start —
an agent might be in the process of being set up.

```go
// recoverStaleHooks finds and recovers stale hooks across all rigs.
// For each stale hook:
// 1. Log the recovery
// 2. Clear the hook file
// 3. Update work item status → "open", clear assignee
// 4. Update agent state → "idle", clear hook_item
// 5. Emit event
//
// Returns the number of hooks recovered.
func (d *Deacon) recoverStaleHooks() (int, error)
```

Implementation approach:
1. List all agents with state `"working"` (across all rigs)
2. For each agent, check if their session exists
3. If session is dead, check the agent's `updated_at` against
   `StaleHookTimeout`
4. If stale: open the rig's store, update the work item and agent,
   clear the hook file

To open a rig store for recovery, use `store.OpenRig(agent.Rig)`. Close
the rig store after use.

Important: the deacon must handle the case where the rig store or hook
file operations fail gracefully. Log the error and continue to the next
agent. Do not let one failure prevent recovering other stale hooks.

---

## Task 4: Stranded Convoy Feeding

A convoy is "stranded" when:
- The convoy status is `"open"`
- It has items that are ready for dispatch (dependencies satisfied,
  status is `"open"`)
- Those items have not been dispatched (no agent assigned)

```go
// feedStrandedConvoys checks all open convoys for ready, undispatched items.
// For each stranded convoy:
// 1. Check readiness of all items
// 2. For items that are ready and status="open":
//    - Send CONVOY_NEEDS_FEEDING protocol message to "operator"
// 3. Emit event
//
// The deacon does NOT dispatch directly — it sends a protocol message
// that the operator (or automation) can act on. Direct dispatch would
// require rig-specific knowledge (source repo, formula) that the deacon
// doesn't have.
//
// Returns the number of convoys with ready items.
func (d *Deacon) feedStrandedConvoys() (int, error)
```

For each open convoy:
1. Call `townStore.CheckConvoyReadiness(convoyID, store.OpenRig)`
2. Count items where `Ready == true` and `WorkItemStatus == "open"`
3. If count > 0 and no pending `CONVOY_NEEDS_FEEDING` message already
   exists for this convoy: send the protocol message

The "no pending message" check prevents duplicate notifications. Check
for pending messages with the convoy ID in the payload before sending
a new one.

---

## Task 5: Lifecycle Request Processing

The deacon checks its mailbox for lifecycle commands from operators.

```go
// processLifecycleRequests reads and processes operator messages.
// Recognized commands (in message subject):
// - "CYCLE": force immediate patrol after current one
// - "SHUTDOWN": set a flag to stop after current patrol
//
// Unrecognized messages are acknowledged but ignored.
//
// Returns true if a shutdown was requested.
func (d *Deacon) processLifecycleRequests() (shutdown bool, err error)
```

The deacon reads messages where recipient is `"town/deacon"` and type
is `"protocol"`. It acknowledges each message after processing.

If `processLifecycleRequests` returns `shutdown=true`, the `Run` loop
should exit cleanly (write final heartbeat, set agent to idle).

---

## Task 6: CLI Commands

Create `cmd/deacon.go` with the `gt deacon` command group.

### gt deacon run

```
gt deacon run [--interval=<duration>] [--stale-timeout=<duration>]
              [--webhook=<url>]
```

- `--interval` (optional, default `5m`): patrol interval
- `--stale-timeout` (optional, default `1h`): stale hook timeout
- `--webhook` (optional): escalation webhook URL (also from
  `GT_ESCALATION_WEBHOOK` env var)

**Behavior:**
1. Build config from flags and env vars
2. Open town store
3. Create session manager
4. Build escalation router
5. Create and run deacon (blocks until interrupted)
6. Handle SIGINT/SIGTERM for graceful shutdown

**Output:**
```
Deacon starting (patrol every 5m0s, stale timeout 1h0m0s)
[2026-02-27T10:30:00Z] Patrol #1: 0 stale hooks, 0 convoy feeds, 2 open escalations
[2026-02-27T10:35:00Z] Patrol #2: 1 stale hook recovered, 0 convoy feeds, 2 open escalations
```

### gt deacon status

```
gt deacon status [--json]
```

**Behavior:**
1. Read heartbeat file
2. If no heartbeat: print `Deacon is not running.`, exit 1
3. If heartbeat stale (>2x patrol interval): print stale warning
4. Print heartbeat data

**Human output:**
```
Deacon: running
Last patrol: 2m30s ago (patrol #42)
Stale hooks recovered: 0
Convoy feeds: 0
Open escalations: 2
```

**JSON output:**
```json
{"status":"running","timestamp":"2026-02-27T10:30:00Z","patrol_count":42,"stale_hooks":0,"convoy_feeds":0,"escalations":2,"stale":false}
```

---

## Task 7: Event Types

Add deacon event type constants to `internal/events/events.go`:

```go
const (
    EventDeaconPatrol    = "deacon_patrol"
    EventDeaconStaleHook = "deacon_stale_hook"
    EventDeaconConvoyFeed = "deacon_convoy_feed"
)
```

Add formatter cases in `cmd/feed.go`'s `formatEventDescription`:

```go
case events.EventDeaconPatrol:
    return fmt.Sprintf("Deacon patrol #%s: %s stale hooks, %s convoy feeds",
        get("patrol_count"), get("stale_hooks"), get("convoy_feeds"))
case events.EventDeaconStaleHook:
    return fmt.Sprintf("Stale hook recovered: %s (%s)", get("agent_id"), get("work_item_id"))
case events.EventDeaconConvoyFeed:
    return fmt.Sprintf("Convoy needs feeding: %s (%s ready items)", get("convoy_id"), get("ready_count"))
```

---

## Task 8: Tests

### Heartbeat Tests

Create `internal/deacon/heartbeat_test.go`:

```go
func TestWriteAndReadHeartbeat(t *testing.T)
    // Write → Read → matches original
    // Verify JSON file on disk

func TestReadHeartbeatMissing(t *testing.T)
    // No file → nil, nil

func TestHeartbeatIsStale(t *testing.T)
    // Fresh heartbeat (1 minute ago) → not stale at 5m threshold
    // Old heartbeat (10 minutes ago) → stale at 5m threshold
```

### Stale Hook Recovery Tests

Create `internal/deacon/deacon_test.go`:

```go
func TestRecoverStaleHooks(t *testing.T)
    // Set up GT_HOME with:
    // - Agent A: state=working, session dead, updated 2 hours ago
    // - Agent B: state=working, session alive
    // - Agent C: state=idle
    // Create hook files and work items for A
    // Patrol → A's hook recovered, B and C untouched
    // Verify: A's work item status back to "open"
    // Verify: A's agent state is "idle"
    // Verify: A's hook file cleared

func TestRecoverStaleHooksTooRecent(t *testing.T)
    // Agent with dead session but updated_at is 5 minutes ago
    // StaleHookTimeout is 1 hour
    // → Not recovered (too recent, might still be starting)

func TestRecoverStaleHooksPartialFailure(t *testing.T)
    // Two stale agents, one with corrupt hook file
    // → First one recovered, second one logged and skipped
```

### Stranded Convoy Tests

```go
func TestFeedStrandedConvoys(t *testing.T)
    // Create convoy with 3 items, 2 ready and undispatched
    // Patrol → CONVOY_NEEDS_FEEDING message sent
    // Verify message payload contains convoy ID and ready count

func TestFeedStrandedConvoysNoDuplicates(t *testing.T)
    // Create convoy with ready items
    // Send CONVOY_NEEDS_FEEDING message (simulate previous patrol)
    // Patrol → no new message sent (already pending)

func TestFeedStrandedConvoysAllDispatched(t *testing.T)
    // Create convoy where all ready items are already hooked
    // Patrol → no message sent (nothing stranded)
```

### Lifecycle Request Tests

```go
func TestProcessLifecycleShutdown(t *testing.T)
    // Send SHUTDOWN message to "town/deacon"
    // processLifecycleRequests → shutdown=true
    // Message acknowledged

func TestProcessLifecycleCycle(t *testing.T)
    // Send CYCLE message to "town/deacon"
    // processLifecycleRequests → shutdown=false
    // Message acknowledged

func TestProcessLifecycleUnknown(t *testing.T)
    // Send unknown message to "town/deacon"
    // processLifecycleRequests → shutdown=false
    // Message acknowledged (but ignored)
```

### Patrol Integration Test

```go
func TestPatrolCycle(t *testing.T)
    // Set up full GT_HOME with:
    // - 1 stale hooked agent (dead session, old timestamp)
    // - 1 open convoy with ready items
    // - 1 healthy working agent
    // Run one Patrol()
    // Verify: stale hook recovered
    // Verify: convoy feed message sent
    // Verify: heartbeat written
    // Verify: healthy agent untouched
```

### CLI Smoke Tests

Add to `test/integration/cli_loop5_test.go`:

```go
func TestCLIDeaconRunHelp(t *testing.T)
func TestCLIDeaconStatusHelp(t *testing.T)
```

---

## Task 9: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export GT_HOME=/tmp/gt-test
   mkdir -p /tmp/gt-test/.store /tmp/gt-test/deacon

   # Check status when not running
   bin/gt deacon status
   # → "Deacon is not running."

   # Start deacon (will need stores initialized)
   # In practice, run in background for testing:
   bin/gt deacon run --interval=10s &
   DEACON_PID=$!

   # Check status
   sleep 15
   bin/gt deacon status
   bin/gt deacon status --json

   # Check heartbeat file
   cat /tmp/gt-test/deacon/heartbeat.json | jq .

   # Send shutdown
   # (requires sending a protocol message to town/deacon)

   kill $DEACON_PID
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Guidelines

- The deacon is a **Go process** (like the witness), not a full Claude
  session. It uses targeted `claude -p` call-outs only if behavior-level
  assessment is needed (future enhancement — not required for Loop 5).
- The deacon registers as agent `"town/deacon"` with role `"deacon"`.
  This is a special agent that does not belong to a specific rig.
- **DEGRADE principle**: individual patrol step failures must not halt
  the patrol loop. Log errors and continue to the next step. The patrol
  result (heartbeat) should reflect what succeeded and what failed.
- The deacon does NOT dispatch work directly for stranded convoys. It
  sends `CONVOY_NEEDS_FEEDING` messages. Direct dispatch would require
  rig-specific configuration (source repo, formula) that the deacon
  doesn't have access to. The operator or future automation handles
  the actual dispatch.
- Stale hook recovery must be conservative. The 1-hour timeout prevents
  recovering hooks that are legitimately being set up (e.g., slow
  worktree creation, network issues). Only truly abandoned hooks should
  be recovered.
- Heartbeat writes must be atomic (write temp file, rename) to prevent
  partial reads by the supervisor.
- The lifecycle request system is minimal: just SHUTDOWN and CYCLE.
  More commands can be added later. Unknown messages are silently acked.
- The deacon's `Run` function handles SIGINT/SIGTERM via context
  cancellation. The CLI command sets this up.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(deacon): add town-level patrol with stale hook and convoy recovery`
