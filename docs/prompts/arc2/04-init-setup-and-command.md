# Prompt 04: Arc 2 — Init Setup Engine + Flag-Based Command

You are building `sol init` — the guided first-time setup command. This
prompt creates the core setup logic and the flag-based CLI mode. Interactive
mode (huh prompts) comes in prompt 05, guided mode (Claude session) in 06.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 2 prompt 03 is complete (doctor command, PreRunE
bypass).

Read the existing code first. Understand:
- `internal/doctor/doctor.go` — `RunAll()`, `Report`, `AllPassed()`
- `internal/config/config.go` — `Home()`, `EnsureDirs()`,
  `ValidateWorldName()`
- `internal/config/world_config.go` — `DefaultWorldConfig()`,
  `WriteWorldConfig()`
- `internal/store/store.go` — `OpenWorld()`, `OpenSphere()`
- `internal/store/worlds.go` — `RegisterWorld()`
- `cmd/world.go` — `worldInitCmd` (existing world init implementation)
- `cmd/root.go` — PersistentPreRunE bypass for "init"

---

## Task 1: Setup Engine

**Create** `internal/setup/setup.go`.

The setup engine encapsulates the initialization sequence so it can be
called from flag mode, interactive mode, and guided mode identically.

```go
package setup

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/nevinsm/sol/internal/config"
    "github.com/nevinsm/sol/internal/doctor"
    "github.com/nevinsm/sol/internal/store"
)

// Params holds the inputs for a first-time setup.
type Params struct {
    WorldName  string // required: name for the first world
    SourceRepo string // optional: path to source git repo
    SkipChecks bool   // skip doctor checks
}

// Validate checks that Params are well-formed before running setup.
func (p *Params) Validate() error {
    if p.WorldName == "" {
        return fmt.Errorf("world name is required")
    }
    return config.ValidateWorldName(p.WorldName)
}

// Result holds the output of a successful setup.
type Result struct {
    SOLHome    string
    WorldName  string
    ConfigPath string
    DBPath     string
    SourceRepo string
}

// Run executes the full first-time setup sequence.
//
// Steps:
// 1. Run doctor checks (unless SkipChecks)
// 2. Create SOL_HOME directory structure
// 3. Create .store/ and .runtime/ directories
// 4. Create world directory + outposts/
// 5. Create world database (triggers schema migration)
// 6. Register world in sphere.db
// 7. Write world.toml with defaults + source repo
//
// Returns a Result on success for display by the caller.
func Run(p Params) (*Result, error) {
    if err := p.Validate(); err != nil {
        return nil, err
    }

    // 1. Doctor checks.
    if !p.SkipChecks {
        report := doctor.RunAll()
        if !report.AllPassed() {
            // Build a useful error message listing failures.
            msg := fmt.Sprintf("%d prerequisite check(s) failed:", report.FailedCount())
            for _, c := range report.Checks {
                if !c.Passed {
                    msg += fmt.Sprintf("\n  ✗ %s: %s", c.Name, c.Message)
                    if c.Fix != "" {
                        msg += fmt.Sprintf("\n    → %s", c.Fix)
                    }
                }
            }
            msg += "\n\nRun 'sol doctor' for full details, or use --skip-checks to bypass."
            return nil, fmt.Errorf("%s", msg)
        }
    }

    home := config.Home()

    // 2. Create SOL_HOME.
    if err := os.MkdirAll(home, 0o755); err != nil {
        return nil, fmt.Errorf("failed to create SOL_HOME (%s): %w", home, err)
    }

    // 3. Create .store/ and .runtime/.
    if err := config.EnsureDirs(); err != nil {
        return nil, fmt.Errorf("failed to create directories: %w", err)
    }

    // 4. Create world directory + outposts/.
    worldDir := config.WorldDir(p.WorldName)
    if err := os.MkdirAll(filepath.Join(worldDir, "outposts"), 0o755); err != nil {
        return nil, fmt.Errorf("failed to create world directory: %w", err)
    }

    // 5. Create world database.
    worldStore, err := store.OpenWorld(p.WorldName)
    if err != nil {
        return nil, fmt.Errorf("failed to create world database: %w", err)
    }
    worldStore.Close()

    // 6. Register in sphere.db.
    sphereStore, err := store.OpenSphere()
    if err != nil {
        return nil, fmt.Errorf("failed to open sphere database: %w", err)
    }
    if err := sphereStore.RegisterWorld(p.WorldName, p.SourceRepo); err != nil {
        sphereStore.Close()
        return nil, fmt.Errorf("failed to register world: %w", err)
    }
    sphereStore.Close()

    // 7. Write world.toml.
    cfg := config.DefaultWorldConfig()
    cfg.World.SourceRepo = p.SourceRepo
    if err := config.WriteWorldConfig(p.WorldName, cfg); err != nil {
        return nil, fmt.Errorf("failed to write world config: %w", err)
    }

    return &Result{
        SOLHome:    home,
        WorldName:  p.WorldName,
        ConfigPath: config.WorldConfigPath(p.WorldName),
        DBPath:     filepath.Join(config.StoreDir(), p.WorldName+".db"),
        SourceRepo: p.SourceRepo,
    }, nil
}
```

