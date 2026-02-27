# Prompt 02: Loop 1 — Prefect

You are building the prefect for the `sol` orchestration system. The
prefect is a sphere-level foreground Go process that monitors all agent
sessions across all worlds, detects crashes, and restarts them with backoff.
It is the core new component of Loop 1.

**Working directory:** `~/sol-src/`
**Prerequisite:** Prompt 01 (name pool + dispatch serialization) is complete.

Read all existing code first. Understand the session manager
(`internal/session/manager.go`), agent states in the store
(`internal/store/agents.go`), tether read/write/clear
(`internal/tether/tether.go`), dispatch cast/prime/done
(`internal/dispatch/dispatch.go`), and the config package
(`internal/config/config.go`).

Read `docs/target-architecture.md` Section 3.6 (Prefect) and Section
5 (Loop 1 requirements) for design context. Note: the architecture doc
shows `sol prefect start` as a background process — we are implementing
it as `sol prefect run` (foreground, blocks until interrupted). Daemon
mode is deferred to a later loop.

---

## Task 1: Prefect Package

Create `internal/prefect/` — the prefect monitors agent session
liveness across all worlds and restarts crashed sessions.

### Core Struct and Interface

```go
// internal/prefect/prefect.go
package prefect

import (
    "context"
    "log/slog"
    "sync"
    "time"

    "github.com/nevinsm/sol/internal/session"
    "github.com/nevinsm/sol/internal/store"
)

// SessionManager abstracts tmux operations for testing.
type SessionManager interface {
    Exists(name string) bool
    Start(name, workdir, cmd string, env map[string]string, role, world string) error
    Stop(name string, force bool) error
    List() ([]session.SessionInfo, error)
}

// Prefect monitors agent sessions and restarts crashed ones.
// It is sphere-level: one prefect watches all worlds.
type Prefect struct {
    sphereStore  *store.Store
    sessions   SessionManager
    logger     *slog.Logger
    cfg        Config

    mu             sync.Mutex
    degraded       bool
    degradedSince  time.Time
    deathTimes     []time.Time    // timestamps of recent session deaths
    backoff        map[string]int // agent ID -> consecutive restart count
}

// Config holds prefect configuration.
type Config struct {
    HeartbeatInterval  time.Duration  // default: 3 minutes
    MassDeathThreshold int            // default: 3 deaths in 30 seconds
    MassDeathWindow    time.Duration  // default: 30 seconds
    DegradedCooldown   time.Duration  // default: 5 minutes
}

func DefaultConfig() Config
func New(cfg Config, sphereStore *store.Store, mgr SessionManager, logger *slog.Logger) *Prefect
func (s *Prefect) Run(ctx context.Context) error  // Blocks until ctx cancelled
func (s *Prefect) IsDegraded() bool
```

The prefect is sphere-level — it monitors agents across **all** worlds.
Each agent record includes its world, so the prefect can derive session
names and worktree paths per-agent.

**Required store change:** The existing `store.ListAgents(world, state)`
always filters by world (`WHERE world = ?`). Modify it so that when `world`
is empty, it omits the world filter and returns agents across all worlds.
This lets the prefect call `sphereStore.ListAgents("", "working")`.

### PID File Guard

Only one prefect may run. PID file at
`config.RuntimeDir() + "/prefect.pid"`.

```go
// internal/prefect/pidfile.go
package prefect

func WritePID() error           // Error if already running (PID alive)
func ReadPID() (int, error)     // Returns 0 if no file
func ClearPID() error
func IsRunning(pid int) bool    // syscall.Kill(pid, 0)
```

**WritePID guard:** read existing PID → if alive, error
`"prefect already running (pid %d)"` → if dead, overwrite
(stale) → write `os.Getpid()`.

### Heartbeat Loop

`Run()` writes the PID file, defers `ClearPID`, runs one immediate
heartbeat, then ticks every `HeartbeatInterval`. On context cancellation,
calls `shutdown()`.

Each heartbeat:
1. List all agents with state `"working"` (all worlds):
   `sphereStore.ListAgents("", "working")`
2. For each, check `sessions.Exists(dispatch.SessionName(agent.World, agent.Name))`
3. If session dead: record death time, check mass death, respawn with
   backoff (or set `"stalled"` if deferred/degraded)
4. Reset backoff for agents whose state changed to `"idle"` since last tick
5. Prune `deathTimes` older than the mass-death window

### Respawn Logic

When respawning a crashed agent:
1. Compute paths via `dispatch.SessionName(agent.World, agent.Name)` and
   `dispatch.WorktreePath(agent.World, agent.Name)`
2. If worktree missing → log warning, set agent `"idle"`, clear tether, return
3. Start tmux session in the existing worktree:
   ```go
   mgr.Start(sessionName, worktreePath,
       "claude --dangerously-skip-permissions",
       map[string]string{
           "SOL_HOME":  config.Home(),
           "SOL_WORLD":   agent.World,
           "SOL_AGENT": agent.Name,
       },
       "outpost", agent.World)
   ```
4. Set agent state back to `"working"`, log with agent name + restart count

The prefect does NOT need a source repo — it only restarts sessions
in worktrees that were already created by `sol cast`. The session-start
tether (installed during the original cast) fires `sol prime`, re-injecting
the work context. This is GUPP — tether persists, agent resumes.

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

