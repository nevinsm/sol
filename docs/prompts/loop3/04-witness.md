# Prompt 04: Loop 3 — Witness Agent with AI-Assisted Assessment

You are extending the `gt` orchestration system with the witness — a
per-rig health monitor that patrols polecat agents, detects stalled and
zombie sessions, triggers recovery, and uses AI-assisted analysis to
evaluate stuck agents. The witness is primarily a Go process for speed
and determinism, but shells out to an AI model for judgment calls when
the heuristic detects potential trouble.

**Working directory:** `~/gt-src/`
**Prerequisite:** Loop 3 prompts 01–03 (mail system, event feed, curator)
are complete.

Read all existing code first. Understand the store package
(`internal/store/` — agents, work items, messages), the session package
(`internal/session/` — Health, Capture, Inject), the supervisor package
(`internal/supervisor/`), the dispatch package (`internal/dispatch/`),
the events package (`internal/events/`), and the refinery package
(`internal/refinery/`) for pattern reference.

Read `docs/target-architecture.md` Section 3.8 (Witness) for design
context.

---

## Task 1: Witness Package

Create `internal/witness/` package with the core witness implementation.

### Configuration

```go
// internal/witness/witness.go
package witness

// Config holds witness configuration.
type Config struct {
    Rig             string
    PatrolInterval  time.Duration // default: 3 minutes
    MaxRespawns     int           // default: 2 (per work item)
    CaptureLines    int           // default: 80 (lines of tmux output to capture)
    AssessCommand   string        // default: "claude -p" (AI assessment command)
    SourceRepo      string        // path to source git repo
    GTHome          string        // GT_HOME path
}

// DefaultConfig returns a Config with default values.
func DefaultConfig(rig, sourceRepo, gtHome string) Config
```

### Store Interfaces

Define narrow store interfaces for testability:

```go
// TownStore is the subset of town store operations the witness needs.
type TownStore interface {
    GetAgent(id string) (*store.Agent, error)
    ListAgents(rig string) ([]store.Agent, error)
    UpdateAgentState(id, state string) error
    CreateAgent(id, name, rig, role string) error
    SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)
}

// RigStore is the subset of rig store operations the witness needs.
type RigStore interface {
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

### Witness Struct

```go
// Witness monitors polecats in a single rig.
type Witness struct {
    config         Config
    townStore      TownStore
    rigStore       RigStore
    sessions       SessionChecker
    logger         *events.Logger   // optional, nil-safe
    respawnCounts  map[respawnKey]int
    lastCaptures   map[string]string // agent ID → hash of last captured output
}

type respawnKey struct {
    AgentID    string
    WorkItemID string
}

// New creates a new Witness.
func New(cfg Config, town TownStore, rig RigStore,
    sessions SessionChecker, logger *events.Logger) *Witness
```

---

## Task 2: Registration and Lifecycle

### Agent Registration

```go
// Register registers the witness agent in the town store.
// Agent ID: "{rig}/witness", role: "witness".
// Creates if not exists, reuses if already registered.
func (w *Witness) Register() error
```

### Run Lifecycle

```go
// Run starts the witness patrol loop. Blocks until context is cancelled.
// Patrols immediately on start, then on each interval.
func (w *Witness) Run(ctx context.Context) error
```

**Lifecycle:**
1. Register witness agent (create or reuse)
2. Set agent state to `working`
3. Patrol immediately, then on ticker at `PatrolInterval`
4. On context cancellation: set agent state to `idle`, log stop event
5. Return nil

Follow the refinery's `Run()` pattern — ticker loop with immediate
first poll, graceful shutdown on context cancellation.

---

## Task 3: Patrol Cycle

```go
// patrol runs one patrol cycle across all polecats in the rig.
func (w *Witness) patrol() error
```

### Patrol Steps

**Step 1: List polecat agents**

```go
agents, err := w.townStore.ListAgents(w.config.Rig)
// Filter to role="polecat" only
```

**Step 2: Check each polecat**

For each polecat with `role="polecat"`, call `checkPolecat(agent)`:

```go
func (w *Witness) checkPolecat(agent store.Agent) error
```

**Case A: Working agent — verify session is alive**

```go
sessionName := fmt.Sprintf("gt-%s-%s", w.config.Rig, agent.Name)
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
        "rig":       w.config.Rig,
        "total":     len(polecats),
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

This is the key differentiator. When a working polecat's session is
alive but appears to have stalled (no output change since last patrol),
the witness uses an AI call to assess the situation and decide how to
respond.

### Heuristic Trigger

On each patrol, for each working agent with a live session:

1. Capture the last `CaptureLines` lines of tmux output
2. Hash the captured output (SHA-256 of the text)
3. Compare with the hash from the last patrol (`w.lastCaptures[agentID]`)
4. If the hash is unchanged → trigger AI assessment
5. Update `w.lastCaptures[agentID]` with the new hash

