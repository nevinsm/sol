# Prompt 04: Loop 3 — Sentinel Agent with AI-Assisted Assessment

You are extending the `sol` orchestration system with the sentinel — a
per-world health monitor that patrols outpost agents, detects stalled and
zombie sessions, triggers recovery, and uses AI-assisted analysis to
evaluate stuck agents. The sentinel is primarily a Go process for speed
and determinism, but shells out to an AI model for judgment calls when
the heuristic detects potential trouble.

**Working directory:** `~/sol-src/`
**Prerequisite:** Loop 3 prompts 01–03 (mail system, event feed, chronicle)
are complete.

Read all existing code first. Understand the store package
(`internal/store/` — agents, work items, messages), the session package
(`internal/session/` — Health, Capture, Inject), the prefect package
(`internal/prefect/`), the dispatch package (`internal/dispatch/`),
the events package (`internal/events/`), and the forge package
(`internal/forge/`) for pattern reference.

Read `docs/target-architecture.md` Section 3.8 (Sentinel) for design
context.

---

## Task 1: Sentinel Package

Create `internal/sentinel/` package with the core sentinel implementation.

### Configuration

```go
// internal/sentinel/sentinel.go
package sentinel

// Config holds sentinel configuration.
type Config struct {
    World             string
    PatrolInterval  time.Duration // default: 3 minutes
    MaxRespawns     int           // default: 2 (per work item)
    CaptureLines    int           // default: 80 (lines of tmux output to capture)
    AssessCommand   string        // default: "claude -p" (AI assessment command)
    SourceRepo      string        // path to source git repo
    GTHome          string        // SOL_HOME path
}

// DefaultConfig returns a Config with default values.
func DefaultConfig(world, sourceRepo, gtHome string) Config
```

### Store Interfaces

Define narrow store interfaces for testability:

```go
// SphereStore is the subset of sphere store operations the sentinel needs.
type SphereStore interface {
    GetAgent(id string) (*store.Agent, error)
    ListAgents(world string) ([]store.Agent, error)
    UpdateAgentState(id, state string) error
    CreateAgent(id, name, world, role string) error
    SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)
}

// WorldStore is the subset of world store operations the sentinel needs.
type WorldStore interface {
    GetWorkItem(id string) (*store.WorkItem, error)
    UpdateWorkItem(id string, updates store.WorkItemUpdates) error
}
```

### Session Interface

```go
// SessionChecker abstracts session operations for testability.
type SessionChecker interface {
    Health(name string, maxInactivity time.Duration) (session.HealthStatus, error)
    IsAlive(name string) (bool, error)
    Capture(name string, lines int) (string, error)
    Start(opts session.StartOpts) error
    Stop(name string) error
    Inject(name string, text string) error
}
```

### Sentinel Struct

```go
// Sentinel monitors outposts in a single world.
type Sentinel struct {
    config         Config
    sphereStore      SphereStore
    worldStore       WorldStore
    sessions       SessionChecker
    logger         *events.Logger   // optional, nil-safe
    respawnCounts  map[respawnKey]int
    lastCaptures   map[string]string // agent ID → hash of last captured output
}

type respawnKey struct {
    AgentID    string
    WorkItemID string
}

// New creates a new Sentinel.
func New(cfg Config, sphere SphereStore, world WorldStore,
    sessions SessionChecker, logger *events.Logger) *Sentinel
```

---

## Task 2: Registration and Lifecycle

### Agent Registration

```go
// Register registers the sentinel agent in the sphere store.
// Agent ID: "{world}/sentinel", role: "sentinel".
// Creates if not exists, reuses if already registered.
func (w *Sentinel) Register() error
```

### Run Lifecycle

```go
// Run starts the sentinel patrol loop. Blocks until context is cancelled.
// Patrols immediately on start, then on each interval.
func (w *Sentinel) Run(ctx context.Context) error
```

**Lifecycle:**
1. Register sentinel agent (create or reuse)
2. Set agent state to `working`
3. Patrol immediately, then on ticker at `PatrolInterval`
4. On context cancellation: set agent state to `idle`, log stop event
5. Return nil

Follow the forge's `Run()` pattern — ticker loop with immediate
first poll, graceful shutdown on context cancellation.

---

## Task 3: Patrol Cycle

