# Prompt 03: Loop 2 — CLI Commands + Status Updates

You are adding the CLI commands for the refinery and updating the status
command to include merge pipeline state. These commands give the operator
full lifecycle control over the refinery and visibility into the merge
queue.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompts 01 and 02 are complete.

Read all existing code first. Understand the refinery package
(`internal/refinery/`), the session manager
(`internal/session/manager.go` — especially `Start`, `Stop`, `Attach`),
the existing CLI commands (`cmd/supervisor.go` for the lifecycle pattern,
`cmd/status.go` for the status pattern), and the status package
(`internal/status/`).

Read `docs/target-architecture.md` Section 5 (Loop 2 definition of
done, items 5 and 7) for the CLI requirements.

---

## Task 1: Refinery Command Group

Create `cmd/refinery.go` with the refinery command group and all
subcommands.

### Command Structure

```
gt refinery run <rig>       — run the merge loop (foreground)
gt refinery start <rig>     — start refinery in a tmux session
gt refinery stop <rig>      — stop the refinery tmux session
gt refinery queue <rig>     — show merge requests
gt refinery attach <rig>    — attach to refinery tmux session
```

### `gt refinery run <rig>`

Runs the refinery in the foreground. Blocks until interrupted
(SIGTERM/SIGINT). This is the core command — it runs the merge loop
directly.

```go
var refineryCmd = &cobra.Command{
    Use:   "refinery",
    Short: "Manage the merge refinery",
}

var refineryRunCmd = &cobra.Command{
    Use:   "run <rig>",
    Short: "Run the refinery merge loop (foreground)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        rig := args[0]

        logPath := filepath.Join(config.RuntimeDir(), "refinery-"+rig+".log")
        logger, logFile, err := refinery.NewLogger(logPath)
        if err != nil {
            return fmt.Errorf("failed to create logger: %w", err)
        }
        if logFile != nil {
            defer logFile.Close()
        }

        rigStore, err := store.OpenRig(rig)
        if err != nil { return err }
        defer rigStore.Close()

        townStore, err := store.OpenTown()
        if err != nil { return err }
        defer townStore.Close()

        // Discover source repo
        sourceRepo, err := dispatch.DiscoverSourceRepo()
        if err != nil { return err }

        // Load quality gates
        gatesPath := filepath.Join(config.RigDir(rig), "refinery", "quality-gates.txt")
        cfg := refinery.DefaultConfig()
        gates, err := refinery.LoadQualityGates(gatesPath, cfg.QualityGates)
        if err != nil { return err }
        cfg.QualityGates = gates

        ref := refinery.New(rig, sourceRepo, rigStore, townStore, cfg, logger)

        // Signal handling
        ctx, cancel := context.WithCancel(cmd.Context())
        defer cancel()
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
        go func() { <-sigCh; cancel() }()

        fmt.Fprintf(os.Stderr, "Refinery started for rig %q\n", rig)
        fmt.Fprintf(os.Stderr, "Log: %s\n", logPath)
        return ref.Run(ctx)
    },
}
```

### `gt refinery start <rig>`

Starts the refinery in a tmux session named `gt-{rig}-refinery`. This
is a convenience command — it creates the session running
`gt refinery run <rig>`.

```go
var refineryStartCmd = &cobra.Command{
    Use:   "start <rig>",
    Short: "Start the refinery in a tmux session",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        rig := args[0]
        sessName := dispatch.SessionName(rig, "refinery")
        mgr := session.New()

        // Check if already running
        if mgr.Exists(sessName) {
            return fmt.Errorf("refinery already running for rig %q (session %s)", rig, sessName)
        }

        // Discover source repo for working directory
        sourceRepo, err := dispatch.DiscoverSourceRepo()
        if err != nil { return err }

        // Start session running the refinery
        err = mgr.Start(sessName, sourceRepo,
            fmt.Sprintf("gt refinery run %s", rig),
            map[string]string{
                "GT_HOME": config.Home(),
                "GT_RIG":  rig,
            },
            "refinery", rig)
        if err != nil {
            return fmt.Errorf("failed to start refinery session: %w", err)
        }

        fmt.Printf("Refinery started for rig %q\n", rig)
        fmt.Printf("  Session: %s\n", sessName)
        fmt.Printf("  Attach:  gt refinery attach %s\n", rig)
        return nil
    },
}
```

