# Prompt 03: Arc 2 — Doctor Command + PersistentPreRunE Bypass

You are wiring the `sol doctor` command to the prerequisite check engine
and adding PersistentPreRunE bypass so that `doctor` and `init` can run
before SOL_HOME exists.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 2 prompt 02 is complete (doctor engine in
`internal/doctor/`).

Read the existing code first. Understand:
- `internal/doctor/doctor.go` — `RunAll()`, `CheckResult`, `Report`
- `cmd/root.go` — `PersistentPreRunE` calls `config.EnsureDirs()`
- `cmd/status.go` — how `--json` flag works with `encoding/json`
- `cmd/world.go` — command registration pattern

---

## Task 1: PersistentPreRunE Bypass

**Modify** `cmd/root.go`.

The current `PersistentPreRunE` calls `config.EnsureDirs()` which
creates `$SOL_HOME/.store/` and `$SOL_HOME/.runtime/`. This fails if
SOL_HOME doesn't exist yet — which is the exact situation `sol doctor`
and `sol init` need to handle.

Update `PersistentPreRunE` to skip `EnsureDirs` for these commands:

```go
PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
    // doctor and init must work before SOL_HOME exists.
    switch cmd.Name() {
    case "doctor", "init":
        return nil
    }
    return config.EnsureDirs()
},
```

**Why `cmd.Name()` not `cmd.Use`:** `cmd.Name()` returns the bare
command name ("doctor"), while `cmd.Use` may contain argument
placeholders ("doctor [flags]"). `Name()` is the reliable check.

**Why not a custom annotation:** The bypass list is exactly two
commands and won't grow. A simple switch is clearer than annotation
metadata.

---

## Task 2: Doctor Command

**Create** `cmd/doctor.go`.

```go
package cmd

import (
    "encoding/json"
    "fmt"
    "os"

    "github.com/nevinsm/sol/internal/doctor"
    "github.com/spf13/cobra"
)

var doctorJSON bool

var doctorCmd = &cobra.Command{
    Use:   "doctor",
    Short: "Check system prerequisites",
    Long: `Validate that all prerequisites for running sol are met.

Checks: tmux, git, claude CLI, SOL_HOME directory, SQLite WAL support.

Exit code 0 if all checks pass, 1 if any check fails.`,
    Args: cobra.NoArgs,
    RunE: func(cmd *cobra.Command, args []string) error {
        report := doctor.RunAll()

        if doctorJSON {
            enc := json.NewEncoder(os.Stdout)
            enc.SetIndent("", "  ")
            return enc.Encode(report)
        }

        // Human-readable output.
        for _, check := range report.Checks {
            if check.Passed {
                fmt.Printf("  ✓ %-12s %s\n", check.Name, check.Message)
            } else {
                fmt.Printf("  ✗ %-12s %s\n", check.Name, check.Message)
                if check.Fix != "" {
                    fmt.Printf("    → %s\n", check.Fix)
                }
            }
        }

        fmt.Println()
        if report.AllPassed() {
            fmt.Println("All checks passed. Ready to run sol.")
            return nil
        }

        fmt.Printf("%d check(s) failed.\n", report.FailedCount())
        os.Exit(1)
        return nil
    },
}

func init() {
    rootCmd.AddCommand(doctorCmd)
    doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output as JSON")
}
```

### Output format (human)

```
  ✓ tmux         tmux 3.4 (/usr/bin/tmux)
  ✓ git          git version 2.43.0 (/usr/bin/git)
  ✗ claude       claude CLI not found in PATH
    → Install Claude Code: npm install -g @anthropic-ai/claude-code
  ✓ sol_home     /home/user/sol
  ✓ sqlite_wal   WAL mode supported

1 check(s) failed.
```

### Output format (JSON)

```json
{
  "checks": [
    {
      "name": "tmux",
      "passed": true,
      "message": "tmux 3.4 (/usr/bin/tmux)",
      "fix": ""
    },
    ...
  ]
}
```

### Exit codes

- `0` — all checks passed
- `1` — one or more checks failed

---

## Task 3: Tests

### Unit test for PersistentPreRunE bypass

**Add** to an appropriate test file (or create `cmd/root_test.go` if
none exists):

```go
func TestDoctorBypassesEnsureDirs(t *testing.T)
    // Set SOL_HOME to a non-existent directory inside t.TempDir().
    // Run: sol doctor
    // Should succeed (exit 0 or just no error from EnsureDirs).
    // The SOL_HOME directory should NOT have been created.
```

### Integration tests

**Add** to `test/integration/` (create `doctor_test.go` or add to an
existing arc2 test file):

```go
func TestDoctorRuns(t *testing.T)
    // Run: sol doctor
    // Should exit 0 (assuming tmux + git are available in CI).
    // Output contains "✓ tmux" and "✓ git".

func TestDoctorJSON(t *testing.T)
    // Run: sol doctor --json
    // Parse output as JSON.
    // Verify structure: array of checks with name, passed, message fields.

func TestDoctorBeforeInit(t *testing.T)
    // Set SOL_HOME to a path that doesn't exist.
    // Run: sol doctor
    // Should succeed — doctor works before SOL_HOME is created.
    // Verify SOL_HOME was NOT created as a side effect.

func TestInitBypassesEnsureDirs(t *testing.T)
    // Set SOL_HOME to a path that doesn't exist (but parent exists).
    // Run: sol init --name=test --source-repo=/tmp
    // Should succeed — init creates SOL_HOME itself.
    // (This test validates the bypass; full init testing is in prompt 04.)
```

Note: `TestInitBypassesEnsureDirs` may fail until prompt 04 implements
`sol init`. Mark it as a TODO or skip it for now and implement it in
prompt 04.

---

## Task 4: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Manual smoke test:
   ```bash
   # Normal operation
   bin/sol doctor

   # JSON output
   bin/sol doctor --json

   # Before SOL_HOME exists
   export SOL_HOME=/tmp/sol-doctor-test-nonexistent
   rm -rf /tmp/sol-doctor-test-nonexistent
   bin/sol doctor
   # Should work, SOL_HOME should NOT be created
   ls /tmp/sol-doctor-test-nonexistent 2>&1 | grep -q "No such file" && echo "PASS: no side effect"
   ```

---

## Guidelines

- The human-readable output uses simple `✓`/`✗` markers. Lipgloss
  styling will be added in prompt 08 — keep this functional for now.
- `os.Exit(1)` for failed checks is intentional — this is a diagnostic
  tool, not a pipeline command. The exit code is the signal.
- The PersistentPreRunE bypass is the simplest correct solution. Do not
  over-engineer it with annotations, interface flags, or middleware.
- `sol doctor` never modifies the filesystem. It is read-only by
  design (except for the temp file in CheckSQLiteWAL, which is cleaned
  up immediately).
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(doctor): add sol doctor command with PersistentPreRunE bypass`
