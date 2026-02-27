# Prompt 02: Loop 5 — Handoff

You are extending the `sol` orchestration system with a handoff mechanism
for session continuity. When an agent approaches its context limit or
has been running for a long time, it calls `sol handoff` to save its
current state, stop the session, and immediately restart with the
preserved context injected. This allows long-running work to continue
across multiple sessions without losing progress.

**Working directory:** `~/sol-src/`
**Prerequisite:** Loop 5 prompt 01 (escalation system) is complete.

Read all existing code first. Understand the dispatch package
(`internal/dispatch/dispatch.go` — especially `Prime()` and `Done()`),
the session manager (`internal/session/manager.go` — `Start()`,
`Stop()`, `Capture()`), the tether package (`internal/tether/`), the
workflow package (`internal/workflow/`), and the protocol package
(`internal/protocol/claudemd.go`).

Read `docs/target-architecture.md` Loop 5 definition of done (item 7:
handoff for session continuity).

---

## Task 1: Handoff Package

Create `internal/handoff/` package with state capture, file I/O, and
execution logic.

### Handoff State

```go
// internal/handoff/handoff.go
package handoff

import "time"

// State captures an agent's context at the moment of handoff.
type State struct {
    WorkItemID      string    `json:"work_item_id"`
    AgentName       string    `json:"agent_name"`
    World             string    `json:"world"`
    PreviousSession string    `json:"previous_session"`
    Summary         string    `json:"summary"`          // agent-provided or auto-generated
    RecentOutput    string    `json:"recent_output"`     // last N lines of tmux output
    RecentCommits   []string  `json:"recent_commits"`    // recent git log --oneline
    WorkflowStep    string    `json:"workflow_step"`     // current step ID (empty if no workflow)
    WorkflowProgress string  `json:"workflow_progress"` // e.g., "2/3 complete"
    HandedOffAt     time.Time `json:"handed_off_at"`
}
```

### File Paths

```go
// HandoffPath returns the path to an agent's handoff state file.
// $SOL_HOME/{world}/outposts/{agentName}/.handoff.json
func HandoffPath(world, agentName string) string

// HasHandoff returns true if a handoff file exists for this agent.
func HasHandoff(world, agentName string) bool
```

### State Capture

```go
// CaptureOpts configures what to capture during handoff.
type CaptureOpts struct {
    World         string
    AgentName   string
    Summary     string // agent-provided summary (optional)
    CaptureLines int   // lines of tmux output to capture (default: 100)
    CommitCount  int   // recent commits to include (default: 10)
}

// Capture gathers the current state of an agent's session.
// 1. Read tether file to get work item ID
// 2. Determine session name: sol-{world}-{agentName}
// 3. Capture tmux output (last CaptureLines lines via session manager)
// 4. Capture recent git commits from the agent's worktree
// 5. Read workflow state (if present): current step and progress
// 6. If no agent-provided summary, generate a basic one from context
//
// The sessionCapture function is injected to avoid depending on the
// session manager directly:
//   func(sessionName string, lines int) (string, error)
//
// The gitLog function captures recent commits:
//   func(worktreeDir string, count int) ([]string, error)
//
// Returns the captured State.
func Capture(opts CaptureOpts, sessionCapture func(string, int) (string, error),
    gitLog func(string, int) ([]string, error)) (*State, error)
```

### File I/O

```go
// Write serializes the handoff state to the agent's handoff file.
// Creates parent directories if needed.
func Write(state *State) error

// Read deserializes the handoff state from the agent's handoff file.
// Returns nil, nil if no handoff file exists.
func Read(world, agentName string) (*State, error)

// Remove deletes the handoff file. No-op if it doesn't exist.
func Remove(world, agentName string) error
```

### Git Log Helper

```go
// GitLog returns the last N commit summaries from a git worktree.
// Runs: git -C <dir> log --oneline -<count>
// Returns one entry per line as a string slice.
// Returns empty slice if the directory has no commits.
func GitLog(worktreeDir string, count int) ([]string, error)
```

This runs `git` as a subprocess. It's used by `Capture` as the `gitLog`
function.

### Execution

```go
// ExecOpts configures the handoff execution.
type ExecOpts struct {
    World       string
    AgentName string
    Summary   string // optional agent-provided summary
}

// Exec performs the full handoff sequence:
// 1. Capture current state (Capture)
// 2. Write handoff file (Write)
// 3. Send handoff mail to self via sphere store (for audit trail)
// 4. Stop the current tmux session
// 5. Start a new tmux session with the same worktree and command
//
// The new session will fire the session-start tether (sol prime), which
// detects the handoff file and injects the preserved context.
//
// sessionManager provides Stop/Start/Capture operations.
// sphereStore provides SendMessage for the handoff mail.
// logger is optional (nil-safe) for event emission.
//
// Returns error if any critical step fails. The handoff file is written
// before the session is stopped, so if the restart fails, the prefect
// will eventually respawn the agent and the handoff context will still
// be available.
func Exec(opts ExecOpts, sessionMgr SessionManager, sphereStore SphereStore,
    logger *events.Logger) error
```

