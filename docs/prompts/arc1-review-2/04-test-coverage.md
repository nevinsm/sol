# Prompt 04: Test Coverage

You are filling test coverage gaps found during the second Arc 1 review
pass. No production code changes — only new tests and test fixes.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 review-2 prompt 03 is complete. All tests pass.

Read all files mentioned below before making changes.

---

## Task 1: Add `internal/config/config_test.go`

**File:** `internal/config/config_test.go` (new file)

**Gap:** `config.go` has zero unit tests. `Home()`, `StoreDir()`,
`RuntimeDir()`, `WorldDir()`, `EnsureDirs()`, and `ValidateWorldName()`
are entirely untested.

Create the test file with these test functions:

### `TestHomeFromEnv`
- Set `SOL_HOME` via `t.Setenv`, call `Home()`, verify it returns the
  env var value.

### `TestHomeDefault`
- Unset `SOL_HOME` via `t.Setenv("SOL_HOME", "")`, call `Home()`,
  verify it returns a path under the user's home directory (should end
  with `/sol`).

### `TestStoreDir`
- Set `SOL_HOME`, call `StoreDir()`, verify it returns
  `$SOL_HOME/.store`.

### `TestRuntimeDir`
- Set `SOL_HOME`, call `RuntimeDir()`, verify it returns
  `$SOL_HOME/.runtime`.

### `TestWorldDir`
- Set `SOL_HOME`, call `WorldDir("myworld")`, verify it returns
  `$SOL_HOME/myworld`.

### `TestEnsureDirs`
- Set `SOL_HOME` to a temp dir, call `EnsureDirs()`, verify that
  `.store` and `.runtime` subdirectories were created.

### `TestEnsureDirsAlreadyExist`
- Set `SOL_HOME` to a temp dir, create the subdirs manually, call
  `EnsureDirs()`, verify no error (idempotent).

### `TestValidateWorldNameEmpty`
- Call `ValidateWorldName("")`, verify error contains "must not be empty".

### `TestValidateWorldNameInvalid`
- Table-driven test with invalid names: `.hidden`, `has spaces`,
  `-starts-dash`, `foo/bar`, `foo.bar`.
- Each should return an error containing "invalid world name".

### `TestValidateWorldNameReserved`
- Table-driven test with reserved names: `store`, `runtime`, `sol`,
  `formulas`.
- Each should return an error containing "reserved".
- Also test that names containing reserved words pass: `mystore`,
  `store1`, `formulas2`.

### `TestValidateWorldNameTooLong`
- Create a name longer than 64 characters, verify error contains
  "too long".

### `TestValidateWorldNameValid`
- Table-driven test with valid names: `myworld`, `test-world`,
  `World_01`, `a1`.

---

## Task 2: Add `DeleteWorldData` unit tests

**File:** `internal/store/worlds_test.go`

**Gap:** `DeleteWorldData` has zero unit tests. It is a transactional
cascade delete affecting up to 5 tables and is critical for data
integrity.

### `TestDeleteWorldDataDeletesAgents`
1. `setupSphere`, register a world, create an agent for that world.
2. Call `DeleteWorldData`.
3. Verify `ListAgents` returns empty for that world.
4. Verify `GetWorld` returns nil for that world.

### `TestDeleteWorldDataDeletesCaravanItems`
1. `setupSphere`, register a world, create a caravan, add items for
   that world.
2. Call `DeleteWorldData`.
3. Verify caravan still exists (it is cross-world) but the items for
   the deleted world are gone.

### `TestDeleteWorldDataDeletesMessagesAndEscalations`
1. `setupSphere`, register a world, create an agent for the world.
2. Send a message from that agent. Create an escalation sourced from
   that agent.
3. Call `DeleteWorldData`.
4. Verify the message and escalation are gone.

### `TestDeleteWorldDataNonexistentWorld`
1. `setupSphere`, call `DeleteWorldData("nonexistent")`.
2. Verify no error (the DELETE WHERE clauses just affect 0 rows).

### `TestDeleteWorldDataPreservesOtherWorlds`
1. `setupSphere`, register two worlds with agents in each.
2. Delete one world.
3. Verify the other world's agents, messages, etc. are untouched.

---

## Task 3: Add `WriteWorldConfig` error path tests

**File:** `internal/config/world_config_test.go`

**Gap:** Only the happy-path round-trip is tested.

### `TestWriteWorldConfigReadOnlyDir`
1. Create a temp dir, make it read-only (`os.Chmod(dir, 0o555)`).
2. Call `WriteWorldConfig` with a path under that dir.
3. Verify error is returned.
4. Cleanup: restore permissions so `t.TempDir()` can clean up.

