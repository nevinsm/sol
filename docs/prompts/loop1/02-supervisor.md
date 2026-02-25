# Prompt 02: Loop 1 — Supervisor

You are building the supervisor for the `gt` orchestration system. The
supervisor is a town-level foreground Go process that monitors all agent
sessions across all rigs, detects crashes, and restarts them with backoff.
It is the core new component of Loop 1.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 01 (name pool + dispatch serialization) is complete.

Read all existing code first. Understand the session manager
(`internal/session/manager.go`), agent states in the store
(`internal/store/agents.go`), hook read/write/clear
(`internal/hook/hook.go`), dispatch sling/prime/done
(`internal/dispatch/dispatch.go`), and the config package
(`internal/config/config.go`).

Read `docs/target-architecture.md` Section 3.6 (Supervisor) and Section
5 (Loop 1 requirements) for design context. Note: the architecture doc
shows `gt supervisor start` as a background process — we are implementing
it as `gt supervisor run` (foreground, blocks until interrupted). Daemon
mode is deferred to a later loop.

---

## Task 1: Supervisor Package

Create `internal/supervisor/` — the supervisor monitors agent session
liveness across all rigs and restarts crashed sessions.

### Core Struct and Interface

```go
// internal/supervisor/supervisor.go
package supervisor

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/nevinsm/gt/internal/session"
    "github.com/nevinsm/gt/internal/store"
)

// SessionManager abstracts tmux operations for testing.
type SessionManager interface {
    Exists(name string) bool
    Start(name, workdir, cmd string, env map[string]string, role, rig string) error
    Stop(name string, force bool) error
    List() ([]session.SessionInfo, error)
}

// Supervisor monitors agent sessions and restarts crashed ones.
// It is town-level: one supervisor watches all rigs.
type Supervisor struct {
    townStore  *store.Store
    sessions   SessionManager
    logger     *slog.Logger
    cfg        Config

    mu             sync.Mutex
    degraded       bool
    degradedSince  time.Time
    deathTimes     []time.Time    // timestamps of recent session deaths
    backoff        map[string]int // agent ID -> consecutive restart count
}

// Config holds supervisor configuration.
type Config struct {
    HeartbeatInterval  time.Duration  // default: 3 minutes
    MassDeathThreshold int            // default: 3 deaths in 30 seconds
    MassDeathWindow    time.Duration  // default: 30 seconds
    DegradedCooldown   time.Duration  // default: 5 minutes
}

func DefaultConfig() Config
func New(cfg Config, townStore *store.Store, mgr SessionManager, logger *slog.Logger) *Supervisor
func (s *Supervisor) Run(ctx context.Context) error  // Blocks until ctx cancelled
func (s *Supervisor) IsDegraded() bool
```

The supervisor is town-level — it monitors agents across **all** rigs.
Each agent record includes its rig, so the supervisor can derive session
names and worktree paths per-agent.

**Required store change:** The existing `store.ListAgents(rig, state)`
always filters by rig (`WHERE rig = ?`). Modify it so that when `rig`
is empty, it omits the rig filter and returns agents across all rigs.
This lets the supervisor call `townStore.ListAgents("", "working")`.

### PID File Guard

Only one supervisor may run. PID file at
`config.RuntimeDir() + "/supervisor.pid"`.

```go
// internal/supervisor/pidfile.go
package supervisor

func WritePID() error           // Error if already running (PID alive)
func ReadPID() (int, error)     // Returns 0 if no file
func ClearPID() error
func IsRunning(pid int) bool    // syscall.Kill(pid, 0)
```

**WritePID guard:** read existing PID → if alive, error
`"supervisor already running (pid %d)"` → if dead, overwrite
(stale) → write `os.Getpid()`.

### Heartbeat Loop

`Run()` writes the PID file, defers `ClearPID`, runs one immediate
heartbeat, then ticks every `HeartbeatInterval`. On context cancellation,
calls `shutdown()`.

Each heartbeat:
1. List all agents with state `"working"` (all rigs):
   `townStore.ListAgents("", "working")`
2. For each, check `sessions.Exists(dispatch.SessionName(agent.Rig, agent.Name))`
3. If session dead: record death time, check mass death, respawn with
   backoff (or set `"stalled"` if deferred/degraded)