```go
// patrol runs one patrol cycle across all outposts in the world.
func (w *Sentinel) patrol() error
```

### Patrol Steps

**Step 1: List outpost agents**

```go
agents, err := w.sphereStore.ListAgents(w.config.World)
// Filter to role="outpost" only
```

**Step 2: Check each outpost**

For each outpost with `role="outpost"`, call `checkPolecat(agent)`:

```go
func (w *Sentinel) checkPolecat(agent store.Agent) error
```

**Case A: Working agent — verify session is alive**

```go
sessionName := fmt.Sprintf("sol-%s-%s", w.config.World, agent.Name)
alive, err := w.sessions.IsAlive(sessionName)
```

- If session is alive: check for progress (see Task 4 — AI assessment)
- If session is dead AND agent has hook_item: agent is **stalled** →
  `handleStalled(agent)`

**Case B: Idle agent — check for zombies**

- If session is alive but agent is idle with no hook_item: **zombie** →
  `handleZombie(agent)`
- If session is dead and agent is idle: healthy idle, no action.

**Case C: Stalled agent — retry recovery**

- If agent state is already `stalled`: call `handleStalled(agent)`

**Step 3: Emit patrol summary event**

```go
w.logger.Emit(events.EventPatrol, w.agentID(), w.agentID(), "feed",
    map[string]any{
        "world":       w.config.World,
        "total":     len(outposts),
        "healthy":   healthyCount,
        "stalled":   stalledCount,
        "zombies":   zombieCount,
        "assessed":  assessedCount,
        "nudged":    nudgedCount,
        "actions":   actionsTaken,
    })
```

---

## Task 4: AI-Assisted Assessment

This is the key differentiator. When a working outpost's session is
alive but appears to have stalled (no output change since last patrol),
the sentinel uses an AI call to assess the situation and decide how to
respond.

### Heuristic Trigger

On each patrol, for each working agent with a live session:

1. Capture the last `CaptureLines` lines of tmux output
2. Hash the captured output (SHA-256 of the text)
3. Compare with the hash from the last patrol (`w.lastCaptures[agentID]`)
4. If the hash is unchanged → trigger AI assessment
5. Update `w.lastCaptures[agentID]` with the new hash

```go
func (w *Sentinel) checkProgress(agent store.Agent, sessionName string) error {
    output, err := w.sessions.Capture(sessionName, w.config.CaptureLines)
    if err != nil {
        return nil // can't capture, skip assessment
    }

    hash := sha256Hash(output)
    lastHash, seen := w.lastCaptures[agent.ID]
    w.lastCaptures[agent.ID] = hash

    if !seen {
        return nil // first patrol for this agent, establish baseline
    }
    if hash != lastHash {
        return nil // output changed, agent is making progress
    }

    // No change since last patrol — assess with AI
    return w.assessAgent(agent, sessionName, output)
}
```

### AI Assessment Call

```go
// assessAgent uses an AI model to evaluate a potentially stuck agent.
func (w *Sentinel) assessAgent(agent store.Agent, sessionName, capturedOutput string) error
```