---

## Task 2: Setup Tests

**Create** `internal/setup/setup_test.go`.

All tests use `t.TempDir()` for `SOL_HOME`.

```go
func TestRunBasic(t *testing.T)
    // Set SOL_HOME to a non-existent path inside t.TempDir().
    // Run with WorldName="myworld", SkipChecks=true.
    // Verify: Result has correct paths.
    // Verify: SOL_HOME directory created.
    // Verify: .store/ directory created.
    // Verify: world.toml exists.
    // Verify: myworld.db exists.
    // Verify: myworld/outposts/ directory exists.

func TestRunWithSourceRepo(t *testing.T)
    // Create a temp directory as a fake source repo.
    // Run with WorldName="myworld", SourceRepo=<tempdir>, SkipChecks=true.
    // Verify: Result.SourceRepo is set.
    // Load world.toml and verify source_repo field.

func TestRunInvalidWorldName(t *testing.T)
    // Run with WorldName="" → error "world name is required".
    // Run with WorldName="store" → error (reserved name).

func TestRunDoctorFails(t *testing.T)
    // Set SOL_HOME to a non-existent path.
    // Manipulate PATH to remove tmux (or use a separate approach).
    // Run with SkipChecks=false.
    // If doctor reports failures, verify the error message includes
    // failure details.
    //
    // Note: This test is environment-dependent. If all prerequisites
    // are present, doctor will pass and this test should verify that
    // setup succeeds (not fails). Adjust assertion accordingly.

func TestRunSkipChecks(t *testing.T)
    // Run with SkipChecks=true.
    // Verify: setup completes regardless of doctor results.

func TestRunIdempotent(t *testing.T)
    // Run setup twice for the same world name.
    // Second run should fail with "already initialized" because
    // world.toml already exists (setup calls store.RegisterWorld
    // which is idempotent, but WriteWorldConfig on an existing file
    // should be handled — verify the actual behavior and test it).
    //
    // Actually: setup.Run doesn't check for existing world.toml.
    // Add a check at the start of Run: if world.toml already exists,
    // return an error.

func TestValidateParams(t *testing.T)
    // Empty WorldName → error.
    // Reserved name → error.
    // Valid name → nil.
```

**Important:** After writing the test for idempotency, add an
existence check at the top of `Run()`:

```go
// Check if world already initialized.
tomlPath := config.WorldConfigPath(p.WorldName)
if _, err := os.Stat(tomlPath); err == nil {
    return nil, fmt.Errorf("world %q is already initialized", p.WorldName)
}
```

---

## Task 3: Init Command (Flag-Based Mode)

**Create** `cmd/init.go`.

```go
package cmd

import (
    "fmt"

    "github.com/nevinsm/sol/internal/setup"
    "github.com/spf13/cobra"
)

var (
    initName       string
    initSourceRepo string
    initSkipChecks bool
)

var initCmd = &cobra.Command{
    Use:   "init",
    Short: "Initialize sol for first-time use",
    Long: `Set up SOL_HOME directory structure and create your first world.

Three modes:
  Flag mode:        sol init --name=myworld [--source-repo=/path]
  Interactive mode: sol init (prompts for input when stdin is a TTY)
  Guided mode:      sol init --guided (Claude-powered setup conversation)

