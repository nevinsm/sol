# Prompt 03: Loop 2 — CLI Commands + Status Updates

You are adding the CLI commands for the forge and updating the status
command to include merge pipeline state. These commands give the operator
full lifecycle control over the forge and visibility into the merge
queue.

**Working directory:** `~/sol-src/`
**Prerequisite:** Prompts 01 and 02 are complete.

Read all existing code first. Understand the forge package
(`internal/forge/`), the session manager
(`internal/session/manager.go` — especially `Start`, `Stop`, `Attach`),
the existing CLI commands (`cmd/prefect.go` for the lifecycle pattern,
`cmd/status.go` for the status pattern), and the status package
(`internal/status/`).

Read `docs/target-architecture.md` Section 5 (Loop 2 definition of
done, items 5 and 7) for the CLI requirements.

---

## Task 1: Forge Command Group

Create `cmd/forge.go` with the forge command group and all
subcommands.

### Command Structure

```
sol forge run <world>       — run the merge loop (foreground)
sol forge start <world>     — start forge in a tmux session
sol forge stop <world>      — stop the forge tmux session
sol forge queue <world>     — show merge requests
sol forge attach <world>    — attach to forge tmux session
```

### `sol forge run <world>`

Runs the forge in the foreground. Blocks until interrupted
(SIGTERM/SIGINT). This is the core command — it runs the merge loop
directly.

```go
var refineryCmd = &cobra.Command{
    Use:   "forge",
    Short: "Manage the merge forge",
}

var refineryRunCmd = &cobra.Command{
    Use:   "run <world>",
    Short: "Run the forge merge loop (foreground)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        world := args[0]

        logPath := filepath.Join(config.RuntimeDir(), "forge-"+world+".log")
        logger, logFile, err := forge.NewLogger(logPath)
        if err != nil {
            return fmt.Errorf("failed to create logger: %w", err)
        }
        if logFile != nil {
            defer logFile.Close()
        }

        worldStore, err := store.OpenWorld(world)
        if err != nil { return err }
        defer worldStore.Close()

        sphereStore, err := store.OpenSphere()
        if err != nil { return err }
        defer sphereStore.Close()

        // Discover source repo
        sourceRepo, err := dispatch.DiscoverSourceRepo()
        if err != nil { return err }

        // Load quality gates
        gatesPath := filepath.Join(config.RigDir(world), "forge", "quality-gates.txt")
        cfg := forge.DefaultConfig()
        gates, err := forge.LoadQualityGates(gatesPath, cfg.QualityGates)
        if err != nil { return err }
        cfg.QualityGates = gates

        ref := forge.New(world, sourceRepo, worldStore, sphereStore, cfg, logger)

        // Signal handling
        ctx, cancel := context.WithCancel(cmd.Context())
        defer cancel()
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
        go func() { <-sigCh; cancel() }()

        fmt.Fprintf(os.Stderr, "Forge started for world %q\n", world)
        fmt.Fprintf(os.Stderr, "Log: %s\n", logPath)
        return ref.Run(ctx)
    },
}
```

### `sol forge start <world>`

Starts the forge in a tmux session named `sol-{world}-forge`. This
is a convenience command — it creates the session running
`sol forge run <world>`.

```go
var refineryStartCmd = &cobra.Command{
    Use:   "start <world>",
    Short: "Start the forge in a tmux session",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        world := args[0]
        sessName := dispatch.SessionName(world, "forge")
        mgr := session.New()

        // Check if already running
        if mgr.Exists(sessName) {
            return fmt.Errorf("forge already running for world %q (session %s)", world, sessName)
        }

        // Discover source repo for working directory
        sourceRepo, err := dispatch.DiscoverSourceRepo()
        if err != nil { return err }

        // Start session running the forge
        err = mgr.Start(sessName, sourceRepo,
            fmt.Sprintf("sol forge run %s", world),
            map[string]string{
                "SOL_HOME": config.Home(),
                "SOL_WORLD":  world,
            },
            "forge", world)
        if err != nil {
            return fmt.Errorf("failed to start forge session: %w", err)
        }

        fmt.Printf("Forge started for world %q\n", world)
        fmt.Printf("  Session: %s\n", sessName)
        fmt.Printf("  Attach:  sol forge attach %s\n", world)
        return nil
    },
}
```

### `sol forge stop <world>`

Stops the forge's tmux session.

```go
var refineryStopCmd = &cobra.Command{
    Use:   "stop <world>",
    Short: "Stop the forge",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        world := args[0]
        sessName := dispatch.SessionName(world, "forge")
        mgr := session.New()

        if !mgr.Exists(sessName) {
            return fmt.Errorf("no forge running for world %q", world)
        }

        if err := mgr.Stop(sessName, false); err != nil {
            return fmt.Errorf("failed to stop forge: %w", err)
        }

        fmt.Printf("Forge stopped for world %q\n", world)
        return nil
    },
}
```

