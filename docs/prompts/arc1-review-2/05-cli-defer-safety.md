# Prompt 05: CLI Defer Safety and Final Verification

You are fixing the `os.Exit` inside `RunE` anti-pattern that causes
deferred cleanup to be skipped, and running a final verification sweep.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 review-2 prompt 04 is complete. All tests pass.

Read all files mentioned below before making changes.

---

## Task 1: Understand the problem

Six commands call `os.Exit()` inside their `RunE` function to
communicate a non-zero exit code. `os.Exit` terminates the process
immediately without running deferred functions, which means store
connections opened with `defer store.Close()` are leaked.

The commands:
- `cmd/status.go` — `os.Exit(result.Health())` (exit code signals health)
- `cmd/session.go` — `os.Exit(status.ExitCode())` (health check result)
- `cmd/consul.go` — `os.Exit(1)` (consul not running)
- `cmd/workflow.go` — `os.Exit(1)` (no active workflow)
- `cmd/mail.go` — `os.Exit(1)` (unread mail)
- `cmd/forge.go` — `os.Exit(1)` (gate failure)

---

## Task 2: Create an exit code error type

**File:** `cmd/root.go` (or a new `cmd/errors.go` if root.go is crowded)

Create a sentinel error type that encodes an exit code:

```go
// exitError wraps an exit code for commands that need to communicate
// non-zero status. The root command's Execute path translates this
// into an os.Exit call after all defers have run.
type exitError struct {
    code int
}

func (e *exitError) Error() string {
    return fmt.Sprintf("exit status %d", e.code)
}

// ExitCode returns the exit code, or 0 if the error is nil or not an
// exitError.
func ExitCode(err error) int {
    var ee *exitError
    if errors.As(err, &ee) {
        return ee.code
    }
    return 0
}
```

---

## Task 3: Update `main.go` to handle exit codes

**File:** `main.go`

The current `main.go` likely calls `cmd.Execute()` or similar. Update
it to extract exit codes from the error:

```go
func main() {
    if err := cmd.Execute(); err != nil {
        code := cmd.ExitCode(err)
        if code != 0 {
            os.Exit(code)
        }
        // Non-exitError errors are already printed by cobra.
        os.Exit(1)
    }
}
```

Read the current `main.go` first to understand the existing structure.
Adapt the above pattern to fit. The key requirement is that `os.Exit`
only runs AFTER the cobra command tree has returned, ensuring all defers
in RunE have completed.

---

## Task 4: Replace `os.Exit` calls with `exitError` returns

### `cmd/status.go`

Replace:
```go
os.Exit(result.Health())
```

With:
```go
if code := result.Health(); code != 0 {
    return &exitError{code: code}
}
return nil
```

Remove the `SilenceErrors` and `SilenceUsage` fields if they were only
set to prevent cobra from printing after `os.Exit`. If they serve
another purpose (like suppressing usage on error), keep them.

### `cmd/session.go`

Replace:
```go
os.Exit(status.ExitCode())
```

With:
```go
if code := status.ExitCode(); code != 0 {
    return &exitError{code: code}
}
return nil
```

### `cmd/consul.go`

Replace:
```go
fmt.Println("consul is not running")
os.Exit(1)
```

With:
```go
fmt.Println("consul is not running")
return &exitError{code: 1}
```

### `cmd/workflow.go`

Replace:
```go
fmt.Println("no active workflow")
os.Exit(1)
```

With:
```go
fmt.Println("no active workflow")
return &exitError{code: 1}
```

### `cmd/mail.go`

Replace:
```go
os.Exit(1)
```

With:
```go
return &exitError{code: 1}
```

### `cmd/forge.go`

Replace:
```go
os.Exit(1)
```

With:
```go
return &exitError{code: 1}
```

**Important:** For each command, verify that the `os.Exit` was being
called AFTER printing output. The `exitError` return should not change
the command's visible output — only the mechanism for exiting.

---

## Task 5: Verify exit code behavior is preserved

The exit code behavior must be identical to before:

```bash
export SOL_HOME=/tmp/sol-test-exit
mkdir -p /tmp/sol-test-exit/.store
bin/sol world init exittest --source-repo=/tmp

# status with no agents should return exit code based on health
bin/sol status exittest; echo "exit: $?"
# → should print status and exit with the health code

# mail check with no unread mail should exit 0
bin/sol mail check --agent=exittest/nobody; echo "exit: $?"
# → may error on missing agent, but the exit code mechanism should work

rm -rf /tmp/sol-test-exit
```

---

## Task 6: Final review sweep — grep for remaining issues

Run these greps to verify the entire review-2 pass is clean:

```bash
# No os.Exit in RunE functions (the main fix)
grep -rn 'os\.Exit' cmd/*.go
# → should only appear in main.go (or not at all in cmd/)

# No silent time.Parse in store
grep -rn 'time\.Parse.*_ =' internal/store/*.go
# → no matches

# No shared flag vars read directly
grep -n 'forgeToolboxJSON\|caravanWorld\|caravanJSON\|wfWorld\|wfAgent' cmd/*.go | grep -v '//'
# → should not appear in RunE bodies (only in flag registration if still used)

# No RemoveWorld
grep -rn 'RemoveWorld' internal/store/ cmd/
# → no matches

# No duplicate parseVarFlags
grep -rn 'func parseVarFlags\|func parseCaravanVarFlags' cmd/
# → exactly one definition

# Reserved names include formulas
grep -n 'formulas' internal/config/config.go
# → should show it in reservedWorldNames

# All --world flags say "world name"
grep -rn '"world.*database' cmd/
# → no matches

# consul.Config has no SourceRepo
grep -n 'SourceRepo' internal/consul/consul.go
# → no matches

# config.go has tests
test -f internal/config/config_test.go && echo "PASS: config_test.go exists"

# No os.Setenv in store tests
grep -rn 'os\.Setenv.*SOL_HOME' internal/store/ internal/status/
# → no matches (all migrated to t.Setenv)
```

---

## Task 7: Verify

1. `make build` — compiles cleanly
2. `make test` — all tests pass
3. Exit code verification (Task 5 above)
4. Grep verification (Task 6 above)

---

## Guidelines

- The `exitError` type should be minimal — just an exit code. Do not
  add logging, formatting, or other features.
- Preserve the exact same user-visible output for each command. The
  only change is HOW the process exits, not WHAT it prints.
- If any command uses `SilenceErrors: true` specifically to prevent
  cobra from printing the exitError, keep that setting. But verify
  — cobra only prints errors that are not nil, and the exitError's
  `Error()` method returns "exit status N" which should not be printed
  to the user.
- If you find that cobra prints "Error: exit status 1" to the user,
  suppress it by setting `SilenceErrors: true` on those specific
  commands.
- All existing tests must continue to pass.
- Commit with message:
  `fix(cli): arc 1 review-2 — replace os.Exit in RunE with exit error type`
