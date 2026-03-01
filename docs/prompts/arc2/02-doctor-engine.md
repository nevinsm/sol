# Prompt 02: Arc 2 — Doctor Engine

You are building the prerequisite check engine for `sol doctor`. This
prompt creates the core checking logic — no CLI wiring yet.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 2 prompt 01 is complete (ADR-0012, lipgloss + huh
dependencies added).

Read the existing code first. Understand:
- `internal/config/config.go` — `Home()`, `StoreDir()`, `RuntimeDir()`,
  `EnsureDirs()`
- `internal/session/session.go` — tmux session management (how tmux is
  invoked)
- `internal/prefect/pidfile.go` — `ReadPID()`, `IsRunning()` pattern
- `internal/consul/consul.go` — heartbeat reading pattern

---

## Task 1: Check Types

**Create** `internal/doctor/doctor.go`.

```go
package doctor

// CheckResult represents the outcome of a single prerequisite check.
type CheckResult struct {
    Name    string `json:"name"`    // short identifier: "tmux", "git", "claude", etc.
    Passed  bool   `json:"passed"`
    Message string `json:"message"` // human-readable status or error detail
    Fix     string `json:"fix"`     // actionable fix suggestion (empty if passed)
}

// Report holds the results of all prerequisite checks.
type Report struct {
    Checks []CheckResult `json:"checks"`
}

// AllPassed returns true if every check passed.
func (r *Report) AllPassed() bool {
    for _, c := range r.Checks {
        if !c.Passed {
            return false
        }
    }
    return true
}

// FailedCount returns the number of failed checks.
func (r *Report) FailedCount() int {
    n := 0
    for _, c := range r.Checks {
        if !c.Passed {
            n++
        }
    }
    return n
}
```

---

## Task 2: Individual Checks

Add the following check functions to `internal/doctor/doctor.go`. Each
returns a `CheckResult`.

### CheckTmux

```go
// CheckTmux verifies tmux is installed and executable.
func CheckTmux() CheckResult {
    path, err := exec.LookPath("tmux")
    if err != nil {
        return CheckResult{
            Name:    "tmux",
            Passed:  false,
            Message: "tmux not found in PATH",
            Fix:     "Install tmux: 'brew install tmux' (macOS) or 'apt install tmux' (Linux)",
        }
    }
    // Run tmux -V to get version string.
    out, err := exec.Command(path, "-V").Output()
    if err != nil {
        return CheckResult{
            Name:    "tmux",
            Passed:  false,
            Message: fmt.Sprintf("tmux found at %s but failed to run: %v", path, err),
            Fix:     "Check tmux installation — it may be corrupted or missing dependencies",
        }
    }
    version := strings.TrimSpace(string(out))
    return CheckResult{
        Name:    "tmux",
        Passed:  true,
        Message: fmt.Sprintf("%s (%s)", version, path),
    }
}
```

### CheckGit

```go
// CheckGit verifies git is installed and executable.
func CheckGit() CheckResult {
    path, err := exec.LookPath("git")
    if err != nil {
        return CheckResult{
            Name:    "git",
            Passed:  false,
            Message: "git not found in PATH",
            Fix:     "Install git: 'brew install git' (macOS) or 'apt install git' (Linux)",
        }
    }
    out, err := exec.Command(path, "--version").Output()
    if err != nil {
        return CheckResult{
            Name:    "git",
            Passed:  false,
            Message: fmt.Sprintf("git found at %s but failed to run: %v", path, err),
            Fix:     "Check git installation",
        }
    }
    version := strings.TrimSpace(string(out))
    return CheckResult{
        Name:    "git",
        Passed:  true,
        Message: fmt.Sprintf("%s (%s)", version, path),
    }
}
```

### CheckClaude