### Interfaces

```go
// SessionManager is the subset of session.Manager used by handoff.
type SessionManager interface {
    Capture(name string, lines int) (string, error)
    Stop(name string, force bool) error
    Start(name, workdir, cmd string, env map[string]string, role, world string) error
}

// SphereStore is the subset of store.Store used by handoff.
type SphereStore interface {
    SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)
}
```

### Exec Implementation Details

Step 3 — Handoff mail:
- Sender: agent ID (`{world}/{agentName}`)
- Recipient: agent ID (sends to self for audit)
- Subject: `"HANDOFF: {workItemID}"`
- Body: summary + recent commits + workflow progress
- Priority: 2 (normal)
- Type: `"notification"`

Step 4 — Stop session:
- Session name: `sol-{world}-{agentName}`
- Use `force: false` (graceful stop via Ctrl-C)

Step 5 — Start new session:
- Session name: same as before
- Workdir: `$SOL_HOME/{world}/outposts/{agentName}/world` (existing worktree)
- Command: `claude --dangerously-skip-permissions`
- Env: `SOL_HOME`, `SOL_WORLD={world}`, `SOL_AGENT={agentName}`
- Role: existing role (read from agent record or use "outpost")
- World: same world

The tether file is NOT cleared — the agent is still working on the same
item. The new session picks up via `sol prime` which reads the tether and
handoff file.

---

## Task 2: Extend Prime for Handoff Context

Modify `Prime()` in `internal/dispatch/dispatch.go` to detect and inject
handoff context.

### Prime Changes

After reading the tether file and work item, check for a handoff file:

```go
// Check for handoff context (session continuity).
handoffState, err := handoff.Read(world, agentName)
if err != nil {
    return nil, fmt.Errorf("failed to read handoff state: %w", err)
}

if handoffState != nil {
    result, err := primeWithHandoff(world, agentName, item, handoffState)
    if err != nil {
        return nil, err
    }
    // Clean up handoff file after successful injection.
    handoff.Remove(world, agentName)
    return result, nil
}
```

This check goes BEFORE the workflow check. Handoff takes priority
because it contains the most recent context (including workflow state).

### Handoff Prime Output

```go
func primeWithHandoff(world, agentName string, item *store.WorkItem,
    state *handoff.State) (*PrimeResult, error) {

    output := fmt.Sprintf(`=== HANDOFF CONTEXT ===
Agent: %s (world: %s)
Work Item: %s
Title: %s

This is a continuation of a previous session. The previous session
handed off to preserve context.

--- PREVIOUS SESSION SUMMARY ---
%s
--- END SUMMARY ---

--- RECENT COMMITS ---
%s
--- END COMMITS ---
`, agentName, world, item.ID, item.Title, state.Summary, strings.Join(state.RecentCommits, "\n"))

    // Add workflow context if the agent has an active workflow.
    if state.WorkflowStep != "" {
        output += fmt.Sprintf(`
Workflow progress: %s (current step: %s)
Read your current step: sol workflow current --world=%s --agent=%s

`, state.WorkflowProgress, state.WorkflowStep, world, agentName)
    }

    output += fmt.Sprintf(`Continue from where the previous session left off.
When complete, run: sol resolve
If you need to hand off again: sol handoff --summary="<what you've done>"
=== END HANDOFF ===`)

    return &PrimeResult{Output: output}, nil
}
```

---

## Task 3: CLAUDE.md Extension

When generating CLAUDE.md in `internal/protocol/claudemd.go`, add the
handoff command to the Commands section for all agents (not just
workflow agents):

```markdown
## Session Management
- `sol handoff` — Hand off to a fresh session (preserves context)
- `sol handoff --summary="what I've done so far"` — Hand off with a summary
```

Add this to the `GenerateClaudeMD` function output. This is always
included — handoff is available to all agents regardless of whether they
have a workflow.

---

## Task 4: CLI Command

Create `cmd/handoff.go`:

```
sol handoff [--summary="<summary>"] [--world=<world>] [--agent=<name>]
```

- `--summary` (optional): agent-provided summary of current state
- `--world` (required): world name (also from `SOL_WORLD` env var)
- `--agent` (required): agent name (also from `SOL_AGENT` env var)

**Behavior:**
1. Resolve world and agent from flags or environment variables
2. Call `handoff.Exec()`
3. If successful: print `Handoff complete. New session starting.`
4. The current process is inside the tmux session being stopped, so
   this output may not be visible — that's OK.

**Errors:** missing world/agent, no tether file (not working) → print error,
exit 1.