4. Reset backoff for agents whose state changed to `"idle"` since last tick
5. Prune `deathTimes` older than the mass-death window

### Respawn Logic

When respawning a crashed agent:
1. Compute paths via `dispatch.SessionName(agent.Rig, agent.Name)` and
   `dispatch.WorktreePath(agent.Rig, agent.Name)`
2. If worktree missing → log warning, set agent `"idle"`, clear hook, return
3. Start tmux session in the existing worktree:
   ```go
   mgr.Start(sessionName, worktreePath,
       "claude --dangerously-skip-permissions",
       map[string]string{
           "GT_HOME":  config.Home(),
           "GT_RIG":   agent.Rig,
           "GT_AGENT": agent.Name,
       },
       "polecat", agent.Rig)
   ```
4. Set agent state back to `"working"`, log with agent name + restart count

The supervisor does NOT need a source repo — it only restarts sessions
in worktrees that were already created by `gt sling`. The session-start
hook (installed during the original sling) fires `gt prime`, re-injecting
the work context. This is GUPP — hook persists, agent resumes.

### Backoff

| Restart # | Delay |
|-----------|-------|
| 1         | 0s    |
| 2         | 30s   |
| 3         | 1m    |
| 4         | 2m    |
| 5+        | 5m    |

```go
func backoffDuration(consecutiveRestarts int) time.Duration
```

The supervisor doesn't sleep during backoff — it checks on each tick
whether enough time has passed. Agent stays `"stalled"` until respawned.

### Mass-Death Detection

3+ deaths in 30 seconds → degraded mode (no respawns). Log at ERROR:
`"mass death detected"`. Recovery: 5 minutes with no new deaths →
auto-exit degraded mode.

```go
func (s *Supervisor) checkMassDeath() bool
func (s *Supervisor) checkDegradedRecovery()
```

### Shutdown

On context cancellation: stop all live sessions for working agents
across all rigs (`force=false`), set agents to `"stalled"` (hooks
persist for recovery), log summary.

---

## Task 2: Structured Logging

```go
// internal/supervisor/logging.go
package supervisor

// NewLogger creates an slog.Logger writing JSON to path.
// If path is empty, logs to stderr.
// Opens file with O_CREATE|O_APPEND|O_WRONLY.
func NewLogger(path string) (*slog.Logger, *os.File, error)
```

Use structured fields in all log calls:
```go
s.logger.Info("heartbeat", "working_agents", N, "dead_sessions", N)
s.logger.Info("respawned session", "agent", name, "rig", rig, "work_item", id, "restart", count)
s.logger.Error("mass death detected", "deaths", N, "window", duration)
```

---

## Task 3: CLI Commands

### `gt supervisor run`

```
gt supervisor run
```

Starts the supervisor in the foreground. Monitors all rigs. The process
runs until interrupted (SIGTERM/SIGINT).

```go
// cmd/supervisor.go
var supervisorCmd = &cobra.Command{
    Use:   "supervisor",
    Short: "Manage the gt supervisor",
}

var supervisorRunCmd = &cobra.Command{
    Use:   "run",
    Short: "Run the supervisor (foreground)",
    Args:  cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        logPath := filepath.Join(config.RuntimeDir(), "supervisor.log")
        logger, logFile, err := supervisor.NewLogger(logPath)
        if err != nil {
            return fmt.Errorf("failed to create logger: %w", err)
        }
        defer logFile.Close()

        townStore, err := store.OpenTown()
        if err != nil { return err }
        defer townStore.Close()

        mgr := session.New()
        cfg := supervisor.DefaultConfig()
        sup := supervisor.New(cfg, townStore, mgr, logger)

        // Signal handling
        ctx, cancel := context.WithCancel(cmd.Context())
        defer cancel()
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
        go func() { <-sigCh; cancel() }()

        fmt.Fprintf(os.Stderr, "Supervisor started (pid %d)\n", os.Getpid())
        fmt.Fprintf(os.Stderr, "Log: %s\n", logPath)
        return sup.Run(ctx)
    },
}
```

### `gt supervisor stop`

```
gt supervisor stop
```

Reads the PID file, sends SIGTERM to the supervisor process.