### `gt refinery stop <rig>`

Stops the refinery's tmux session.

```go
var refineryStopCmd = &cobra.Command{
    Use:   "stop <rig>",
    Short: "Stop the refinery",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        rig := args[0]
        sessName := dispatch.SessionName(rig, "refinery")
        mgr := session.New()

        if !mgr.Exists(sessName) {
            return fmt.Errorf("no refinery running for rig %q", rig)
        }

        if err := mgr.Stop(sessName, false); err != nil {
            return fmt.Errorf("failed to stop refinery: %w", err)
        }

        fmt.Printf("Refinery stopped for rig %q\n", rig)
        return nil
    },
}
```

### `gt refinery attach <rig>`

Attaches to the refinery's tmux session. Uses `session.Manager.Attach()`
which replaces the current process via `syscall.Exec`.

```go
var refineryAttachCmd = &cobra.Command{
    Use:   "attach <rig>",
    Short: "Attach to the refinery tmux session",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        rig := args[0]
        sessName := dispatch.SessionName(rig, "refinery")
        mgr := session.New()

        if !mgr.Exists(sessName) {
            return fmt.Errorf("no refinery session for rig %q (run 'gt refinery start %s' first)", rig, rig)
        }

        return mgr.Attach(sessName)
    },
}
```

### `gt refinery queue <rig> [--json]`

Displays the merge queue for a rig. Default output is a human-readable
table. `--json` outputs the full list as JSON.

```go
var refineryQueueJSON bool

var refineryQueueCmd = &cobra.Command{
    Use:   "queue <rig>",
    Short: "Show the merge request queue",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        rig := args[0]

        rigStore, err := store.OpenRig(rig)
        if err != nil { return err }
        defer rigStore.Close()

        // List all merge requests (all phases)
        mrs, err := rigStore.ListMergeRequests("")
        if err != nil { return err }

        if refineryQueueJSON {
            return printJSON(mrs)
        }

        printQueue(rig, mrs)
        return nil
    },
}
```

### Human-Readable Queue Output

```
Merge Queue: myrig (3 items)

ID            WORK ITEM      BRANCH                              PHASE    ATTEMPTS
mr-a1b2c3d4   gt-11223344    polecat/Toast/gt-11223344           ready    0
mr-e5f6a7b8   gt-55667788    polecat/Jasper/gt-55667788          claimed  1
mr-c9d0e1f2   gt-99aabbcc    polecat/Sage/gt-99aabbcc            merged   1

Summary: 1 ready, 1 in progress, 1 merged
```

When the queue is empty:
```
Merge Queue: myrig (empty)
```

Use `text/tabwriter` for column alignment (same pattern as `gt status`).

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

Extend `gt status` to show refinery state and merge queue depth. The
status package gathers this information alongside existing agent data.

### New Fields in RigStatus

```go
// internal/status/status.go

// RigStatus — add new fields:
type RigStatus struct {
    Rig           string          `json:"rig"`
    Supervisor    SupervisorInfo  `json:"supervisor"`
    Refinery      RefineryInfo    `json:"refinery"`     // NEW
    Agents        []AgentStatus   `json:"agents"`
    MergeQueue    MergeQueueInfo  `json:"merge_queue"`  // NEW
    Summary       Summary         `json:"summary"`
}

// RefineryInfo holds refinery process state.
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
func Gather(rig string, townStore TownStore, rigStore RigStore,
    mqStore MergeQueueStore, checker SessionChecker) (*RigStatus, error)
```

In the gather logic:
1. Check refinery session: `checker.Exists(dispatch.SessionName(rig, "refinery"))`
2. List all merge requests: `mqStore.ListMergeRequests("")`
3. Count by phase to populate `MergeQueueInfo`

**Note:** `*store.Store` already implements `ListMergeRequests`, so the
caller can pass the same rig store for both `RigStore` and
`MergeQueueStore`. Adjust the call sites in `cmd/status.go`.

### Updated Human-Readable Output

Add refinery and merge queue lines to the status output:

```
Rig: myrig
Supervisor: running (pid 12345)
Refinery: running (gt-myrig-refinery)

AGENT      STATE     SESSION   WORK
Toast      working   alive     gt-a1b2c3d4: Implement login page
Jasper     idle      -         -

Merge Queue: 2 ready, 1 in progress, 0 failed
Summary: 2 agents (1 working, 1 idle, 0 stalled, 0 dead sessions)
Health: healthy
```

When the refinery is not running:
```
Refinery: not running
```

When there are no merge requests:
```
Merge Queue: empty
```

### Updated Health Logic

The refinery state does not affect the health exit code in Loop 2. An
absent refinery just means merges won't happen — the system is still
operational. Keep health based on supervisor + session liveness only.
The operator can see refinery state in the output and take action.

---

## Task 3: Shared printJSON Helper

If `cmd/status.go` already has a `printJSON` function, make sure it's
accessible from `cmd/refinery.go`. If it's a package-level function in
`cmd/`, it's already shared. If it's a local function, either:

- Move it to a shared file like `cmd/helpers.go`, or
- Duplicate the 4-line function in `cmd/refinery.go`

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
    // bin/gt refinery run --help exits 0

func TestCLIRefineryStartHelp(t *testing.T)
    // bin/gt refinery start --help exits 0

func TestCLIRefineryStopHelp(t *testing.T)
    // bin/gt refinery stop --help exits 0

func TestCLIRefineryQueueHelp(t *testing.T)
    // bin/gt refinery queue --help exits 0

func TestCLIRefineryAttachHelp(t *testing.T)
    // bin/gt refinery attach --help exits 0

func TestCLIRefineryQueue(t *testing.T)
    // Create a work item, sling, and done
    // bin/gt refinery queue testrig
    // Verify: output contains the MR ID
    // bin/gt refinery queue testrig --json
    // Verify: valid JSON with expected fields

func TestCLIStatusWithRefinery(t *testing.T)
    // Start refinery for a rig
    // bin/gt status testrig
    // Verify: output contains "Refinery:" line
    // bin/gt status testrig --json
    // Verify: JSON contains refinery and merge_queue fields
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
    // Mock: refinery session alive
    // Verify: RefineryInfo.Running == true

func TestGatherWithoutRefinery(t *testing.T)
    // Mock: refinery session not alive
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
   export GT_HOME=/tmp/gt-test
   # Setup: create work, sling, done (creates MR)
   bin/gt store create --db=testrig --title="Test item"
   bin/gt sling <id> testrig
   bin/gt done --rig=testrig --agent=<name>

   # View queue
   bin/gt refinery queue testrig
   bin/gt refinery queue testrig --json | jq .

   # Start refinery in tmux
   bin/gt refinery start testrig
   # Attach to watch it work
   bin/gt refinery attach testrig
   # Detach with Ctrl-B D

   # Check status
   bin/gt status testrig
   bin/gt status testrig --json | jq .refinery
   bin/gt status testrig --json | jq .merge_queue

   # Stop refinery
   bin/gt refinery stop testrig
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Guidelines

- Follow the existing CLI patterns exactly. `cmd/supervisor.go` is the
  reference for lifecycle commands (run/stop). `cmd/status.go` is the
  reference for display commands.
- `gt refinery start` creates a tmux session running `gt refinery run`.
  The operator can also run the refinery in the foreground directly —
  the `start` command is a convenience.
- `gt refinery attach` is a thin wrapper around
  `session.Manager.Attach()`. It replaces the current process with
  `tmux attach-session`.
- The `printQueue` function should handle the case where there are no
  merge requests gracefully.
- When updating the `Gather` function signature, update all call sites:
  `cmd/status.go` and any tests. The `*store.Store` type satisfies both
  `RigStore` and `MergeQueueStore` — pass the same store for both.
- The refinery session name follows the existing convention:
  `gt-{rig}-refinery` (via `dispatch.SessionName(rig, "refinery")`).
- Don't add `--watch` or `--follow` mode to `gt refinery queue`. A
  simple one-shot listing is sufficient for Loop 2.
- Commit after tests pass with message:
  `feat(cli): add refinery lifecycle commands and merge queue status`