### `sol forge attach <world>`

Attaches to the forge's tmux session. Uses `session.Manager.Attach()`
which replaces the current process via `syscall.Exec`.

```go
var refineryAttachCmd = &cobra.Command{
    Use:   "attach <world>",
    Short: "Attach to the forge tmux session",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        world := args[0]
        sessName := dispatch.SessionName(world, "forge")
        mgr := session.New()

        if !mgr.Exists(sessName) {
            return fmt.Errorf("no forge session for world %q (run 'sol forge start %s' first)", world, world)
        }

        return mgr.Attach(sessName)
    },
}
```

### `sol forge queue <world> [--json]`

Displays the merge queue for a world. Default output is a human-readable
table. `--json` outputs the full list as JSON.

```go
var refineryQueueJSON bool

var refineryQueueCmd = &cobra.Command{
    Use:   "queue <world>",
    Short: "Show the merge request queue",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        world := args[0]

        worldStore, err := store.OpenWorld(world)
        if err != nil { return err }
        defer worldStore.Close()

        // List all merge requests (all phases)
        mrs, err := worldStore.ListMergeRequests("")
        if err != nil { return err }

        if refineryQueueJSON {
            return printJSON(mrs)
        }

        printQueue(world, mrs)
        return nil
    },
}
```

### Human-Readable Queue Output

```
Merge Queue: myworld (3 items)

ID            WORK ITEM      BRANCH                              PHASE    ATTEMPTS
mr-a1b2c3d4   sol-11223344    outpost/Toast/sol-11223344           ready    0
mr-e5f6a7b8   sol-55667788    outpost/Jasper/sol-55667788          claimed  1
mr-c9d0e1f2   sol-99aabbcc    outpost/Sage/sol-99aabbcc            merged   1

Summary: 1 ready, 1 in progress, 1 merged
```

When the queue is empty:
```
Merge Queue: myworld (empty)
```

Use `text/tabwriter` for column alignment (same pattern as `sol status`).

### Init

```go
func init() {
    rootCmd.AddCommand(refineryCmd)
    refineryCmd.AddCommand(refineryRunCmd)
    refineryCmd.AddCommand(refineryStartCmd)
    refineryCmd.AddCommand(refineryStopCmd)
    refineryCmd.AddCommand(refineryQueueCmd)
    refineryCmd.AddCommand(refineryAttachCmd)
    refineryQueueCmd.Flags().BoolVar(&refineryQueueJSON, "json", false, "output as JSON")
}
```

---

## Task 2: Update Status Command

Extend `sol status` to show forge state and merge queue depth. The
status package gathers this information alongside existing agent data.

### New Fields in RigStatus

```go
// internal/status/status.go

// RigStatus — add new fields:
type RigStatus struct {
    World           string          `json:"world"`
    Prefect    SupervisorInfo  `json:"prefect"`
    Forge      RefineryInfo    `json:"forge"`     // NEW
    Agents        []AgentStatus   `json:"agents"`
    MergeQueue    MergeQueueInfo  `json:"merge_queue"`  // NEW
    Summary       Summary         `json:"summary"`
}

// RefineryInfo holds forge process state.
type RefineryInfo struct {
    Running      bool   `json:"running"`
    SessionName  string `json:"session_name,omitempty"`
}

// MergeQueueInfo holds merge queue summary.
type MergeQueueInfo struct {
    Ready   int `json:"ready"`
    Claimed int `json:"claimed"`
    Failed  int `json:"failed"`
    Merged  int `json:"merged"`
    Total   int `json:"total"`
}
```

### Updated Gather Function

Add a `MergeQueueStore` interface:

```go
// MergeQueueStore abstracts merge request queries for testing.
type MergeQueueStore interface {
    ListMergeRequests(phase string) ([]store.MergeRequest, error)
}
```

Update `Gather()` to accept this interface and populate the new fields:

```go
func Gather(world string, sphereStore SphereStore, worldStore WorldStore,
    mqStore MergeQueueStore, checker SessionChecker) (*RigStatus, error)
```

In the gather logic:
1. Check forge session: `checker.Exists(dispatch.SessionName(world, "forge"))`
2. List all merge requests: `mqStore.ListMergeRequests("")`
3. Count by phase to populate `MergeQueueInfo`

**Note:** `*store.Store` already implements `ListMergeRequests`, so the
caller can pass the same world store for both `WorldStore` and
`MergeQueueStore`. Adjust the call sites in `cmd/status.go`.

### Updated Human-Readable Output

Add forge and merge queue lines to the status output:

```
World: myworld
Prefect: running (pid 12345)
Forge: running (sol-myworld-forge)

AGENT      STATE     SESSION   WORK
Toast      working   alive     sol-a1b2c3d4: Implement login page
Jasper     idle      -         -

Merge Queue: 2 ready, 1 in progress, 0 failed
Summary: 2 agents (1 working, 1 idle, 0 stalled, 0 dead sessions)
Health: healthy
```

