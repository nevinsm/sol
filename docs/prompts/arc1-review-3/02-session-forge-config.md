# Prompt 02: Session, Forge, Config Fixes

You are fixing issues in session management, forge, config, and CLI
consistency found during the third Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** All Arc 1 review-2 prompts are complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/session/manager.go` — session manager, `tmuxCmd`, `Health`
- `internal/session/manager_test.go` — existing session tests
- `internal/forge/forge.go` — `EnsureWorktree` error wrapping
- `internal/config/world_config.go` — `WriteWorldConfig`
- `cmd/store.go` — flag access pattern
- `cmd/store_dep.go` — flag access pattern

---

## Task 1: Fix `tmuxCmd` goroutine leak

**File:** `internal/session/manager.go` (lines 84-94)

The current implementation leaks a goroutine per tmux command:

```go
func tmuxCmd(args ...string) *exec.Cmd {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    cmd := exec.CommandContext(ctx, "tmux", args...)
    go func() {
        <-ctx.Done()
        cancel()
    }()
    return cmd
}
```

The goroutine blocks on `<-ctx.Done()` until the 10-second timeout fires,
even after the command completes and its result is consumed. In
prefect/sentinel loops that call tmux commands every heartbeat, this
compounds to dozens of leaked goroutines.

**Fix:** Return both the command and a cancel function. Callers invoke
cancel after consuming the command's output.

```go
// tmuxCmd creates a tmux command with a 10-second timeout.
// The caller MUST call the returned cancel function after the command
// completes to release the context resources.
func tmuxCmd(args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := exec.CommandContext(ctx, "tmux", args...)
	return cmd, cancel
}
```

Then update **every call site** in `manager.go`. There are many — work
through them methodically. The pattern is:

**Before:**
```go
cmd := tmuxCmd("has-session", "-t", name)
return cmd.Run() == nil
```

**After:**
```go
cmd, cancel := tmuxCmd("has-session", "-t", name)
defer cancel()
return cmd.Run() == nil
```

For calls that use `cmd.CombinedOutput()` or `cmd.Output()`:

**Before:**
```go
tmux := tmuxCmd("new-session", "-d", "-s", name, "-c", workdir, cmd)
if out, err := tmux.CombinedOutput(); err != nil {
```

**After:**
```go
tmux, tmuxCancel := tmuxCmd("new-session", "-d", "-s", name, "-c", workdir, shellCmd)
defer tmuxCancel()
if out, err := tmux.CombinedOutput(); err != nil {
```

Note: in `Start()`, the parameter `cmd string` shadows the function name.
When you add `cancel` you may want to rename the `tmuxCmd` return to avoid
confusion with the `cmd` parameter. Use descriptive variable names like
`tmux, tmuxCancel` or `newSess, newSessCancel`.

Walk through every function in manager.go and update all call sites:
- `Start` — multiple tmux calls (new-session, set-environment)
- `Stop` — multiple tmux calls (send-keys, kill-session)
- `List` — display-message call
- `Health` — list-panes call
- `Capture` — capture-pane call
- `Attach` — does NOT use tmuxCmd (uses syscall.Exec), skip
- `Inject` — send-keys call
- `Exists` — has-session call

---

## Task 2: Surface health hash write failures

**File:** `internal/session/manager.go` — `Health` function (lines 255-314)

The function writes capture-hash files in three places (lines 282-285,
293-294, 302-303) and silently discards errors:

```go
_ = os.MkdirAll(filepath.Dir(hashFile), 0o755)
_ = os.WriteFile(hashFile, j, 0o644)
```

If these writes fail, the next health check will have no previous hash
to compare and will always report `Healthy`, even for hung sessions.

**Fix:** Return the hash-write error as part of the health result. Add a
wrapper that logs the error but doesn't change the health status — the
caller (sentinel/prefect) will see the error in their logs.

Change the `Health` function signature to return a third value would be
too invasive. Instead, keep the current signature but log the write
failure to stderr if it occurs. Use a simple helper:

```go
// writeHashFile writes the capture hash to disk. Errors are logged to
// stderr but not returned — a hash write failure degrades future health
// checks but does not affect the current check's result.
func writeHashFile(path string, ch captureHash) {
	j, err := json.Marshal(ch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session: failed to marshal capture hash: %v\n", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "session: failed to create hash directory: %v\n", err)
		return
	}
	if err := os.WriteFile(path, j, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "session: failed to write hash file %s: %v\n", path, err)
	}
}
```

Then replace all three hash-write blocks in `Health` with calls to
`writeHashFile(hashFile, ch)`.

---

## Task 3: Fix forge error wrapping

**File:** `internal/forge/forge.go` (lines 105-108)

Current code wraps a new `fmt.Errorf` instead of the original error:

```go
if out, err := cmd.CombinedOutput(); err != nil {
    return fmt.Errorf("failed to verify forge worktree for world %q: %w",
        r.world, fmt.Errorf("%s", strings.TrimSpace(string(out))))
}
```

This discards the original `err` (the exec.ExitError) and wraps a
synthetic error containing only stdout. The original exit code and error
type are lost.

**Fix:** Include both the git output and the original error:

```go
if out, err := cmd.CombinedOutput(); err != nil {
    return fmt.Errorf("failed to verify forge worktree for world %q: %s: %w",
        r.world, strings.TrimSpace(string(out)), err)
}
```

---

## Task 4: Make config writes atomic

**File:** `internal/config/world_config.go` — `WriteWorldConfig` (lines 106-125)

The current implementation uses `os.Create` which truncates the file
immediately. If the process crashes between truncate and the TOML encode,
`world.toml` is left empty or partial.

**Fix:** Write to a temp file in the same directory, then atomically rename:

```go
func WriteWorldConfig(world string, cfg WorldConfig) error {
	path := WorldConfigPath(world)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory %q: %w", dir, err)
	}

	// Write to temp file first for atomic rename.
	tmp, err := os.CreateTemp(dir, ".world.toml.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()

	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(cfg); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file for %s: %w", path, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file to %s: %w", path, err)
	}
	return nil
}
```

Remove the `defer f.Close()` / double-close pattern from the old
implementation.

---

## Task 5: Standardize flag access in store commands

**Files:** `cmd/store.go`, `cmd/store_dep.go`

These two files use `cmd.Flag("world").Value.String()` to read the
`--world` flag, while every other command in `cmd/` uses
`cmd.Flags().GetString("world")`.

**In `cmd/store.go`**, replace all 6 occurrences (lines 44, 86, 130, 199,
239, 275):

```go
// Before:
world := cmd.Flag("world").Value.String()

// After:
world, _ := cmd.Flags().GetString("world")
```

**In `cmd/store_dep.go`**, replace all 3 occurrences (lines 27, 56, 85):

Same pattern.

The `_` is safe here because these flags are always registered on the
command — `GetString` only errors if the flag doesn't exist or isn't a
string, neither of which can happen for a flag we just registered.

---

## Verification

- `make build && make test` passes
- `go vet ./...` clean
- Verify the tmuxCmd change didn't break any session integration tests
  (if integration tests touch tmux, run those too)

## Commit

```
fix(session,forge,config): arc 1 review-3 — tmux goroutine leak, atomic config, error handling
```