```go
func (w *Witness) checkProgress(agent store.Agent, sessionName string) error {
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
func (w *Witness) assessAgent(agent store.Agent, sessionName, capturedOutput string) error
```

**Implementation:** Shell out to `claude -p` (Claude Code's print mode)
with a structured prompt. The prompt asks the model to analyze the
agent's session output and return a JSON assessment.

```go
func (w *Witness) assessAgent(agent store.Agent, sessionName, capturedOutput string) error {
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
You are a witness agent monitoring AI coding agents in a multi-agent
orchestration system. An agent's tmux session output has not changed
since the last patrol cycle (3 minutes ago). Analyze the session output
below and determine the agent's status.

Agent: {agent.Name} (ID: {agent.ID})
Work item: {agent.HookItem}
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
  May be a zombie or may have completed work without calling gt done.

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
func (w *Witness) actOnAssessment(agent store.Agent, sessionName string,
    result AssessmentResult) error
```

**Decision tree:**

```
switch result.SuggestedAction {
case "none":
    // Agent is progressing or we're not confident — do nothing
    log assessment for audit trail

case "nudge":
    // Inject nudge message into the agent's session
    w.sessions.Inject(sessionName, result.NudgeMessage)
    emit EventNudge event
    send informational mail to operator

case "escalate":
    // Send RECOVERY_NEEDED protocol message to operator
    w.townStore.SendProtocolMessage(
        w.agentID(), "operator",
        store.ProtoRecoveryNeeded,
        store.RecoveryNeededPayload{
            AgentID:    agent.ID,
            WorkItemID: agent.HookItem,
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
// handleStalled handles a polecat whose session died while work was hooked.
func (w *Witness) handleStalled(agent store.Agent) error
```

### Recovery Logic

Track respawn attempts with an in-memory map keyed by agent ID + work
item ID:

```go
key := respawnKey{AgentID: agent.ID, WorkItemID: agent.HookItem}
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
func (w *Witness) respawnAgent(agent store.Agent) error
```

1. Ensure agent state is `working` in town store
2. Start a new tmux session:
   ```go
   sessionName := fmt.Sprintf("gt-%s-%s", w.config.Rig, agent.Name)
   workdir := config.WorktreePath(w.config.GTHome, w.config.Rig, agent.Name)
   cmd := "claude --dangerously-skip-permissions"
   ```
3. Emit `EventRespawn` event
4. Send `RECOVERY_NEEDED` protocol message to operator (informational)

**The witness does NOT re-sling or re-prime.** The hook file is durable,
and the Claude Code `SessionStart` hook fires `gt prime` automatically
(GUPP principle). Restarting the tmux session is sufficient.

### Return Work to Open

```go
func (w *Witness) returnWorkToOpen(agent store.Agent) error
```

1. Update work item: status → `open`, clear assignee
2. Clear the hook file: `os.Remove(hookPath)`
3. Set agent state → `idle`, clear hook_item
4. Clear respawn count for this key
5. Emit `EventStalled` event with `"recovered": false`
6. Send `RECOVERY_NEEDED` protocol message to operator

Hook file path: `$GT_HOME/{rig}/polecats/{agent.Name}/.hook`

Use the hook package's `Clear()` function if available, or
`os.Remove()` directly.

---

## Task 6: Zombie Detection and Cleanup

```go
// handleZombie handles a polecat with a live session but no hooked work.
func (w *Witness) handleZombie(agent store.Agent) error
```

### Zombie Criteria

An agent is a zombie if ALL of:
1. Agent state is `idle` (no hook_item in store)
2. Hook file does not exist on disk
3. A tmux session with `gt-{rig}-{name}` exists

### Cleanup

1. Stop the tmux session
2. Log the cleanup
3. Emit patrol event noting zombie cleanup

**Safety:** Only touch sessions matching `gt-{rig}-{name}` convention
for this rig.

---

## Task 7: Supervisor Integration

Extend the supervisor to handle witness agents.

### respawnCommand

```go
func respawnCommand(agent store.Agent) string {
    switch agent.Role {
    case "refinery":
        // Refinery is a Claude session (ADR-0005). Start it the same
        // way as a polecat — Claude handles the patrol loop using Go
        // CLI subcommands as tools.
        return "claude --dangerously-skip-permissions"
    case "witness":
        return fmt.Sprintf("gt witness run %s", agent.Rig)
    default:
        return "claude --dangerously-skip-permissions"
    }
}
```

### worktreeForAgent

```go
func worktreeForAgent(agent store.Agent, gtHome string) string {
    switch agent.Role {
    case "refinery":
        return config.RefineryWorktreePath(gtHome, agent.Rig)
    case "witness":
        // Witness is a Go process, not a worktree-based agent.
        // Use GT_HOME as working directory.
        return gtHome
    default:
        return config.WorktreePath(gtHome, agent.Rig, agent.Name)
    }
}
```

---

## Task 8: CLI Commands

Create `cmd/witness.go` following the refinery CLI pattern exactly.

### Commands

**`gt witness run <rig>`** — Foreground witness loop:
- Signal handling (SIGTERM, SIGINT)
- Opens town store and rig store
- Creates event logger
- Discovers source repo
- Runs witness patrol loop until cancelled

**`gt witness start <rig>`** — Background session:
- Starts tmux session `gt-{rig}-witness`
- Runs `gt witness run <rig>` inside session
- Output: `Witness started: gt-{rig}-witness`

**`gt witness stop <rig>`** — Stop session:
- Stops `gt-{rig}-witness` tmux session

**`gt witness attach <rig>`** — Attach to session:
- `syscall.Exec()` to tmux attach

### Registration

Register `witness` under root in `cmd/root.go`.

---

## Task 9: Status Integration

Extend `internal/status/status.go` to include witness state.

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
Witness WitnessInfo `json:"witness"`
```

### Updated Gather()

```go
witnessSession := fmt.Sprintf("gt-%s-witness", rig)
witnessAlive, _ := checker.IsAlive(witnessSession)
status.Witness = WitnessInfo{
    Running:     witnessAlive,
    SessionName: witnessSession,
}
```

### Updated Human Output

```
Rig: myrig
Supervisor: running (pid 12345)
Refinery: running (gt-myrig-refinery)
Witness: running (gt-myrig-witness)

AGENT      STATE     SESSION   WORK
Toast      working   alive     gt-a1b2c3d4: Implement login page
Jasper     idle      -         -

Merge Queue: 2 ready, 1 in progress, 0 failed
Summary: 2 agents (1 working, 1 idle, 0 stalled, 0 dead sessions)
Health: healthy
```

---

## Task 10: Tests

### Witness Unit Tests

Create `internal/witness/witness_test.go` with mock stores and mock
session checker:

```go
func TestPatrolHealthyAgents(t *testing.T)
    // 3 polecats working with live sessions, output changes each patrol
    // Patrol → no actions taken
    // Verify: patrol event with healthy=3, stalled=0

func TestPatrolDetectsStalled(t *testing.T)
    // 1 polecat working, dead session, hook_item set
    // Patrol → respawn attempted
    // Verify: session start called, respawn event emitted

func TestPatrolMaxRespawns(t *testing.T)
    // Polecat respawned MaxRespawns times already
    // Patrol → work returned to open
    // Verify: work item open, agent idle, hook cleared, event emitted

func TestPatrolDetectsZombie(t *testing.T)
    // Idle polecat, no hook, but live session
    // Patrol → session stopped
    // Verify: session stop called

func TestPatrolIgnoresIdleClean(t *testing.T)
    // Idle polecat, no hook, no session
    // Patrol → no action

func TestPatrolIgnoresNonPolecats(t *testing.T)
    // Agents with role=refinery and role=witness
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
// In witness.go:
type assessFunc func(agent store.Agent, sessionName, output string) (*AssessmentResult, error)

// In Witness struct:
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
   export GT_HOME=/tmp/gt-test
   bin/gt witness run testrig   # foreground, Ctrl+C to stop
   # In another terminal:
   bin/gt status testrig        # should show witness running
   bin/gt feed --type=patrol    # should show patrol events
   bin/gt feed --type=assess    # should show assessments (if triggered)
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Guidelines

- The witness is **primarily a Go process**. The AI assessment is a
  targeted call-out, not a persistent AI session. The patrol loop,
  state detection, respawn logic, and zombie cleanup are all
  deterministic Go code.
- **AI assessment fires only when the heuristic triggers** — no tmux
  output change since the last patrol. This keeps AI costs low while
  catching stuck agents that a pure heuristic would miss.
- **Assessment failure is non-blocking.** If the AI call times out,
  returns garbage, or fails entirely, the witness logs a warning and
  continues its patrol. The assessment is additive — the mechanical
  checks (session liveness, stall detection) work regardless.
- **Low confidence = no action.** When the AI is unsure, wait another
  patrol cycle. The cost of a false nudge (interrupting a working
  agent) exceeds the cost of waiting 3 more minutes.
- **`claude -p`** (Claude Code print mode) is the default assessment
  command. It uses whatever auth the operator has configured. The
  command is configurable via `AssessCommand` for operators who want
  to use a different tool or model.
- **Respawn tracking is in-memory.** If the witness restarts, counts
  reset. This is acceptable — witness restarts are rare, and resetting
  gives agents another chance.
- The witness **does not re-sling or re-prime** crashed agents. It
  only restarts the tmux session. GUPP handles the rest.
- One witness per rig. Agent ID: `{rig}/witness`.
- All Loop 0, 1, and 2 tests must continue to pass.
- Commit after tests pass with message:
  `feat(witness): add per-rig health monitor with AI-assisted assessment`