### `TestWriteWorldConfigCreatesParentDirs`
1. Call `WriteWorldConfig` with a path under a non-existent subdirectory.
2. Verify the file is written and parent dirs were created.

---

## Task 4: Migrate `os.Setenv` to `t.Setenv` in store tests

**Files:**
- `internal/store/store_test.go`
- `internal/store/caravans_test.go`
- `internal/status/status_test.go`

**Gap:** These files use `os.Setenv("SOL_HOME", dir)` with manual
`t.Cleanup(func() { os.Unsetenv("SOL_HOME") })`. This is not
goroutine-safe and prevents `t.Parallel()`.

**Fix:** Replace every instance of:

```go
os.Setenv("SOL_HOME", dir)
t.Cleanup(func() { os.Unsetenv("SOL_HOME") })
```

With:

```go
t.Setenv("SOL_HOME", dir)
```

`t.Setenv` automatically restores the original value on cleanup and
panics if called from a parallel test (providing a safety net).

Search each file for `os.Setenv` and `os.Unsetenv` and replace all
occurrences. Make sure to remove the `t.Cleanup` wrappers that manually
unset the env var, since `t.Setenv` handles this automatically.

Remove the `"os"` import if it becomes unused after these changes (check
that no other code in the file uses `os`).

---

## Task 5: Fix integration tests that ignore setup errors

**File:** `test/integration/world_lifecycle_test.go`

**Gap:** Some tests discard the error return from `runGT` during setup
commands. If setup fails silently, subsequent assertions pass vacuously.

Find all instances where `runGT` is called during setup without checking
the error. For example:

```go
runGT(t, gtHome, "world", "init", "testworld")  // error ignored
```

Fix by checking the error:

```go
if _, err := runGT(t, gtHome, "world", "init", "testworld"); err != nil {
    t.Fatalf("setup: world init failed: %v", err)
}
```

Apply this fix to every `runGT` call that is part of test setup (not
the call being tested). Look for patterns where the return value is
assigned to `_` or not captured at all.

---

## Task 6: Add `world delete` with active sessions test

**File:** `test/integration/world_lifecycle_test.go`

**Gap:** The safety check that refuses deletion when tmux sessions are
running (cmd/world.go, around line 311-329) has no integration test.

### `TestWorldDeleteRefusesWithActiveSessions`

```go
func TestWorldDeleteRefusesWithActiveSessions(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    gtHome := t.TempDir()
    os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

    // Create a world.
    if _, err := runGT(t, gtHome, "world", "init", "deltest"); err != nil {
        t.Fatalf("setup: %v", err)
    }

    // Start a tmux session with the world's naming convention.
    // Session name format: sol-{world}-{agent}
    sessionName := "sol-deltest-TestAgent"
    exec.Command("tmux", "new-session", "-d", "-s", sessionName, "sleep", "60").Run()
    t.Cleanup(func() {
        exec.Command("tmux", "kill-session", "-t", sessionName).Run()
    })

    // Attempt to delete — should be refused.
    out, err := runGT(t, gtHome, "world", "delete", "deltest", "--confirm")
    if err == nil {
        t.Fatalf("expected error with active session, got success: %s", out)
    }
    if !strings.Contains(out, "active session") {
        t.Fatalf("expected 'active session' error, got: %s", out)
    }
}
```

**Note:** This test requires tmux to be available. If the test
environment doesn't have tmux, skip with an appropriate message. Check
how existing session tests handle this — look at
`internal/session/manager_test.go` for the pattern.

---

## Task 7: Verify

1. `make build` — compiles cleanly (no production code changed)
2. `make test` — all tests pass, including the new ones
3. Run new tests individually to verify they exercise the right paths:
   ```bash
   go test ./internal/config/ -run TestHome -v
   go test ./internal/config/ -run TestValidateWorldName -v
   go test ./internal/config/ -run TestEnsureDirs -v
   go test ./internal/config/ -run TestWriteWorldConfig -v
   go test ./internal/store/ -run TestDeleteWorldData -v
   go test ./test/integration/ -run TestWorldDeleteRefuses -v
   ```

---

## Guidelines

- This prompt is test-only. Do not modify production code.
- Use `t.Setenv` instead of `os.Setenv` in all new tests.
- Use `t.TempDir()` for all temporary directories (auto-cleanup).
- Each test should be self-contained — no dependency on other tests.
- Follow existing test conventions in each file (helper functions,
  naming patterns, assertion styles).
- All existing tests must continue to pass.
- Commit with message:
  `test: arc 1 review-2 — config tests, DeleteWorldData tests, coverage gaps`
