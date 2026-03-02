# Prompt 03: Arc 1 Review-5 — Durability and Cleanup

You are fixing durability gaps and cleanup bugs found during the fifth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 02 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/tether/tether.go` — tether read/write/clear
- `internal/tether/tether_test.go` — existing tether tests
- `internal/session/manager.go` — Stop and List functions
- `internal/session/manager_test.go` — existing session tests
- `internal/handoff/handoff.go` — Exec function (stop/start sequence)
- `internal/handoff/handoff_test.go` — existing handoff tests
- `cmd/world.go` — worldInitCmd
- `internal/dispatch/dispatch.go` — SessionName and WorktreePath helpers

---

## Task 1: Add fsync to tether Write

**File:** `internal/tether/tether.go`, `Write` function (lines 32-47)

The tether is described as "the durability primitive" for crash recovery. Currently `Write` uses `os.WriteFile` to a temp file then renames. But without an explicit `fsync`, a power failure between write and rename could result in an empty or corrupt tether file.

**Fix:** Replace `os.WriteFile` with explicit file creation, write, sync, close, rename:

```go
func Write(world, agentName, workItemID string) error {
	path := TetherPath(world, agentName)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create tether directory for agent %q in world %q: %w", agentName, world, err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("failed to write tether for agent %q in world %q: %w", agentName, world, err)
	}
	if _, err := f.WriteString(workItemID); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to write tether for agent %q in world %q: %w", agentName, world, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("failed to sync tether for agent %q in world %q: %w", agentName, world, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to close tether for agent %q in world %q: %w", agentName, world, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("failed to commit tether for agent %q in world %q: %w", agentName, world, err)
	}
	return nil
}
```

---

## Task 2: Fix session Stop metadata cleanup

**File:** `internal/session/manager.go`, `Stop` function (around line 189)

If `kill-session` fails at line 213, the function returns immediately without removing the metadata file. This leaves orphaned metadata that appears as a ghost session in `List()`.

**Fix:** Move metadata cleanup before the early return, or restructure so cleanup always runs. The cleanest fix is to always clean up metadata regardless of kill result, since a failed kill likely means the session is already dead:

```go
func (m *Manager) Stop(name string, force bool) error {
	if !m.Exists(name) {
		// Session doesn't exist in tmux, but clean up any stale metadata.
		_ = os.Remove(metadataPath(name))
		_ = os.Remove(captureHashPath(name))
		return fmt.Errorf("session %q not found", name)
	}

	if !force {
		// Send C-c for graceful shutdown.
		interrupt, interruptCancel := tmuxCmd("send-keys", "-t", tmuxExactTarget(name), "C-c")
		_ = interrupt.Run()
		interruptCancel()
		// Wait up to 5 seconds for the session to exit.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !m.Exists(name) {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Kill the session if it still exists.
	var killErr error
	if m.Exists(name) {
		kill, killCancel := tmuxCmd("kill-session", "-t", tmuxExactTarget(name))
		defer killCancel()
		if out, err := kill.CombinedOutput(); err != nil {
			killErr = fmt.Errorf("failed to kill session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
		}
	}

	// Always remove metadata — even if kill failed (session may already be dead).
	_ = os.Remove(metadataPath(name))
	_ = os.Remove(captureHashPath(name))

	return killErr
}
```

---

## Task 3: Log warnings for unreadable session metadata in List

**File:** `internal/session/manager.go`, `List` function (around line 228)

Lines 244-249 silently skip unreadable or corrupt metadata files. For a system with 10-30+ agents, silently losing a session from the listing is dangerous.

**Fix:** Accept an optional `*slog.Logger` on the `Manager` struct, or log to stderr (consistent with how `writeHashFile` handles errors). The simplest approach — log to stderr:

```go
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "session: skipping unreadable metadata %s: %v\n", entry.Name(), err)
			continue
		}

		var meta sessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			fmt.Fprintf(os.Stderr, "session: skipping corrupt metadata %s: %v\n", entry.Name(), err)
			continue
		}
