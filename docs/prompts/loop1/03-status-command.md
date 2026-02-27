# Prompt 03: Loop 1 — Status Command

You are adding the `sol status` command to the `sol` orchestration system.
This is the operator's primary observability tool — a single command that
shows everything happening in a world: agents, sessions, tethered work, and
prefect health.

**Working directory:** `~/sol-src/`
**Prerequisite:** Prompts 01 and 02 are complete.

Read all existing code first. Understand the store agent operations
(`internal/store/agents.go`), session manager list/health
(`internal/session/manager.go`), tether read (`internal/tether/tether.go`),
prefect PID file (`internal/prefect/pidfile.go` — note: the
prefect is sphere-level, one PID file at `.runtime/prefect.pid`),
and the config package (`internal/config/config.go`).

Read `docs/target-architecture.md` Section 5 (Loop 1, definition of
done #5) for the `sol status` requirement.

---

## Task 1: Status Package

Create `internal/status/` — a package that gathers all runtime state for
a world into a single struct, ready for display.

### Data Structures

```go
// internal/status/status.go
package status

import "time"

// RigStatus holds the complete runtime state for a world.
type RigStatus struct {
    World           string        `json:"world"`
    Prefect    SupervisorInfo `json:"prefect"`
    Agents        []AgentStatus  `json:"agents"`
    Summary       Summary        `json:"summary"`
}

// SupervisorInfo holds prefect process state (sphere-level, not per-world).
type SupervisorInfo struct {
    Running bool `json:"running"`
    PID     int  `json:"pid,omitempty"`
}

// AgentStatus holds the combined state of one agent.
type AgentStatus struct {
    Name         string `json:"name"`
    State        string `json:"state"`          // idle|working|stalled|stuck|zombie
    SessionAlive bool   `json:"session_alive"`
    TetherItem     string `json:"hook_item,omitempty"`
    WorkTitle    string `json:"work_title,omitempty"` // title of tethered work item
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
// 2 = degraded (prefect not running or mass death)
func (r *RigStatus) Health() int
```

### Gather Interface

```go
// SessionChecker abstracts session liveness checks for testing.
type SessionChecker interface {
    Exists(name string) bool
}

// WorldStore abstracts work item lookups for testing.
type WorldStore interface {
    GetWorkItem(id string) (*store.WorkItem, error)
}

// SphereStore abstracts agent queries for testing.
type SphereStore interface {
    ListAgents(world string, state string) ([]store.Agent, error)
}

// Gather collects runtime state for a world.
func Gather(world string, sphereStore SphereStore, worldStore WorldStore, checker SessionChecker) (*RigStatus, error)
```

### Gather Logic

```
Gather(world, sphereStore, worldStore, checker):
    1. Check prefect: prefect.ReadPID() + prefect.IsRunning(pid)
       (prefect is sphere-level — same check regardless of world)
    2. List all agents: sphereStore.ListAgents(world, "")
    3. For each agent:
       a. Compute session name: dispatch.SessionName(world, agent.Name)
       b. Check session liveness: checker.Exists(sessionName)
       c. If agent has a hook_item, look up the work item title:
          worldStore.GetWorkItem(agent.TetherItem)
          (If not found, use "(unknown)" as title — don't fail)
       d. Build AgentStatus
    4. Compute Summary counts:
       - Total = len(agents)
       - Working = agents with state "working"
       - Idle = agents with state "idle"
       - Stalled = agents with state "stalled"
       - Dead = agents with state "working" but session not alive
    5. Compute Health:
       - If prefect not running -> 2 (degraded)
       - If any Dead > 0 -> 1 (unhealthy)
       - Otherwise -> 0 (healthy)
    6. Return RigStatus
```

---

## Task 2: CLI Command

### `sol status <world> [--json]`

```
sol status <world> [--json]
```

Displays the runtime state for a world. Default output is a human-readable
table. `--json` outputs the full `RigStatus` as JSON.

**Exit codes:**
- 0: healthy
- 1: unhealthy (dead sessions detected)
- 2: degraded (prefect not running)

```go
// cmd/status.go
var statusJSON bool

var statusCmd = &cobra.Command{
    Use:   "status <world>",
    Short: "Show world status",
    Args:  cobra.ExactArgs(1),
    // SilenceErrors and SilenceUsage so exit code reflects health, not cobra
    SilenceErrors: true,
    SilenceUsage:  true,
    RunE: func(cmd *cobra.Command, args []string) error {
        world := args[0]

        sphereStore, err := store.OpenSphere()
        if err != nil { return err }
        defer sphereStore.Close()

        worldStore, err := store.OpenWorld(world)
        if err != nil { return err }
        defer worldStore.Close()

        mgr := session.New()

        result, err := status.Gather(world, sphereStore, worldStore, mgr)
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
World: myworld
Prefect: running (pid 12345)

AGENT      STATE     SESSION   WORK
Toast      working   alive     sol-a1b2c3d4: Implement login page
Jasper     working   dead!     sol-c5d6e7f8: Fix CSS regression
Sage       idle      -         -
Copper     stalled   dead!     sol-11223344: Add unit tests

Summary: 4 agents (2 working, 1 idle, 1 stalled, 1 dead session)
Health: unhealthy
```

When the prefect is not running:
```
Prefect: not running
```

When there are no agents:
```
World: myworld
Prefect: running (pid 12345)

No agents registered.
```

### Implementation Notes

- The `SESSION` column shows `alive`, `dead!`, or `-` (no session expected).
  An agent in `idle` state should show `-`. An agent in `working` or
  `stalled` state should show `alive` or `dead!`.
- The `WORK` column shows `{workItemID}: {title}` or `-` if no tethered work.
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

Similarly, `status.SphereStore` requires `ListAgents(world, state string)`,
which `*store.Store` already implements.

And `status.WorldStore` requires `GetWorkItem(id string)`, which
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
func (m *mockTownStore) ListAgents(world, state string) ([]store.Agent, error)

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
    // Prefect not running (no PID file)
    // Verify: Health() == 2

func TestGatherNoAgents(t *testing.T)
    // Empty agent list
    // Verify: Summary.Total == 0, Health() == 0 or 2 depending on prefect

func TestGatherWithTetheredWork(t *testing.T)
    // Agent has hook_item set
    // Mock world store returns the work item
    // Verify: AgentStatus.WorkTitle matches the work item title

func TestGatherMissingWorkItem(t *testing.T)
    // Agent has hook_item but work item not found in store
    // Verify: AgentStatus.WorkTitle == "(unknown)", no error

func TestGatherMixedStates(t *testing.T)
    // Multiple agents in different states
    // Verify: Summary counts are correct for each state

func TestHealthExitCodes(t *testing.T)
    // Directly test RigStatus.Health() with various configurations
    // Prefect running + all healthy -> 0
    // Prefect running + dead session -> 1
    // Prefect not running -> 2
```

---

## Task 5: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   # Create and dispatch work
   bin/sol store create --world=testrig --title="Test task"
   bin/sol cast <id> testrig
   # Check status (no prefect running -> degraded)
   bin/sol status testrig
   echo $?   # should be 2
   # Start prefect (sphere-level, in another terminal)
   bin/sol prefect run &
   # Check status again
   bin/sol status testrig
   echo $?   # should be 0
   # JSON output
   bin/sol status testrig --json | jq .
   # Kill agent session
   tmux kill-session -t sol-testrig-<agent>
   # Check status
   bin/sol status testrig
   echo $?   # should be 1
   # Stop prefect
   bin/sol prefect stop
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- `sol status` is read-only — it never modifies state. It reads from the
  store, checks tmux, reads the PID file, and reports.
- The `Gather` function should not import the `prefect` package's
  internal types. Use `prefect.ReadPID()` and `prefect.IsRunning()`
  which are public functions.
- Keep the status package self-contained. It depends on store types for
  the agent struct, but not on dispatch or session internals beyond the
  `Exists` check.
- The exit code behavior means `sol status` can be used in scripts:
  `sol status myworld || echo "Something is wrong"`.
- Don't add watch/follow mode. A simple one-shot status check is
  sufficient for Loop 1. Continuous monitoring comes later.
- Commit after tests pass with message:
  `feat(status): add sol status command with health reporting`