The prefect doesn't sleep during backoff — it checks on each tick
whether enough time has passed. Agent stays `"stalled"` until respawned.

### Mass-Death Detection

3+ deaths in 30 seconds → degraded mode (no respawns). Log at ERROR:
`"mass death detected"`. Recovery: 5 minutes with no new deaths →
auto-exit degraded mode.

```go
func (s *Prefect) checkMassDeath() bool
func (s *Prefect) checkDegradedRecovery()
```

### Shutdown

On context cancellation: stop all live sessions for working agents
across all worlds (`force=false`), set agents to `"stalled"` (tethers
persist for recovery), log summary.

---

## Task 2: Structured Logging

```go
// internal/prefect/logging.go
package prefect

// NewLogger creates an slog.Logger writing JSON to path.
// If path is empty, logs to stderr.
// Opens file with O_CREATE|O_APPEND|O_WRONLY.
func NewLogger(path string) (*slog.Logger, *os.File, error)
```

Use structured fields in all log calls:
```go
s.logger.Info("heartbeat", "working_agents", N, "dead_sessions", N)
s.logger.Info("respawned session", "agent", name, "world", world, "work_item", id, "restart", count)
s.logger.Error("mass death detected", "deaths", N, "window", duration)
```

---

## Task 3: CLI Commands

### `sol prefect run`

```
sol prefect run
```

Starts the prefect in the foreground. Monitors all worlds. The process
runs until interrupted (SIGTERM/SIGINT).

```go
// cmd/prefect.go
var supervisorCmd = &cobra.Command{
    Use:   "prefect",
    Short: "Manage the sol prefect",
}

var supervisorRunCmd = &cobra.Command{
    Use:   "run",
    Short: "Run the prefect (foreground)",
    Args:  cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        logPath := filepath.Join(config.RuntimeDir(), "prefect.log")
        logger, logFile, err := prefect.NewLogger(logPath)
        if err != nil {
            return fmt.Errorf("failed to create logger: %w", err)
        }
        defer logFile.Close()

        sphereStore, err := store.OpenSphere()
        if err != nil { return err }
        defer sphereStore.Close()

        mgr := session.New()
        cfg := prefect.DefaultConfig()
        sup := prefect.New(cfg, sphereStore, mgr, logger)

        // Signal handling
        ctx, cancel := context.WithCancel(cmd.Context())
        defer cancel()
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
        go func() { <-sigCh; cancel() }()

        fmt.Fprintf(os.Stderr, "Prefect started (pid %d)\n", os.Getpid())
        fmt.Fprintf(os.Stderr, "Log: %s\n", logPath)
        return sup.Run(ctx)
    },
}
```

### `sol prefect stop`

```
sol prefect stop
```

Reads the PID file, sends SIGTERM to the prefect process.

```go
// cmd/prefect.go (continued)
var supervisorStopCmd = &cobra.Command{
    Use:   "stop",
    Short: "Stop the running prefect",
    Args:  cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        // ReadPID() -> if 0, "no prefect running"
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

### PID File Tests (`internal/prefect/pidfile_test.go`)

```go
func TestWriteAndReadPID(t *testing.T)     // Write, read, clear, read again
func TestWritePIDAlreadyRunning(t *testing.T) // Current PID -> error "already running"
func TestWritePIDStalePID(t *testing.T)    // Dead PID 99999 -> overwrite succeeds
func TestIsRunning(t *testing.T)           // os.Getpid() -> true, 99999 -> false
```

### Prefect Logic Tests (`internal/prefect/supervisor_test.go`)

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
func TestHeartbeatMultipleWorlds(t *testing.T)    // Agents in different worlds all monitored
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
   export SOL_HOME=/tmp/sol-test
   bin/sol store create --world=testrig --title="Supervised task"
   bin/sol cast <id> testrig
   bin/sol prefect run          # in another terminal
   cat /tmp/sol-test/.runtime/prefect.pid
   tmux kill-session -t sol-testrig-<agent>   # prefect should restart it
   bin/sol prefect stop
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Deferred to Later Loops

The following architecture features are **intentionally deferred**:

- **Heartbeat files** (`$SOL_HOME/.runtime/heartbeats/`): The architecture
  describes agents writing heartbeat files and the prefect checking
  freshness. Loop 1 uses tmux session existence only (crash detection).
  Stale/hung agent detection via heartbeat freshness is deferred.
- **Consul triage**: The architecture includes consul monitoring in the
  prefect's heartbeat loop. The consul itself is a Loop 5 feature, so
  triage is deferred until then.
- **`sol prefect status`**: Covered by `sol status` (prompt 03).
- **`sol prefect logs`**: The operator can `tail -f` the log file
  directly.

---

## Guidelines

- Foreground process only. No daemonization — deferred to a later loop.
- The prefect is sphere-level — one instance monitors all worlds. It does
  NOT take a world argument.
- The prefect does NOT manage dispatch decisions. It only monitors
  and restarts sessions that die.
- The prefect does NOT need a source repo. It respawns into existing
  worktrees created by `sol cast`.
- Use `dispatch.SessionName()` and `dispatch.WorktreePath()` — don't
  duplicate path logic.
- The prefect must be safe to restart: read agent states from store,
  check sessions, respawn as needed. No in-memory state that can't be
  re-derived.
- Commit after tests pass with message:
  `feat(prefect): add prefect with heartbeat, backoff, and mass-death detection`