Runs prerequisite checks (sol doctor) by default. Use --skip-checks to bypass.`,
    Args: cobra.NoArgs,
    RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
    // Flag-based mode: if --name is provided, run directly.
    if initName != "" {
        return runFlagInit()
    }

    // If --name is not provided, we need interactive or guided mode.
    // Those are implemented in prompts 05 and 06.
    // For now, return an error asking for --name.
    return fmt.Errorf("--name flag is required (interactive mode coming soon)")
}

func runFlagInit() error {
    params := setup.Params{
        WorldName:  initName,
        SourceRepo: initSourceRepo,
        SkipChecks: initSkipChecks,
    }

    result, err := setup.Run(params)
    if err != nil {
        return err
    }

    printInitSuccess(result)
    return nil
}

func printInitSuccess(result *setup.Result) {
    fmt.Printf("sol initialized successfully!\n\n")
    fmt.Printf("  SOL_HOME:  %s\n", result.SOLHome)
    fmt.Printf("  World:     %s\n", result.WorldName)
    fmt.Printf("  Config:    %s\n", result.ConfigPath)
    fmt.Printf("  Database:  %s\n", result.DBPath)

    sourceDisplay := result.SourceRepo
    if sourceDisplay == "" {
        sourceDisplay = "(none)"
    }
    fmt.Printf("  Source:    %s\n", sourceDisplay)

    fmt.Printf("\nNext steps:\n")
    fmt.Printf("  sol store create --world=%s --title=\"First task\"\n", result.WorldName)
    fmt.Printf("  sol cast <work-item-id> %s\n", result.WorldName)
}

func init() {
    rootCmd.AddCommand(initCmd)
    initCmd.Flags().StringVar(&initName, "name", "", "world name (required in flag mode)")
    initCmd.Flags().StringVar(&initSourceRepo, "source-repo", "", "path to source git repository")
    initCmd.Flags().BoolVar(&initSkipChecks, "skip-checks", false, "skip prerequisite checks")
}
```

---

## Task 4: Integration Tests

**Add** to `test/integration/` (create `init_test.go` or extend
existing):

```go
func TestInitFlagMode(t *testing.T)
    // Set SOL_HOME to a non-existent path inside t.TempDir().
    // Run: sol init --name=myworld --skip-checks
    // Verify: exit 0
    // Verify: SOL_HOME created
    // Verify: world.toml exists
    // Verify: myworld.db exists
    // Verify: outposts/ directory exists

func TestInitFlagModeWithSourceRepo(t *testing.T)
    // Create a temp dir as source repo.
    // Run: sol init --name=myworld --source-repo=<tempdir> --skip-checks
    // Verify: world.toml contains source_repo.

func TestInitRequiresName(t *testing.T)
    // Run: sol init (no flags)
    // Should error with "--name flag is required"

func TestInitAlreadyInitialized(t *testing.T)
    // Run: sol init --name=myworld --skip-checks → success
    // Run: sol init --name=myworld --skip-checks → error "already initialized"

func TestInitThenWorldOperations(t *testing.T)
    // Run: sol init --name=myworld --skip-checks
    // Run: sol store create --world=myworld --title="test"
    // Verify: work item created (exit 0)
    // Run: sol world list
    // Verify: output contains "myworld"
    // Run: sol world status myworld
    // Verify: exit 0, output contains "Config:"

func TestInitRunsDoctorByDefault(t *testing.T)
    // Run: sol init --name=myworld (no --skip-checks)
    // If doctor passes (tmux+git available): setup succeeds.
    // If doctor fails: error message includes failed check details.
```

---

## Task 5: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-init-test
   rm -rf /tmp/sol-init-test

   # Flag mode
   bin/sol init --name=myworld --skip-checks
   cat /tmp/sol-init-test/myworld/world.toml
   bin/sol world list
   bin/sol store create --world=myworld --title="test"
   bin/sol status myworld

   # Already initialized
   bin/sol init --name=myworld --skip-checks 2>&1 | grep "already initialized"

   rm -rf /tmp/sol-init-test
   ```

---

## Guidelines

- The setup engine (`internal/setup/`) is the single source of truth
  for the initialization sequence. All three modes (flag, interactive,
  guided) call `setup.Run()` with the same `Params`.
- The `runInit` function is structured to be extended in prompts 05
  and 06 with interactive and guided modes. The `if initName != ""`
  branch handles flag mode; the else branch will dispatch to
  interactive or guided mode.
- `sol init` runs doctor by default. This catches common problems
  before they manifest as cryptic errors during `cast` or `forge`.
  `--skip-checks` exists for CI or environments where prerequisites
  are known-good.
- Error messages from doctor failures include the full failure details
  inline — the operator shouldn't have to run `sol doctor` separately
  to understand what went wrong.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(init): add setup engine and sol init flag-based mode`