**Environment variables:**
- `SOL_WORLD`: default world (set by session start env)
- `SOL_AGENT`: default agent name (set by session start env)

These are already set in the session environment by `Cast()`. The
`sol handoff` command should prefer flag values but fall back to env vars.

---

## Task 5: Event Types

Add handoff event type to `internal/events/events.go`:

```go
const (
    EventHandoff = "handoff"
)
```

Add formatter case in `cmd/feed.go`'s `formatEventDescription`:

```go
case events.EventHandoff:
    return fmt.Sprintf("Agent %s handed off: %s", get("agent"), get("work_item_id"))
```

Emit from `handoff.Exec()` after successfully writing the handoff file
(before stopping the session).

---

## Task 6: Tests

### Handoff Package Tests

Create `internal/handoff/handoff_test.go`:

```go
func TestCapture(t *testing.T)
    // Set up SOL_HOME, tether file, workflow state
    // Mock session capture → returns fake output
    // Mock git log → returns fake commits
    // Capture → State has correct fields

func TestCaptureNoWorkflow(t *testing.T)
    // No workflow directory → WorkflowStep empty

func TestCaptureNoSummary(t *testing.T)
    // No summary provided → auto-generated from context

func TestWriteAndRead(t *testing.T)
    // Write state → Read → matches original
    // Verify JSON file on disk is valid

func TestReadNoFile(t *testing.T)
    // No handoff file → nil, nil

func TestRemove(t *testing.T)
    // Write → Remove → Read returns nil
    // Remove non-existent → no error

func TestHasHandoff(t *testing.T)
    // No file → false
    // Write → true

func TestGitLog(t *testing.T)
    // Create temp git repo with commits
    // GitLog(dir, 3) → returns 3 most recent
    // Empty repo → empty slice

func TestExec(t *testing.T)
    // Set up SOL_HOME with tether file and stores
    // Mock session manager (record Stop/Start calls)
    // Mock sphere store (record SendMessage calls)
    // Exec → handoff file written
    // Exec → session stopped then started
    // Exec → mail sent to self
    // Verify new session started with same worktree

func TestExecNoHook(t *testing.T)
    // Agent not working (no tether file) → error
```

### Prime Handoff Tests

Add to existing dispatch tests or create new:

```go
func TestPrimeWithHandoff(t *testing.T)
    // Set up tether file and handoff file
    // Prime() → output contains handoff context
    // Handoff file deleted after prime

func TestPrimeHandoffTakesPriority(t *testing.T)
    // Set up tether file, handoff file, AND workflow
    // Prime() → output contains handoff context (not workflow)
    // Handoff file deleted

func TestPrimeNoHandoff(t *testing.T)
    // Set up tether file, no handoff
    // Prime() → standard output (backwards compatible)
```

### CLI Smoke Tests

Add to `test/integration/cli_loop5_test.go`:

```go
func TestCLIHandoffHelp(t *testing.T)
```

---

## Task 7: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   mkdir -p /tmp/sol-test/myworld/outposts/Toast

   # Simulate an agent with work tethered
   echo "sol-abc12345" > /tmp/sol-test/myworld/outposts/Toast/.tether

   # Write a fake handoff file
   cat > /tmp/sol-test/myworld/outposts/Toast/.handoff.json << 'EOF'
   {
     "work_item_id": "sol-abc12345",
     "agent_name": "Toast",
     "world": "myworld",
     "previous_session": "sol-myworld-Toast",
     "summary": "Implemented login form. Tests passing. Starting on validation.",
     "recent_output": "All tests passed.\n$",
     "recent_commits": ["abc1234 feat: add login form", "def5678 test: add login tests"],
     "workflow_step": "implement",
     "workflow_progress": "1/3 complete",
     "handed_off_at": "2026-02-27T10:30:00Z"
   }
   EOF

   # Test prime with handoff (requires store setup)
   # The handoff file should be consumed by prime
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- The handoff is a **voluntary restart** — the agent decides when to
  hand off. This is different from a crash (involuntary) which is
  handled by the prefect.
- The handoff file is the signal that distinguishes handoff from crash.
  Prime checks for it before workflow state. After injecting handoff
  context, the file is deleted so subsequent primes (if the new session
  crashes) use normal workflow injection.
- The tether file is NOT cleared during handoff — the agent is still
  working on the same item. The new session inherits the same tether,
  worktree, and workflow state.
- The handoff mail is for audit trail only. It lets operators and the
  consul see that a handoff occurred. The actual context is in the
  `.handoff.json` file.
- If `Exec` fails after writing the handoff file but before restarting,
  the prefect will eventually respawn the agent (it detects a dead
  session with tethered work). The handoff context will be available to
  the respawned session.
- `GitLog` runs `git` as a subprocess. It should handle the case where
  the worktree directory doesn't exist or has no commits gracefully
  (return empty slice, not error).
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(handoff): add session continuity with context preservation`