```

---

## Task 4: Fix handoff Exec — rollback on Start failure

**File:** `internal/handoff/handoff.go`, `Exec` function (around line 282)

If `Stop` succeeds but `Start` fails, the agent is left in a broken state: session dead, handoff file on disk, tether still active. There's no recovery.

**Fix:** If `Start` fails after a successful `Stop`, attempt to restart the old session. Also use `dispatch.SessionName` and `dispatch.WorktreePath` instead of manual construction:

First, add the import for the dispatch package if not already present.

```go
	sessionName := dispatch.SessionName(opts.World, opts.AgentName)
	worktreeDir := dispatch.WorktreePath(opts.World, opts.AgentName)

	// 4. Stop the current session (graceful).
	if err := sessionMgr.Stop(sessionName, false); err != nil {
		fmt.Fprintf(os.Stderr, "handoff: failed to stop session %s: %v\n", sessionName, err)
	}

	// 5. Start a new session with the same worktree.
	env := map[string]string{
		"SOL_HOME":  config.Home(),
		"SOL_WORLD": opts.World,
		"SOL_AGENT": opts.AgentName,
	}
	if err := sessionMgr.Start(sessionName, worktreeDir, "claude --dangerously-skip-permissions", env, "agent", opts.World); err != nil {
		// Attempt to restart the old session so the agent isn't left dead.
		fmt.Fprintf(os.Stderr, "handoff: new session failed, attempting to restart previous session: %v\n", err)
		if restartErr := sessionMgr.Start(sessionName, worktreeDir, "claude --dangerously-skip-permissions", env, "agent", opts.World); restartErr != nil {
			fmt.Fprintf(os.Stderr, "handoff: restart also failed: %v\n", restartErr)
		}
		return fmt.Errorf("failed to start new session: %w", err)
	}
```

Also fix the other manual session name construction in `Capture` (around line 86) to use `dispatch.SessionName`:

```go
	sessionName := dispatch.SessionName(opts.World, opts.AgentName)
```

And the manual worktree path in `Capture` (around line 98):

```go
	worktreeDir := dispatch.WorktreePath(opts.World, opts.AgentName)
```

---

## Task 5: Add deferred store close in worldInitCmd

**File:** `cmd/world.go`, `worldInitCmd` RunE (around line 81)

The world store and sphere store are closed manually with `worldStore.Close()` and `sphereStore.Close()`. If code between open and close panics or if new code is inserted, the close can be skipped.

**Fix:** Use defer for both stores:

```go
		// Create world database (triggers schema migration).
		worldStore, err := store.OpenWorld(name)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		// Register in sphere.db.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		if err := sphereStore.RegisterWorld(name, sourceRepo); err != nil {
			return err
		}
```

Remove the manual `worldStore.Close()` and `sphereStore.Close()` calls.

---

## Task 6: Tests

**File:** `internal/session/manager_test.go`

```go
func TestStopCleansMetadataOnKillFailure(t *testing.T)
```
- Start a session, then manually kill it via raw tmux command (bypassing the manager)
- Call `mgr.Stop(name, true)` — the kill-session will fail (session already dead)
- Verify: metadata file no longer exists
- Verify: `List()` does not include the session

**File:** `internal/handoff/handoff_test.go`

Verify that the `dispatch.SessionName` import works and the manual `fmt.Sprintf` construction has been replaced. If there's an existing test for `Exec`, update it. If not, verify the import compiles cleanly.

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Verify no circular import from handoff -> dispatch. Check that `internal/handoff` does not already depend on anything that depends on `internal/dispatch`. If it would create a circular import, instead extract `SessionName` and `WorktreePath` to a shared package (e.g., `internal/config` or a new `internal/naming` package). Use the simplest approach that avoids the cycle.

## Commit

```
fix(tether,session,handoff,cmd): arc 1 review-5 — fsync durability, metadata cleanup, handoff recovery
```