**Implementation:** Shell out to `claude -p` (Claude Code's print mode)
with a structured prompt. The prompt asks the model to analyze the
agent's session output and return a JSON assessment.

```go
func (w *Sentinel) assessAgent(agent store.Agent, sessionName, capturedOutput string) error {
    prompt := buildAssessmentPrompt(agent, capturedOutput)

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    cmd := exec.CommandContext(ctx, "sh", "-c", w.config.AssessCommand)
    cmd.Stdin = strings.NewReader(prompt)
    out, err := cmd.Output()
    if err != nil {
        // AI call failed — log and move on, don't block patrol
        w.logWarn("assessment failed for %s: %v", agent.ID, err)
        return nil
    }

    var result AssessmentResult
    if err := json.Unmarshal(out, &result); err != nil {
        // Couldn't parse response — try to extract JSON from output
        result, err = extractJSON(out)
        if err != nil {
            w.logWarn("unparseable assessment for %s", agent.ID)
            return nil
        }
    }

    return w.actOnAssessment(agent, sessionName, result)
}
```

### Assessment Prompt

```go
func buildAssessmentPrompt(agent store.Agent, capturedOutput string) string
```

The prompt should be clear, structured, and request JSON output:

```
You are a sentinel agent monitoring AI coding agents in a multi-agent
orchestration system. An agent's tmux session output has not changed
since the last patrol cycle (3 minutes ago). Analyze the session output
below and determine the agent's status.

Agent: {agent.Name} (ID: {agent.ID})
Work item: {agent.TetherItem}
Session output (last 80 lines):
---
{capturedOutput}
---

Respond with ONLY a JSON object (no markdown, no explanation):
{
    "status": "progressing|stuck|waiting|idle",
    "confidence": "high|medium|low",
    "reason": "brief explanation of what the agent appears to be doing",
    "suggested_action": "none|nudge|escalate",
    "nudge_message": "if suggested_action is nudge, the message to send"
}

Status meanings:
- "progressing": Agent is actively working (e.g., long compilation,
  large file write, waiting for a tool call to complete). No action
  needed despite unchanged output.
- "stuck": Agent appears confused, looping, or unable to make progress.
  A nudge with guidance may help.
- "waiting": Agent is waiting for external input or a resource. May
  need a nudge to check its mail or retry.
- "idle": Agent appears to have finished or is not doing anything.
  May be a zombie or may have completed work without calling sol resolve.

Only suggest "escalate" if the situation requires human intervention
(e.g., repeated failures, auth issues, infrastructure problems).
```

### Assessment Result

```go
// AssessmentResult is the structured output from an AI assessment.
type AssessmentResult struct {
    Status         string `json:"status"`          // progressing, stuck, waiting, idle
    Confidence     string `json:"confidence"`      // high, medium, low
    Reason         string `json:"reason"`
    SuggestedAction string `json:"suggested_action"` // none, nudge, escalate
    NudgeMessage   string `json:"nudge_message"`
}
```

### Acting on Assessment

```go
func (w *Sentinel) actOnAssessment(agent store.Agent, sessionName string,
    result AssessmentResult) error
```

**Decision tree:**

```
switch result.SuggestedAction {
case "none":
    // Agent is progressing or we're not confident — do nothing
    log assessment for audit trail

case "nudge":
    // Inject nudge message into the agent's session.
    // NOTE: Direct tmux injection is temporary. When the nudge queue
    // is implemented (future loop), replace with nudge queue submission
    // for turn-boundary delivery.
    w.sessions.Inject(sessionName, result.NudgeMessage)
    emit EventNudge event
    send informational mail to operator

case "escalate":
    // Send RECOVERY_NEEDED protocol message to operator
    w.sphereStore.SendProtocolMessage(
        w.agentID(), "operator",
        store.ProtoRecoveryNeeded,
        store.RecoveryNeededPayload{
            AgentID:    agent.ID,
            WorkItemID: agent.TetherItem,
            Reason:     result.Reason,
        },
    )
    emit EventStalled event
}
```

**Low confidence handling:** If `confidence="low"`, always treat as
`"none"` regardless of suggested action. Better to wait another patrol
cycle than to act on uncertain assessment.

### Event Types

Add to `internal/events/events.go`:

```go
const (
    // ... existing types ...
    EventAssess = "assess"  // AI assessment performed
    EventNudge  = "nudge"   // nudge injected into agent session
)
```

---

## Task 5: Stalled Agent Recovery

```go
// handleStalled handles a outpost whose session died while work was tethered.
func (w *Sentinel) handleStalled(agent store.Agent) error
```

### Recovery Logic

Track respawn attempts with an in-memory map keyed by agent ID + work
item ID:

```go
key := respawnKey{AgentID: agent.ID, WorkItemID: agent.TetherItem}
attempts := w.respawnCounts[key]
```

**Decision tree:**

```
if attempts >= w.config.MaxRespawns {
    → returnWorkToOpen(agent)
} else {
    → respawnAgent(agent)
    w.respawnCounts[key]++
}
```

### Respawn Agent

```go
func (w *Sentinel) respawnAgent(agent store.Agent) error
```

1. Ensure agent state is `working` in sphere store
2. Start a new tmux session:
   ```go
   sessionName := fmt.Sprintf("sol-%s-%s", w.config.World, agent.Name)
   workdir := config.WorktreePath(w.config.GTHome, w.config.World, agent.Name)
   cmd := "claude --dangerously-skip-permissions"
   ```
3. Emit `EventRespawn` event
4. Send `RECOVERY_NEEDED` protocol message to operator (informational)

**The sentinel does NOT re-cast or re-prime.** The tether file is durable,
and the Claude Code `SessionStart` tether fires `sol prime` automatically
(GUPP principle). Restarting the tmux session is sufficient.

### Return Work to Open

```go
func (w *Sentinel) returnWorkToOpen(agent store.Agent) error
```

1. Update work item: status → `open`, clear assignee
2. Clear the tether file: `os.Remove(tetherPath)`
3. Set agent state → `idle`, clear hook_item
4. Clear respawn count for this key
5. Emit `EventStalled` event with `"recovered": false`
6. Send `RECOVERY_NEEDED` protocol message to operator

Tether file path: `$SOL_HOME/{world}/outposts/{agent.Name}/.tether`

Use the tether package's `Clear()` function if available, or
`os.Remove()` directly.

---

## Task 6: Zombie Detection and Cleanup

```go
// handleZombie handles a outpost with a live session but no tethered work.
func (w *Sentinel) handleZombie(agent store.Agent) error
```

### Zombie Criteria

An agent is a zombie if ALL of:
1. Agent state is `idle` (no hook_item in store)
2. Tether file does not exist on disk
3. A tmux session with `sol-{world}-{name}` exists

### Cleanup

1. Stop the tmux session
2. Log the cleanup
3. Emit patrol event noting zombie cleanup

**Safety:** Only touch sessions matching `sol-{world}-{name}` convention
for this world.

---

## Task 7: Prefect Integration

Extend the prefect to handle sentinel agents and defer outpost
management to the sentinel when it's active (ADR-0006).

### Prefect Defers to Sentinel (ADR-0006)

When a sentinel is active for a world, the prefect must skip outpost
management in that world. The sentinel owns outpost supervision (respawn,
max-respawn tracking, return-to-open). The prefect still manages the
sentinel itself and the forge.

**A world is "sentineled" when both conditions hold:**
1. The `{world}/sentinel` agent has `state=working` in the sphere store
2. The sentinel tmux session (`sol-{world}-sentinel`) is alive

Read `docs/decisions/0006-prefect-defers-to-sentinel.md` for full
context.

#### heartbeat changes

In `heartbeat()`, before processing working agents:

1. Query all sentinel agents (`role=sentinel`, `state=working`)
2. For each, check if its tmux session is alive
3. Build a set of sentineled worlds
4. When iterating working agents, skip `role=outpost` agents in
   sentineled worlds

```go
// Build set of sentineled worlds.
witnessedRigs := s.getWitnessedRigs()

for _, agent := range workingAgents {
    sessName := dispatch.SessionName(agent.World, agent.Name)
    if !s.sessions.Exists(sessName) {
        deadCount++
        s.recordDeath() // All deaths count toward mass-death, always.

        // Outposts in sentineled worlds are the sentinel's responsibility.
        if agent.Role == "outpost" && witnessedRigs[agent.World] {
            continue
        }

        if s.degraded {
            // ... existing degraded handling ...
            continue
        }
        s.respawn(agent)
    }
}
```

```go
// getWitnessedRigs returns the set of worlds with an active sentinel.
// A world is sentineled when its sentinel agent is working AND the
// sentinel tmux session is alive.
func (s *Prefect) getWitnessedRigs() map[string]bool {
    sentinels, err := s.sphereStore.ListAgents("", "working")
    if err != nil {
        return nil
    }
    worlds := make(map[string]bool)
    for _, w := range sentinels {
        if w.Role != "sentinel" {
            continue
        }
        sessName := dispatch.SessionName(w.World, w.Name)
        if s.sessions.Exists(sessName) {
            worlds[w.World] = true
        }
    }
    return worlds
}
```

**Note:** Dead outposts in sentineled worlds still count toward mass-death
detection. This is intentional — infrastructure failures are worth
detecting even if another component handles the per-agent response.

### respawnCommand

```go
func respawnCommand(agent store.Agent) string {
    switch agent.Role {
    case "forge":
        // Forge is a Claude session (ADR-0005). Start it the same
        // way as a outpost — Claude handles the patrol loop using Go
        // CLI subcommands as tools.
        return "claude --dangerously-skip-permissions"
    case "sentinel":
        return fmt.Sprintf("sol sentinel run %s", agent.World)
    default:
        return "claude --dangerously-skip-permissions"
    }
}
```

### worktreeForAgent

```go
func worktreeForAgent(agent store.Agent, gtHome string) string {
    switch agent.Role {
    case "forge":
        return config.RefineryWorktreePath(gtHome, agent.World)
    case "sentinel":
        // Sentinel is a Go process, not a worktree-based agent.
        // Use SOL_HOME as working directory.
        return gtHome
    default:
        return config.WorktreePath(gtHome, agent.World, agent.Name)
    }
}
```

---

## Task 8: CLI Commands

Create `cmd/sentinel.go` following the forge CLI pattern exactly.

### Commands

**`sol sentinel run <world>`** — Foreground sentinel loop:
- Signal handling (SIGTERM, SIGINT)
- Opens sphere store and world store
- Creates event logger
- Discovers source repo
- Runs sentinel patrol loop until cancelled

**`sol sentinel start <world>`** — Background session:
- Starts tmux session `sol-{world}-sentinel`
- Runs `sol sentinel run <world>` inside session
- Output: `Sentinel started: sol-{world}-sentinel`

**`sol sentinel stop <world>`** — Stop session:
- Stops `sol-{world}-sentinel` tmux session

**`sol sentinel attach <world>`** — Attach to session:
- `syscall.Exec()` to tmux attach

### Registration

Register `sentinel` under root in `cmd/root.go`.

---

## Task 9: Status Integration

Extend `internal/status/status.go` to include sentinel state.

### Updated RigStatus

Add:
```go
type WitnessInfo struct {
    Running     bool   `json:"running"`
    SessionName string `json:"session_name,omitempty"`
}
```

```go
// In RigStatus:
Sentinel WitnessInfo `json:"sentinel"`
```

### Updated Gather()

```go
witnessSession := fmt.Sprintf("sol-%s-sentinel", world)
witnessAlive, _ := checker.IsAlive(witnessSession)
status.Sentinel = WitnessInfo{
    Running:     witnessAlive,
    SessionName: witnessSession,
}
```

### Updated Human Output

```
World: myworld
Prefect: running (pid 12345)
Forge: running (sol-myworld-forge)
Sentinel: running (sol-myworld-sentinel)

AGENT      STATE     SESSION   WORK
Toast      working   alive     sol-a1b2c3d4: Implement login page
Jasper     idle      -         -

Merge Queue: 2 ready, 1 in progress, 0 failed
Summary: 2 agents (1 working, 1 idle, 0 stalled, 0 dead sessions)
Health: healthy
```

---

## Task 10: Tests

### Sentinel Unit Tests

Create `internal/sentinel/witness_test.go` with mock stores and mock
session checker:

```go
func TestPatrolHealthyAgents(t *testing.T)
    // 3 outposts working with live sessions, output changes each patrol
    // Patrol → no actions taken
    // Verify: patrol event with healthy=3, stalled=0

func TestPatrolDetectsStalled(t *testing.T)
    // 1 outpost working, dead session, hook_item set
    // Patrol → respawn attempted
    // Verify: session start called, respawn event emitted

func TestPatrolMaxRespawns(t *testing.T)
    // Outpost respawned MaxRespawns times already
    // Patrol → work returned to open
    // Verify: work item open, agent idle, tether cleared, event emitted

func TestPatrolDetectsZombie(t *testing.T)
    // Idle outpost, no tether, but live session
    // Patrol → session stopped
    // Verify: session stop called

func TestPatrolIgnoresIdleClean(t *testing.T)
    // Idle outpost, no tether, no session
    // Patrol → no action

func TestPatrolIgnoresNonPolecats(t *testing.T)
    // Agents with role=forge and role=sentinel
    // Patrol → skipped entirely

func TestProgressDetectionOutputChanged(t *testing.T)
    // Working agent, live session
    // First patrol: capture output, store hash
    // Second patrol: different output captured
    // Verify: no AI assessment triggered

func TestProgressDetectionOutputUnchanged(t *testing.T)
    // Working agent, live session
    // First patrol: capture output, store hash
    // Second patrol: same output captured
    // Verify: AI assessment triggered (assessAgent called)

func TestAssessmentNudge(t *testing.T)
    // Mock AI assessment returns: status=stuck, action=nudge
    // Verify: nudge injected into session
    // Verify: nudge event emitted

func TestAssessmentEscalate(t *testing.T)
    // Mock AI assessment returns: status=stuck, action=escalate
    // Verify: RECOVERY_NEEDED message sent to operator
    // Verify: stalled event emitted

func TestAssessmentNone(t *testing.T)
    // Mock AI assessment returns: status=progressing, action=none
    // Verify: no nudge, no escalation

func TestAssessmentLowConfidenceIgnored(t *testing.T)
    // Mock AI assessment returns: confidence=low, action=nudge
    // Verify: no nudge sent (low confidence → treat as none)

func TestAssessmentFailureNonBlocking(t *testing.T)
    // Mock AI command fails (exit code 1, timeout, etc.)
    // Verify: patrol continues without error
    // Verify: no nudge or escalation

func TestRegisterAgent(t *testing.T)
func TestRegisterAgentIdempotent(t *testing.T)
func TestRunLifecycle(t *testing.T)

func TestRespawnAttemptsTracking(t *testing.T)
    // Patrol 1: stalled → respawn (attempt 1)
    // Patrol 2: still stalled → respawn (attempt 2)
    // Patrol 3: still stalled → return to open (max reached)
```

### Testing the AI Assessment

For unit tests, mock the assessment by replacing the command execution.
The simplest approach: make `assessAgent` call a function that can be
overridden in tests:

```go
// In sentinel.go:
type assessFunc func(agent store.Agent, sessionName, output string) (*AssessmentResult, error)

// In Sentinel struct:
assessFn assessFunc // nil = use real AI call

// In tests:
w.assessFn = func(agent store.Agent, sessionName, output string) (*AssessmentResult, error) {
    return &AssessmentResult{
        Status:         "stuck",
        Confidence:     "high",
        SuggestedAction: "nudge",
        NudgeMessage:   "You appear stuck. Try checking the error log.",
    }, nil
}
```

### CLI Smoke Tests

```go
func TestCLIWitnessRunHelp(t *testing.T)
func TestCLIWitnessStartHelp(t *testing.T)
func TestCLIWitnessStopHelp(t *testing.T)
func TestCLIWitnessAttachHelp(t *testing.T)
```

---

## Task 11: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   bin/sol sentinel run testrig   # foreground, Ctrl+C to stop
   # In another terminal:
   bin/sol status testrig        # should show sentinel running
   bin/sol feed --type=patrol    # should show patrol events
   bin/sol feed --type=assess    # should show assessments (if triggered)
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- The sentinel is **primarily a Go process**. The AI assessment is a
  targeted call-out, not a persistent AI session. The patrol loop,
  state detection, respawn logic, and zombie cleanup are all
  deterministic Go code.
- **AI assessment fires only when the heuristic triggers** — no tmux
  output change since the last patrol. This keeps AI costs low while
  catching stuck agents that a pure heuristic would miss.
- **Assessment failure is non-blocking.** If the AI call times out,
  returns garbage, or fails entirely, the sentinel logs a warning and
  continues its patrol. The assessment is additive — the mechanical
  checks (session liveness, stall detection) work regardless.
- **Low confidence = no action.** When the AI is unsure, wait another
  patrol cycle. The cost of a false nudge (interrupting a working
  agent) exceeds the cost of waiting 3 more minutes.
- **`claude -p`** (Claude Code print mode) is the default assessment
  command. It uses whatever auth the operator has configured. The
  command is configurable via `AssessCommand` for operators who want
  to use a different tool or model.
- **Respawn tracking is in-memory.** If the sentinel restarts, counts
  reset. This is acceptable — sentinel restarts are rare, and resetting
  gives agents another chance.
- The sentinel **does not re-cast or re-prime** crashed agents. It
  only restarts the tmux session. GUPP handles the rest.
- One sentinel per world. Agent ID: `{world}/sentinel`.
- All Loop 0, 1, and 2 tests must continue to pass.
- Commit after tests pass with message:
  `feat(sentinel): add per-world health monitor with AI-assisted assessment`
