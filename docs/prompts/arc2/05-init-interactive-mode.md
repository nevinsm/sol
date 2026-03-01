# Prompt 05: Arc 2 — Init Interactive Mode

You are adding interactive prompts to `sol init` using charmbracelet/huh.
When the operator runs `sol init` without `--name` and stdin is a TTY,
interactive mode presents a form to collect setup parameters.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 2 prompt 04 is complete (setup engine,
flag-based init command).

Read the existing code first. Understand:
- `cmd/init.go` — current `runInit()` with flag-mode branch and
  placeholder else branch
- `internal/setup/setup.go` — `Params`, `Run()`, `Result`
- `go.mod` — verify `charmbracelet/huh` is a direct dependency

Familiarize yourself with charmbracelet/huh:
- huh is a Go library for building interactive terminal forms
- Key types: `huh.Form`, `huh.Group`, `huh.Input`, `huh.Select`,
  `huh.Confirm`
- A form contains groups of fields; `form.Run()` blocks until complete
- Fields bind to pointer variables for result collection

---

## Task 1: TTY Detection

**Modify** `cmd/init.go`.

Add a helper to detect if stdin is a terminal:

```go
import "golang.org/x/term"

func isTerminal() bool {
    return term.IsTerminal(int(os.Stdin.Fd()))
}
```

Note: `golang.org/x/term` may already be an indirect dependency
(pulled in by huh). If not, add it:
```bash
go get golang.org/x/term
```

---

## Task 2: Interactive Form

**Modify** `cmd/init.go`.

Update `runInit` to dispatch to interactive mode when no `--name` flag
and stdin is a TTY:

```go
func runInit(cmd *cobra.Command, args []string) error {
    // Flag mode: --name provided → run directly.
    if initName != "" {
        return runFlagInit()
    }

    // Guided mode: --guided flag → Claude session (prompt 06).
    // (placeholder — will be added in prompt 06)

    // Interactive mode: stdin is a TTY → prompt for input.
    if isTerminal() {
        return runInteractiveInit()
    }

    // Non-interactive, no flags → error.
    return fmt.Errorf("--name flag is required when stdin is not a terminal\n" +
        "Usage: sol init --name=<world> [--source-repo=<path>]")
}
```

Implement the interactive form:

```go
func runInteractiveInit() error {
    var (
        worldName  string
        sourceRepo string
        skipChecks bool
    )

    form := huh.NewForm(
        huh.NewGroup(
            huh.NewNote().
                Title("sol init").
                Description("Set up sol for first-time use.\n"+
                    "This creates SOL_HOME and your first world."),
        ),
        huh.NewGroup(
            huh.NewInput().
                Title("World name").
                Description("Name for your first world (e.g., 'myproject')").
                Placeholder("myworld").
                Value(&worldName).
                Validate(func(s string) error {
                    if s == "" {
                        return fmt.Errorf("world name is required")
                    }
                    return config.ValidateWorldName(s)
                }),

            huh.NewInput().
                Title("Source repository").
                Description("Path to your project's git repo (optional)").
                Placeholder("/path/to/repo").
                Value(&sourceRepo),

            huh.NewConfirm().
                Title("Skip prerequisite checks?").
                Description("Run 'sol doctor' checks before setup").
                Affirmative("Skip checks").
                Negative("Run checks").
                Value(&skipChecks),
        ),
    )

    if err := form.Run(); err != nil {
        return fmt.Errorf("setup cancelled: %w", err)
    }

    // Validate source repo path if provided.
    if sourceRepo != "" {
        info, err := os.Stat(sourceRepo)
        if err != nil {
            return fmt.Errorf("source repo path %q: %w", sourceRepo, err)
        }
        if !info.IsDir() {
            return fmt.Errorf("source repo path %q is not a directory", sourceRepo)
        }
    }

    params := setup.Params{
        WorldName:  worldName,
        SourceRepo: sourceRepo,
        SkipChecks: skipChecks,
    }

    result, err := setup.Run(params)
    if err != nil {
        return err
    }

    printInitSuccess(result)
    return nil
}
```

### Import updates

Add to imports:

```go
"github.com/charmbracelet/huh"
"github.com/nevinsm/sol/internal/config"
```

---

## Task 3: Source Repo Validation in Flag Mode

While we're here, add source repo path validation to `runFlagInit()`
as well (matching what we added in interactive mode):

```go
func runFlagInit() error {
    // Validate source repo if provided.
    if initSourceRepo != "" {
        info, err := os.Stat(initSourceRepo)
        if err != nil {
            return fmt.Errorf("source repo path %q: %w", initSourceRepo, err)
        }
        if !info.IsDir() {
            return fmt.Errorf("source repo path %q is not a directory", initSourceRepo)
        }
    }

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
```

---

## Task 4: Tests

Interactive mode is difficult to test programmatically (requires a
pseudo-TTY). Focus on testing the dispatch logic and error paths:

### Unit tests

**Modify** `cmd/init.go` test file or create one:

```go
func TestInitDispatchFlagMode(t *testing.T)
    // Run: sol init --name=test --skip-checks
    // Should succeed (flag mode, no TTY needed).

func TestInitDispatchNoFlagsNoTTY(t *testing.T)
    // Run: sol init (no flags, pipe stdin to suppress TTY)
    // Should error with "--name flag is required when stdin is not a terminal"

func TestInitSourceRepoValidation(t *testing.T)
    // Run: sol init --name=test --source-repo=/nonexistent --skip-checks
    // Should error with path not found.
    // Run: sol init --name=test --source-repo=<temp-file> --skip-checks
    // Should error with "not a directory".
```

### Integration tests

```go
func TestInitInteractiveRequiresTTY(t *testing.T)
    // Run: echo "" | sol init
    // (piped stdin → not a TTY)
    // Should error, not hang waiting for input.
```

---

## Task 5: Verify

1. `go mod tidy` — in case golang.org/x/term needed adding
2. `make build` — compiles cleanly
3. `make test` — all existing and new tests pass
4. Manual smoke test (requires a terminal):
   ```bash
   export SOL_HOME=/tmp/sol-interactive-test
   rm -rf /tmp/sol-interactive-test

   # Interactive mode — run in a real terminal
   bin/sol init
   # Should show the huh form with world name, source repo, skip checks

   rm -rf /tmp/sol-interactive-test

   # Non-interactive (piped)
   echo "" | bin/sol init 2>&1
   # Should show error about --name flag

   rm -rf /tmp/sol-interactive-test
   ```

---

## Guidelines

- The huh form is deliberately simple — three fields in one group.
  Don't over-design the form with complex navigation or conditional
  fields. The operator should be in and out in 10 seconds.
- The `huh.NewNote()` at the top provides context without being a
  field. It's the form's "header."
- `skipChecks` defaults to `false` (Confirm fields default to the
  Negative option). Most operators should run checks.
- Source repo validation happens after the form completes, not as a
  form-level validator. This avoids the form rejecting partial paths
  as the operator types.
- `form.Run()` returns an error if the user presses Ctrl+C (interrupt).
  Wrap it as "setup cancelled" for a clean exit message.
- The interactive mode still calls `setup.Run()` — same engine, same
  behavior, different input method.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(init): add interactive mode with huh prompts`