When the forge is not running:
```
Forge: not running
```

When there are no merge requests:
```
Merge Queue: empty
```

### Updated Health Logic

The forge state does not affect the health exit code in Loop 2. An
absent forge just means merges won't happen — the system is still
operational. Keep health based on prefect + session liveness only.
The operator can see forge state in the output and take action.

---

## Task 3: Shared printJSON Helper

If `cmd/status.go` already has a `printJSON` function, make sure it's
accessible from `cmd/forge.go`. If it's a package-level function in
`cmd/`, it's already shared. If it's a local function, either:

- Move it to a shared file like `cmd/helpers.go`, or
- Duplicate the 4-line function in `cmd/forge.go`

```go
func printJSON(v any) error {
    enc := json.NewEncoder(os.Stdout)
    enc.SetIndent("", "  ")
    return enc.Encode(v)
}
```

---

## Task 4: Tests

### CLI Smoke Tests

Add to `test/integration/cli_test.go` (or create
`test/integration/cli_loop2_test.go`):

```go
func TestCLIRefineryRunHelp(t *testing.T)
    // bin/sol forge run --help exits 0

func TestCLIRefineryStartHelp(t *testing.T)
    // bin/sol forge start --help exits 0

func TestCLIRefineryStopHelp(t *testing.T)
    // bin/sol forge stop --help exits 0

func TestCLIRefineryQueueHelp(t *testing.T)
    // bin/sol forge queue --help exits 0

func TestCLIRefineryAttachHelp(t *testing.T)
    // bin/sol forge attach --help exits 0

func TestCLIRefineryQueue(t *testing.T)
    // Create a work item, cast, and done
    // bin/sol forge queue testrig
    // Verify: output contains the MR ID
    // bin/sol forge queue testrig --json
    // Verify: valid JSON with expected fields

func TestCLIStatusWithRefinery(t *testing.T)
    // Start forge for a world
    // bin/sol status testrig
    // Verify: output contains "Forge:" line
    // bin/sol status testrig --json
    // Verify: JSON contains forge and merge_queue fields
```

### Status Package Tests

Add to `internal/status/status_test.go`:

```go
// New mock
type mockMergeQueueStore struct {
    mrs []store.MergeRequest
}
func (m *mockMergeQueueStore) ListMergeRequests(phase string) ([]store.MergeRequest, error)

func TestGatherWithRefinery(t *testing.T)
    // Mock: forge session alive
    // Verify: RefineryInfo.Running == true

func TestGatherWithoutRefinery(t *testing.T)
    // Mock: forge session not alive
    // Verify: RefineryInfo.Running == false

func TestGatherMergeQueue(t *testing.T)
    // Mock: 3 MRs (2 ready, 1 claimed)
    // Verify: MergeQueueInfo counts correct

func TestGatherMergeQueueEmpty(t *testing.T)
    // Mock: no MRs
    // Verify: MergeQueueInfo all zeros
```

Update all existing status tests to pass the new `MergeQueueStore`
parameter. For existing tests where the merge queue is irrelevant, pass
a mock that returns empty results.

---

## Task 5: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   # Setup: create work, cast, done (creates MR)
   bin/sol store create --world=testrig --title="Test item"
   bin/sol cast <id> testrig
   bin/sol done --world=testrig --agent=<name>

   # View queue
   bin/sol forge queue testrig
   bin/sol forge queue testrig --json | jq .

   # Start forge in tmux
   bin/sol forge start testrig
   # Attach to watch it work
   bin/sol forge attach testrig
   # Detach with Ctrl-B D

   # Check status
   bin/sol status testrig
   bin/sol status testrig --json | jq .forge
   bin/sol status testrig --json | jq .merge_queue

   # Stop forge
   bin/sol forge stop testrig
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- Follow the existing CLI patterns exactly. `cmd/prefect.go` is the
  reference for lifecycle commands (run/stop). `cmd/status.go` is the
  reference for display commands.
- `sol forge start` creates a tmux session running `sol forge run`.
  The operator can also run the forge in the foreground directly —
  the `start` command is a convenience.
- `sol forge attach` is a thin wrapper around
  `session.Manager.Attach()`. It replaces the current process with
  `tmux attach-session`.
- The `printQueue` function should handle the case where there are no
  merge requests gracefully.
- When updating the `Gather` function signature, update all call sites:
  `cmd/status.go` and any tests. The `*store.Store` type satisfies both
  `WorldStore` and `MergeQueueStore` — pass the same store for both.
- The forge session name follows the existing convention:
  `sol-{world}-forge` (via `dispatch.SessionName(world, "forge")`).
- Don't add `--watch` or `--follow` mode to `sol forge queue`. A
  simple one-shot listing is sufficient for Loop 2.
- Commit after tests pass with message:
  `feat(cli): add forge lifecycle commands and merge queue status`