```go
// CheckClaude verifies the Claude CLI is installed and executable.
func CheckClaude() CheckResult {
    path, err := exec.LookPath("claude")
    if err != nil {
        return CheckResult{
            Name:    "claude",
            Passed:  false,
            Message: "claude CLI not found in PATH",
            Fix:     "Install Claude Code: npm install -g @anthropic-ai/claude-code",
        }
    }
    // Just verify it's executable — don't run a full command
    // as that might trigger auth flows.
    return CheckResult{
        Name:    "claude",
        Passed:  true,
        Message: fmt.Sprintf("found at %s", path),
    }
}
```

### CheckSOLHome

```go
// CheckSOLHome verifies SOL_HOME exists and is writable.
// If SOL_HOME doesn't exist yet, checks that the parent directory is
// writable (so init can create it).
func CheckSOLHome() CheckResult {
    home := config.Home()

    info, err := os.Stat(home)
    if os.IsNotExist(err) {
        // SOL_HOME doesn't exist — check parent is writable.
        parent := filepath.Dir(home)
        if err := checkWritable(parent); err != nil {
            return CheckResult{
                Name:    "sol_home",
                Passed:  false,
                Message: fmt.Sprintf("SOL_HOME (%s) does not exist and parent is not writable", home),
                Fix:     fmt.Sprintf("Create directory manually: mkdir -p %s", home),
            }
        }
        return CheckResult{
            Name:    "sol_home",
            Passed:  true,
            Message: fmt.Sprintf("%s (will be created by 'sol init')", home),
        }
    } else if err != nil {
        return CheckResult{
            Name:    "sol_home",
            Passed:  false,
            Message: fmt.Sprintf("cannot stat SOL_HOME (%s): %v", home, err),
            Fix:     "Check directory permissions",
        }
    }

    if !info.IsDir() {
        return CheckResult{
            Name:    "sol_home",
            Passed:  false,
            Message: fmt.Sprintf("SOL_HOME (%s) exists but is not a directory", home),
            Fix:     fmt.Sprintf("Remove the file and create directory: rm %s && mkdir -p %s", home, home),
        }
    }

    if err := checkWritable(home); err != nil {
        return CheckResult{
            Name:    "sol_home",
            Passed:  false,
            Message: fmt.Sprintf("SOL_HOME (%s) is not writable", home),
            Fix:     fmt.Sprintf("Fix permissions: chmod u+w %s", home),
        }
    }

    return CheckResult{
        Name:    "sol_home",
        Passed:  true,
        Message: home,
    }
}
```

### CheckSQLiteWAL

```go
// CheckSQLiteWAL verifies SQLite WAL mode works by creating a temp
// database and enabling WAL.
func CheckSQLiteWAL() CheckResult {
    dir, err := os.MkdirTemp("", "sol-doctor-wal-*")
    if err != nil {
        return CheckResult{
            Name:    "sqlite_wal",
            Passed:  false,
            Message: fmt.Sprintf("cannot create temp directory: %v", err),
            Fix:     "Check temp directory permissions and disk space",
        }
    }
    defer os.RemoveAll(dir)

    dbPath := filepath.Join(dir, "test.db")
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return CheckResult{
            Name:    "sqlite_wal",
            Passed:  false,
            Message: fmt.Sprintf("cannot open SQLite database: %v", err),
            Fix:     "This is unexpected with the embedded SQLite driver — file a bug",
        }
    }
    defer db.Close()

    var mode string
    err = db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode)
    if err != nil {
        return CheckResult{
            Name:    "sqlite_wal",
            Passed:  false,
            Message: fmt.Sprintf("failed to enable WAL mode: %v", err),
            Fix:     "Check filesystem supports WAL (some network filesystems don't)",
        }
    }

    if strings.ToLower(mode) != "wal" {
        return CheckResult{
            Name:    "sqlite_wal",
            Passed:  false,
            Message: fmt.Sprintf("WAL mode not supported (got journal_mode=%s)", mode),
            Fix:     "SOL_HOME must be on a local filesystem that supports WAL locks",
        }
    }

    return CheckResult{
        Name:    "sqlite_wal",
        Passed:  true,
        Message: "WAL mode supported",
    }
}
```

### Helper

