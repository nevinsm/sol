# Prompt 03: Loop 1 — Status Command

You are adding the `gt status` command to the `gt` orchestration system.
This is the operator's primary observability tool — a single command that
shows everything happening in a rig: agents, sessions, hooked work, and
supervisor health.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompts 01 and 02 are complete.

Read all existing code first. Understand the store agent operations
(`internal/store/agents.go`), session manager list/health
(`internal/session/manager.go`), hook read (`internal/hook/hook.go`),
supervisor PID file (`internal/supervisor/pidfile.go` — note: the
supervisor is town-level, one PID file at `.runtime/supervisor.pid`),
and the config package (`internal/config/config.go`).

Read `docs/target-architecture.md` Section 5 (Loop 1, definition of
done #5) for the `gt status` requirement.

---

## Task 1: Status Package

Create `internal/status/` — a package that gathers all runtime state for
a rig into a single struct, ready for display.

### Data Structures

```go
// internal/status/status.go
package status

import "time"

// RigStatus holds the complete runtime state for a rig.
type RigStatus struct {
    Rig           string        `json:"rig"`
    Supervisor    SupervisorInfo `json:"supervisor"`
    Agents        []AgentStatus  `json:"agents"`
    Summary       Summary        `json:"summary"`
}

// SupervisorInfo holds supervisor process state (town-level, not per-rig).
type SupervisorInfo struct {
    Running bool `json:"running"`
    PID     int  `json:"pid,omitempty"`
}

// AgentStatus holds the combined state of one agent.
type AgentStatus struct {
    Name         string `json:"name"`
    State        string `json:"state"`          // idle|working|stalled|stuck|zombie
    SessionAlive bool   `json:"session_alive"`
    HookItem     string `json:"hook_item,omitempty"`
    WorkTitle    string `json:"work_title,omitempty"` // title of hooked work item
}

// Summary holds aggregate counts.
type Summary struct {
    Total    int `json:"total"`
    Working  int `json:"working"`
    Idle     int `json:"idle"`
    Stalled  int `json:"stalled"`
    Dead     int `json:"dead"`     // working agents with dead sessions
}

// Health returns the overall health level.
// 0 = healthy (all sessions alive or idle)
// 1 = unhealthy (at least one dead session)
// 2 = degraded (supervisor not running or mass death)
func (r *RigStatus) Health() int
```

### Gather Interface

```go
// SessionChecker abstracts session liveness checks for testing.
type SessionChecker interface {
    Exists(name string) bool
}

// RigStore abstracts work item lookups for testing.
type RigStore interface {
    GetWorkItem(id string) (*store.WorkItem, error)
}

// TownStore abstracts agent queries for testing.
type TownStore interface {
    ListAgents(rig string, state string) ([]store.Agent, error)
}

// Gather collects runtime state for a rig.
func Gather(rig string, townStore TownStore, rigStore RigStore, checker SessionChecker) (*RigStatus, error)
```

### Gather Logic

```
Gather(rig, townStore, rigStore, checker):
    1. Check supervisor: supervisor.ReadPID() + supervisor.IsRunning(pid)
       (supervisor is town-level — same check regardless of rig)
    2. List all agents: townStore.ListAgents(rig, "")
    3. For each agent:
       a. Compute session name: dispatch.SessionName(rig, agent.Name)
       b. Check session liveness: checker.Exists(sessionName)
       c. If agent has a hook_item, look up the work item title:
          rigStore.GetWorkItem(agent.HookItem)
          (If not found, use "(unknown)" as title — don't fail)
       d. Build AgentStatus
    4. Compute Summary counts:
       - Total = len(agents)
       - Working = agents with state "working"
       - Idle = agents with state "idle"
       - Stalled = agents with state "stalled"
       - Dead = agents with state "working" but session not alive
    5. Compute Health:
       - If supervisor not running -> 2 (degraded)
       - If any Dead > 0 -> 1 (unhealthy)
       - Otherwise -> 0 (healthy)
    6. Return RigStatus
```

---

## Task 2: CLI Command

### `gt status <rig> [--json]`

```
gt status <rig> [--json]
```

Displays the runtime state for a rig. Default output is a human-readable
table. `--json` outputs the full `RigStatus` as JSON.

**Exit codes:**
- 0: healthy
- 1: unhealthy (dead sessions detected)
- 2: degraded (supervisor not running)

```go
// cmd/status.go
var statusJSON bool

var statusCmd = &cobra.Command{
    Use:   "status <rig>",
    Short: "Show rig status",
    Args:  cobra.ExactArgs(1),
    // SilenceErrors and SilenceUsage so exit code reflects health, not cobra
    SilenceErrors: true,
    SilenceUsage:  true,
    RunE: func(cmd *cobra.Command, args []string) error {
        rig := args[0]

        townStore, err := store.OpenTown()
        if err != nil { return err }
        defer townStore.Close()

        rigStore, err := store.OpenRig(rig)
        if err != nil { return err }
        defer rigStore.Close()

        mgr := session.New()

        result, err := status.Gather(rig, townStore, rigStore, mgr)
        if err != nil { return err }

        if statusJSON {
            return printJSON(result)
        }

        printStatus(result)

        // Exit with health code
        os.Exit(result.Health())
        return nil
    },
}

func init() {
    rootCmd.AddCommand(statusCmd)
    statusCmd.Flags().BoolVar(&statusJSON, "json", false, "output as JSON")
}
```

### Human-Readable Output Format

```
Rig: myrig
Supervisor: running (pid 12345)

AGENT      STATE     SESSION   WORK
Toast      working   alive     gt-a1b2c3d4: Implement login page
Jasper     working   dead!     gt-c5d6e7f8: Fix CSS regression
Sage       idle      -         -
Copper     stalled   dead!     gt-11223344: Add unit tests

Summary: 4 agents (2 working, 1 idle, 1 stalled, 1 dead session)
Health: unhealthy
```

When the supervisor is not running:
```
Supervisor: not running
```

When there are no agents:
```
Rig: myrig
Supervisor: running (pid 12345)

No agents registered.
```

### Implementation Notes

- The `SESSION` column shows `alive`, `dead!`, or `-` (no session expected).
  An agent in `idle` state should show `-`. An agent in `working` or
  `stalled` state should show `alive` or `dead!`.
- The `WORK` column shows `{workItemID}: {title}` or `-` if no hooked work.
- Use `fmt.Fprintf` with tab-aligned columns (either `text/tabwriter` or
  manual `%-Ns` format strings). `text/tabwriter` is preferred.
- The `Health` line uses the same text as the exit code meaning:
  `healthy`, `unhealthy`, `degraded`.

---

## Task 3: Session Manager Interface Satisfaction

The `status.SessionChecker` interface requires only `Exists(name string) bool`,
which `session.Manager` already implements. No changes to the session
package are needed — the existing `*session.Manager` satisfies
`SessionChecker`.

Similarly, `status.TownStore` requires `ListAgents(rig, state string)`,
which `*store.Store` already implements.

And `status.RigStore` requires `GetWorkItem(id string)`, which
`*store.Store` already implements.

Verify that the existing types satisfy these interfaces. If any method
signatures don't match, adjust the status interfaces to match the
existing implementations (not the other way around).

---

## Task 4: Tests

Create `internal/status/status_test.go`:

```go
// Mock implementations
type mockChecker struct {
    alive map[string]bool
}
func (m *mockChecker) Exists(name string) bool { return m.alive[name] }

type mockTownStore struct {
    agents []store.Agent
}
func (m *mockTownStore) ListAgents(rig, state string) ([]store.Agent, error)

type mockRigStore struct {
    items map[string]*store.WorkItem
}
func (m *mockRigStore) GetWorkItem(id string) (*store.WorkItem, error)
```

```go
func TestGatherHealthy(t *testing.T)
    // All working agents have live sessions
    // Verify: Health() == 0, Summary.Dead == 0

func TestGatherUnhealthy(t *testing.T)
    // One working agent has a dead session
    // Verify: Health() == 1, Summary.Dead == 1

func TestGatherDegraded(t *testing.T)
    // Supervisor not running (no PID file)
    // Verify: Health() == 2

func TestGatherNoAgents(t *testing.T)
    // Empty agent list
    // Verify: Summary.Total == 0, Health() == 0 or 2 depending on supervisor

func TestGatherWithHookedWork(t *testing.T)
    // Agent has hook_item set
    // Mock rig store returns the work item
    // Verify: AgentStatus.WorkTitle matches the work item title

func TestGatherMissingWorkItem(t *testing.T)
    // Agent has hook_item but work item not found in store
    // Verify: AgentStatus.WorkTitle == "(unknown)", no error

func TestGatherMixedStates(t *testing.T)
    // Multiple agents in different states
    // Verify: Summary counts are correct for each state

func TestHealthExitCodes(t *testing.T)
    // Directly test RigStatus.Health() with various configurations
    // Supervisor running + all healthy -> 0
    // Supervisor running + dead session -> 1
    // Supervisor not running -> 2
```

---

## Task 5: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export GT_HOME=/tmp/gt-test
   # Create and dispatch work
   bin/gt store create --db=testrig --title="Test task"
   bin/gt sling <id> testrig
   # Check status (no supervisor running -> degraded)
   bin/gt status testrig
   echo $?   # should be 2
   # Start supervisor (town-level, in another terminal)
   bin/gt supervisor run &
   # Check status again
   bin/gt status testrig
   echo $?   # should be 0
   # JSON output
   bin/gt status testrig --json | jq .
   # Kill agent session
   tmux kill-session -t gt-testrig-<agent>
   # Check status
   bin/gt status testrig
   echo $?   # should be 1
   # Stop supervisor
   bin/gt supervisor stop
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Guidelines

- `gt status` is read-only — it never modifies state. It reads from the
  store, checks tmux, reads the PID file, and reports.
- The `Gather` function should not import the `supervisor` package's
  internal types. Use `supervisor.ReadPID()` and `supervisor.IsRunning()`
  which are public functions.
- Keep the status package self-contained. It depends on store types for
  the agent struct, but not on dispatch or session internals beyond the
  `Exists` check.
- The exit code behavior means `gt status` can be used in scripts:
  `gt status myrig || echo "Something is wrong"`.
- Don't add watch/follow mode. A simple one-shot status check is
  sufficient for Loop 1. Continuous monitoring comes later.
- Commit after tests pass with message:
  `feat(status): add gt status command with health reporting`