```go
// cmd/supervisor.go (continued)
var supervisorStopCmd = &cobra.Command{
    Use:   "stop",
    Short: "Stop the running supervisor",
    Args:  cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        // ReadPID() -> if 0, "no supervisor running"
        // if !IsRunning(pid), clear stale PID, report
        // else send syscall.SIGTERM, print confirmation
    },
}

func init() {
    rootCmd.AddCommand(supervisorCmd)
    supervisorCmd.AddCommand(supervisorRunCmd)
    supervisorCmd.AddCommand(supervisorStopCmd)
}
```

---

## Task 4: Tests

### PID File Tests (`internal/supervisor/pidfile_test.go`)

```go
func TestWriteAndReadPID(t *testing.T)     // Write, read, clear, read again
func TestWritePIDAlreadyRunning(t *testing.T) // Current PID -> error "already running"
func TestWritePIDStalePID(t *testing.T)    // Dead PID 99999 -> overwrite succeeds
func TestIsRunning(t *testing.T)           // os.Getpid() -> true, 99999 -> false
```

### Supervisor Logic Tests (`internal/supervisor/supervisor_test.go`)

Mock session manager:
```go
type mockSessions struct {
    mu      sync.Mutex
    alive   map[string]bool
    started []string
    stopped []string
}
// Implements SessionManager + Kill(name) test helper
```

Test cases:
```go
func TestHeartbeatDetectsDead(t *testing.T)     // Working agent, dead session -> respawn
func TestHeartbeatIgnoresIdle(t *testing.T)     // Idle agent -> no action
func TestHeartbeatMultipleRigs(t *testing.T)    // Agents in different rigs all monitored
func TestBackoffEscalation(t *testing.T)        // Verify backoff schedule
func TestMassDeathDetection(t *testing.T)       // 3 deaths in 30s -> degraded
func TestMassDeathRecovery(t *testing.T)        // 5min quiet -> exits degraded
func TestDegradedModeSkipsRespawn(t *testing.T) // Degraded -> no respawns
func TestShutdownStopsSessions(t *testing.T)    // shutdown() stops all live sessions
func TestBackoffReset(t *testing.T)             // Agent goes idle -> backoff clears
```

Backoff unit test with table-driven cases:
```go
func TestBackoffDuration(t *testing.T) {
    // {1, 0}, {2, 30s}, {3, 1m}, {4, 2m}, {5, 5m}, {6, 5m}, {100, 5m}
}
```

Test the heartbeat directly (export it or use short intervals) to avoid
slow tests.

---

## Task 5: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export GT_HOME=/tmp/gt-test
   bin/gt store create --db=testrig --title="Supervised task"
   bin/gt sling <id> testrig
   bin/gt supervisor run          # in another terminal
   cat /tmp/gt-test/.runtime/supervisor.pid
   tmux kill-session -t gt-testrig-<agent>   # supervisor should restart it
   bin/gt supervisor stop
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Deferred to Later Loops

The following architecture features are **intentionally deferred**:

- **Heartbeat files** (`$GT_HOME/.runtime/heartbeats/`): The architecture
  describes agents writing heartbeat files and the supervisor checking
  freshness. Loop 1 uses tmux session existence only (crash detection).
  Stale/hung agent detection via heartbeat freshness is deferred.
- **Deacon triage**: The architecture includes deacon monitoring in the
  supervisor's heartbeat loop. The deacon itself is a Loop 5 feature, so
  triage is deferred until then.
- **`gt supervisor status`**: Covered by `gt status` (prompt 03).
- **`gt supervisor logs`**: The operator can `tail -f` the log file
  directly.

---

## Guidelines

- Foreground process only. No daemonization — deferred to a later loop.
- The supervisor is town-level — one instance monitors all rigs. It does
  NOT take a rig argument.
- The supervisor does NOT manage dispatch decisions. It only monitors
  and restarts sessions that die.
- The supervisor does NOT need a source repo. It respawns into existing
  worktrees created by `gt sling`.
- Use `dispatch.SessionName()` and `dispatch.WorktreePath()` — don't
  duplicate path logic.
- The supervisor must be safe to restart: read agent states from store,
  check sessions, respawn as needed. No in-memory state that can't be
  re-derived.
- Commit after tests pass with message:
  `feat(supervisor): add supervisor with heartbeat, backoff, and mass-death detection`