```go
// checkWritable tests if a directory is writable by creating and
// removing a temp file.
func checkWritable(dir string) error {
    f, err := os.CreateTemp(dir, ".sol-doctor-*")
    if err != nil {
        return err
    }
    name := f.Name()
    f.Close()
    return os.Remove(name)
}
```

---

## Task 3: RunAll Function

```go
// RunAll executes all prerequisite checks and returns a report.
// Checks always run in full — a failing check does not short-circuit.
func RunAll() *Report {
    report := &Report{}
    report.Checks = append(report.Checks, CheckTmux())
    report.Checks = append(report.Checks, CheckGit())
    report.Checks = append(report.Checks, CheckClaude())
    report.Checks = append(report.Checks, CheckSOLHome())
    report.Checks = append(report.Checks, CheckSQLiteWAL())
    return report
}
```

---

## Task 4: Tests

**Create** `internal/doctor/doctor_test.go`.

Use the SQLite driver import: `_ "modernc.org/sqlite"` (imported by
the main package but needed here for standalone test compilation).

Actually — the SQLite driver is registered by `database/sql` globally.
The `CheckSQLiteWAL` function uses `sql.Open("sqlite", ...)` which
requires the driver to be registered. Add a blank import in `doctor.go`:

```go
import _ "modernc.org/sqlite"
```

### Tests

```go
func TestCheckTmux(t *testing.T)
    // tmux should be installed in the test environment.
    // Verify Passed=true, Message contains "tmux".
    // If tmux is not available, skip the test with t.Skip.

func TestCheckGit(t *testing.T)
    // git should be installed in the test environment.
    // Verify Passed=true, Message contains "git version".

func TestCheckClaude(t *testing.T)
    // Claude may or may not be installed.
    // If found: Passed=true.
    // If not found: Passed=false, Fix is non-empty.
    // Either outcome is valid — just verify the result is well-formed.

func TestCheckSOLHomeExists(t *testing.T)
    // Set SOL_HOME to t.TempDir() (exists, writable).
    // Verify Passed=true.

func TestCheckSOLHomeNotExists(t *testing.T)
    // Set SOL_HOME to a path inside t.TempDir() that doesn't exist yet.
    // Parent exists and is writable.
    // Verify Passed=true, Message contains "will be created".

func TestCheckSOLHomeNotWritable(t *testing.T)
    // Create a directory, chmod 0o444.
    // Set SOL_HOME to that directory.
    // Verify Passed=false, Fix is non-empty.
    // Skip on root (root can always write).

func TestCheckSQLiteWAL(t *testing.T)
    // Should always pass with the embedded driver.
    // Verify Passed=true.

func TestRunAll(t *testing.T)
    // Set SOL_HOME to t.TempDir().
    // Call RunAll().
    // Verify report has 5 checks.
    // Verify AllPassed() returns consistent result.

func TestReportAllPassed(t *testing.T)
    // Build a Report with all passing checks → AllPassed() = true.
    // Build a Report with one failure → AllPassed() = false.

func TestReportFailedCount(t *testing.T)
    // Build a Report with 2 failures → FailedCount() = 2.
```

---

## Task 5: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Verify the doctor package is self-contained:
   ```bash
   go test ./internal/doctor/ -v
   ```

---

## Guidelines

- The doctor engine is pure logic — no CLI, no terminal styling. That
  comes in prompt 03 (CLI) and prompt 08 (lipgloss rendering).
- Each check function is independent and can be tested in isolation.
- Fix messages must be actionable: tell the operator exactly what to
  install or fix, including specific commands.
- `CheckClaude` does NOT run `claude --version` or similar — Claude CLI
  commands may trigger authentication flows. Just verify the binary
  exists in PATH.
- `CheckSOLHome` handles three cases: exists+writable, not-exists but
  parent-writable, not-writable. The "not exists" case is valid
  because `sol init` will create it.
- `CheckSQLiteWAL` creates a real database in a temp directory. This
  catches filesystem-level WAL issues (e.g., NFS mounts that don't
  support WAL locking).
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(doctor): add prerequisite check engine`
