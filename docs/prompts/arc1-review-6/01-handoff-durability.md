# Prompt 01: Arc 1 Review-6 — Handoff Hardening and Config Durability

You are fixing durability gaps in the handoff and config packages, and a logic bug in the handoff retry path.

**Working directory:** `~/gt-src/`
**Prerequisite:** `make build && make test` passes on current main.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/tether/tether.go` — `Write()` function as the reference pattern for fsync-before-rename
- `internal/handoff/handoff.go` — `Write()`, `Exec()`, `GitLog()`, `Capture()` functions
- `internal/handoff/handoff_test.go` — existing tests
- `internal/config/world_config.go` — `WriteWorldConfig()` function
- `internal/config/world_config_test.go` — existing config tests

---

## Task 1: Add fsync to `handoff.Write()`

**File:** `internal/handoff/handoff.go`, `Write` function (around line 155)

The current implementation uses `os.WriteFile()` to a temp file then renames. This is missing an explicit `fsync` between write and rename — a power failure could leave the handoff file empty or corrupt. `tether.Write()` already has this fixed correctly.

**Fix:** Replace `os.WriteFile(tmp, data, 0o644)` with the explicit open/write/sync/close/rename pattern matching `tether.Write()`:

```go
func Write(state *State) error {
	path := HandoffPath(state.World, state.AgentName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create handoff directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal handoff state: %w", err)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("failed to write handoff file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to write handoff file: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to sync handoff file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to close handoff file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to commit handoff file: %w", err)
	}
	return nil
}
```

---

## Task 2: Add fsync to `WriteWorldConfig()`

**File:** `internal/config/world_config.go`, `WriteWorldConfig` function (around line 116)

Same durability gap. Currently writes via TOML encoder to temp file, closes, renames — but no `fsync` before close. Add `tmp.Sync()` after the successful encode and before `tmp.Close()`:

```go
	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(cfg); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file for %s: %w", path, err)
	}
```

---

## Task 3: Fix handoff `Exec()` retry logic

**File:** `internal/handoff/handoff.go`, `Exec` function (around line 285)

There's a logic bug in the retry path. When `Start()` fails at line ~296, the code retries with another `Start()` at line ~299. But if the original `Stop()` at line ~286 failed (error is ignored) and the session still exists, both `Start()` calls fail with "session already exists" — the retry can never work in the most common failure case.

**Fix:** Force-stop the session before retrying:

```go
	// 5. Start a new session with the same worktree.
	env := map[string]string{
		"SOL_HOME":  config.Home(),
		"SOL_WORLD": opts.World,
		"SOL_AGENT": opts.AgentName,
	}
	if err := sessionMgr.Start(sessionName, worktreeDir, "claude --dangerously-skip-permissions", env, "agent", opts.World); err != nil {
		// Start failed — force-kill any remnant session, then retry once.
		fmt.Fprintf(os.Stderr, "handoff: new session failed, attempting recovery: %v\n", err)
		_ = sessionMgr.Stop(sessionName, true)
		if restartErr := sessionMgr.Start(sessionName, worktreeDir, "claude --dangerously-skip-permissions", env, "agent", opts.World); restartErr != nil {
			fmt.Fprintf(os.Stderr, "handoff: recovery also failed: %v\n", restartErr)
			return fmt.Errorf("failed to start new session (recovery also failed): %w", err)
		}
		fmt.Fprintf(os.Stderr, "handoff: recovery succeeded\n")
	}
```

The key change: insert `_ = sessionMgr.Stop(sessionName, true)` before the retry `Start()`.

---

## Task 4: Log errors in `GitLog()` instead of swallowing

**File:** `internal/handoff/handoff.go`, `GitLog` function (around line 206)

Currently returns `([]string{}, nil)` on any git error. A corrupted repo looks identical to "no commits." Add stderr logging consistent with the DEGRADE pattern used elsewhere in handoff:

```go
func GitLog(worktreeDir string, count int) ([]string, error) {
	cmd := exec.Command("git", "-C", worktreeDir, "log", "--oneline", fmt.Sprintf("-%d", count))
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "handoff: git log failed in %s: %v\n", worktreeDir, err)
		return []string{}, nil
	}
```

Still returns empty slice + nil error (graceful degradation), but now the failure is observable in logs.

---

## Task 5: Tests

No new test functions are required for these changes — they're durability and logging improvements. Verify:

- Existing `handoff_test.go` tests still pass
- Existing `world_config_test.go` tests still pass (including `TestWriteWorldConfigReadOnlyDir`)
- If there is an existing test for `Exec`, verify it still passes with the retry logic change

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Manually inspect that `handoff.Write()` now mirrors `tether.Write()` in its fsync pattern
- Manually inspect that `WriteWorldConfig()` calls `tmp.Sync()` before `tmp.Close()`

## Commit

```
fix(handoff,config): arc 1 review-6 — fsync durability, exec retry, git log observability
```
