# Prompt 04: Loop 1 — Integration Tests + Acceptance

You are writing the integration tests and performing final acceptance
verification for Loop 1 of the `sol` orchestration system. Loop 1 is
"multi-agent with supervision" — multiple agents can be dispatched
concurrently, a prefect monitors and restarts crashed sessions, and
the operator can observe everything with `sol status`.

**Working directory:** `~/sol-src/`
**Prerequisite:** Prompts 01, 02, and 03 are complete.

Read all existing code first. Understand the full Loop 1 pipeline:
name pool (`internal/namepool/`), dispatch flock serialization
(`internal/dispatch/flock.go`), auto-provisioning in cast, prefect
(`internal/prefect/`), and status (`internal/status/`). Also review
the Loop 0 integration tests (`test/integration/loop0_test.go`) and CLI
smoke tests (`test/integration/cli_test.go`) for patterns.

---

## Task 1: Fix Any Broken Tests

Run `make test`. If any tests fail, fix them before proceeding. The
previous prompts may have left inconsistencies. Get to green first.

---

## Task 2: Integration Test Suite

Create `test/integration/loop1_test.go`. These tests exercise the full
Loop 1 pipeline end-to-end with real SQLite, real tmux, and real git.

**Test environment setup:**

Reuse or extend the `setupTestEnv` helper from `loop0_test.go`. If it's
not already exported or in a shared file, extract it to a
`test/integration/helpers_test.go` file that both loop0 and loop1 tests
can use.

```go
func setupTestEnv(t *testing.T) (gtHome string, sourceRepo string)
    // Same as loop0: temp SOL_HOME, temp git repo, isolated TMUX_TMPDIR
    // Cleanup: kill all sol-* tmux sessions

func openStores(t *testing.T, world string) (*store.Store, *store.Store)

func pollUntil(timeout, interval time.Duration, fn func() bool) bool
```

All integration tests should guard with:
```go
if testing.Short() {
    t.Skip("skipping integration test")
}
```

**Test cases:**

### Test 1: Multi-Agent Dispatch

Two work items dispatched to the same world, each auto-provisioned to a
different agent.

1. Create two work items:
   ```
   item1 := sol store create --world=testrig --title="Task Alpha"
   item2 := sol store create --world=testrig --title="Task Beta"
   ```
2. Cast both without specifying agents:
   ```
   sol cast <item1> testrig
   sol cast <item2> testrig
   ```
3. Verify:
   - Two different agents were auto-provisioned (different names)
   - Both agents are in "working" state
   - Both tmux sessions exist (different session names)
   - Each work item has a different assignee
   - Both tether files exist with their respective work item IDs
   - Both worktrees exist at different paths

### Test 2: Flock Serialization

Two concurrent cast attempts for the same work item — only one wins.

1. Create one work item
2. Create two idle agents manually:
   ```
   sol agent create Alpha --world=testrig
   sol agent create Beta --world=testrig
   ```
3. Acquire the work item lock manually using `dispatch.AcquireWorkItemLock`
4. Attempt `sol cast <item> testrig --agent=Alpha` in the main goroutine
5. Verify: cast returns a contention error
6. Release the lock
7. Cast again: should succeed now
8. Verify: work item assigned to Alpha, session running

Alternatively, if you can test with goroutines:
1. Create one work item and two idle agents
2. Launch two goroutines, each calling `dispatch.Cast()` for the same
   work item with a different agent
3. Verify: exactly one succeeds, the other gets a contention error
4. The winning agent has the work item tethered

### Test 3: Prefect Session Restart

The prefect detects a dead session and restarts it.

1. Create a work item and cast it (auto-provisions an agent)
2. Start the prefect with a short heartbeat (use the prefect
   package directly, not the CLI, for test control):
   ```go
   cfg := prefect.DefaultConfig()
   cfg.HeartbeatInterval = 2 * time.Second  // fast for testing
   sup := prefect.New(cfg, sphereStore, session.New(), logger)
   ctx, cancel := context.WithCancel(context.Background())
   go sup.Run(ctx)
   defer cancel()
   ```
3. Kill the agent's tmux session directly:
   ```go
   exec.Command("tmux", "kill-session", "-t", sessionName).Run()
   ```
4. Wait for the prefect to restart it (poll for session existence,
   timeout 15 seconds):
   ```go
   pollUntil(15*time.Second, 500*time.Millisecond, func() bool {
       return mgr.Exists(sessionName)
   })
   ```
5. Verify:
   - Session exists again
   - Agent state is "working" (not "stalled")
   - Tether file still contains the same work item ID
   - The restarted session has the same name

### Test 4: Mass-Death Degradation

The prefect enters degraded mode when too many sessions die at once.

1. Create and cast 5 work items (auto-provisions 5 agents)
2. Start the prefect with short heartbeat and a mass-death window:
   ```go
   cfg.HeartbeatInterval = 1 * time.Second
   cfg.MassDeathThreshold = 3
   cfg.MassDeathWindow = 30 * time.Second
   cfg.DegradedCooldown = 10 * time.Second  // short for testing
   ```
3. Kill all 5 tmux sessions at once
4. Wait for the prefect to detect deaths (poll `sup.IsDegraded()`,
   timeout 10 seconds)
5. Verify:
   - Prefect is in degraded mode
   - No sessions were restarted (degraded = no respawns)
   - All agents are in "stalled" state
6. Wait for degraded cooldown to expire (poll `!sup.IsDegraded()`,
   timeout 15 seconds)
7. After recovery, kill one more session
8. Verify: prefect respawns it (not degraded anymore)

### Test 5: GUPP Recovery

A restarted agent picks up its tethered work — the tether file and worktree
persist across session death and restart.

1. Create a work item, cast it
2. Verify: tether file exists, CLAUDE.md in worktree has work item context
3. Kill the tmux session
4. Verify: tether file still exists (durability)
5. Re-cast the same work item to the same agent (or let the prefect
   restart it)
6. Verify:
   - New session is running
   - The session environment includes SOL_AGENT and SOL_WORLD
   - `sol prime --world=testrig --agent=<name>` returns the work item context
   - Tether file still contains the same work item ID

### Test 6: Status Accuracy

The status command reflects the actual state of the system.

1. Create 3 work items, cast all 3 (auto-provisions 3 agents)
2. Kill one agent's tmux session
3. Run `status.Gather()`:
   ```go
   result, err := status.Gather(world, sphereStore, worldStore, session.New())
   ```
4. Verify:
   - `result.Summary.Total == 3`
   - `result.Summary.Working == 3` (agent state hasn't changed yet)
   - `result.Summary.Dead == 1` (session is dead)
   - `result.Health() == 1` (unhealthy)
   - The dead agent's `AgentStatus.SessionAlive == false`
   - Each agent's `WorkTitle` matches their work item title

5. Start the prefect, let it restart the dead session
6. Run `status.Gather()` again:
   - `result.Summary.Dead == 0`
   - `result.Health() == 0` (healthy)

### Test 7: Name Pool Exhaustion

The system handles name pool exhaustion gracefully.

1. Create a custom names file with only 2 names:
   ```
   Alpha
   Beta
   ```
   Write it to `$SOL_HOME/testrig/names.txt`
2. Create and cast 2 work items (exhausts the pool)
3. Create a third work item and attempt to cast it
4. Verify: cast returns an error containing "exhausted"
5. The third work item remains in "open" status, unassigned

---

## Task 3: CLI Smoke Tests

Add to `test/integration/cli_test.go` (or create a new file
`test/integration/cli_loop1_test.go`):

```go
func TestCLISupervisorRun(t *testing.T)
    // bin/sol prefect run --help exits 0

func TestCLISupervisorStop(t *testing.T)
    // bin/sol prefect stop --help exits 0

func TestCLIStatus(t *testing.T)
    // bin/sol status --help exits 0
    // bin/sol status testrig exits non-zero (no agents, but should not crash)

func TestCLIStatusJSON(t *testing.T)
    // Create an agent and work item, cast
    // bin/sol status testrig --json
    // Verify output is valid JSON
    // Verify JSON has expected fields: world, prefect, agents, summary

func TestCLISlingAutoProvision(t *testing.T)
    // No agent pre-created
    // bin/sol store create --world=testrig --title="test"
    // bin/sol cast <id> testrig (no --agent flag)
    // Verify exit 0
    // bin/sol agent list --world=testrig -> contains an auto-provisioned agent name
```

Each test should use a unique `SOL_HOME` temp directory.

---

## Task 4: Loop 1 Acceptance Checklist

Create `test/integration/LOOP1_ACCEPTANCE.md`:

```markdown
# Loop 1 Acceptance Criteria

## 1. Multi-agent dispatch
- [ ] `sol cast <item1> myworld && sol cast <item2> myworld` dispatches to two different outposts
- [ ] Each outpost has a unique name from the name pool
- [ ] Each outpost runs in its own worktree and tmux session

## 2. Dispatch serialization
- [ ] No two outposts get the same work item (flock prevents races)
- [ ] Concurrent `sol cast` for the same item: one wins, one fails with contention error

## 3. Name pool
- [ ] Auto-provisioned agents get names from the embedded pool (Toast, Jasper, ...)
- [ ] Custom `$SOL_HOME/{world}/names.txt` overrides the default pool
- [ ] Pool exhaustion returns a clear error

## 4. Prefect — crash detection and restart
- [ ] `sol prefect run` starts the prefect (foreground, PID file written)
- [ ] Prefect is sphere-level — monitors all worlds from one process
- [ ] Kill a outpost's tmux session → prefect detects and restarts within heartbeat interval
- [ ] Restarted outpost picks up tethered work (GUPP principle)

## 5. Prefect — backoff
- [ ] Repeated crashes of the same agent increase restart delay
- [ ] Backoff resets when agent completes work normally

## 6. Prefect — mass-death protection
- [ ] Kill 3+ sessions in 30s → prefect enters degraded mode (no respawns)
- [ ] Degraded mode auto-recovers after 5 minutes of quiet

## 7. Prefect — lifecycle
- [ ] `sol prefect stop` sends SIGTERM, prefect stops all sessions gracefully
- [ ] Only one prefect instance (PID file guard)
- [ ] Stale PID files from crashed prefects are detected and overwritten

## 8. Status command
- [ ] `sol status myworld` shows agents, sessions, tethered work, prefect state
- [ ] `sol status myworld --json` outputs valid JSON with all fields
- [ ] Exit code 0 = healthy, 1 = dead sessions, 2 = degraded/no prefect

## 9. All tests pass
- [ ] `make test` exits 0
- [ ] Loop 0 integration tests still pass
- [ ] Loop 1 integration tests pass
- [ ] CLI smoke tests pass (new commands)
```

---

## Task 5: Verify Backward Compatibility

Ensure all Loop 0 functionality still works:

1. Run the Loop 0 integration tests:
   ```
   go test ./test/integration/ -run TestLoop0 -v -count=1
   ```
   (Or whatever naming convention the tests use)
2. Run all unit tests: `make test`
3. Verify that single-agent dispatch (with `--agent` flag) still works
   the same as before
4. Verify that `sol cast` with a pre-created idle agent still prefers
   that agent over auto-provisioning

---

## Task 6: Final Verification

1. `make test` — all pass
2. `make build` — succeeds
3. Run the full integration test suite:
   ```
   go test ./test/integration/ -v -count=1
   ```
4. Commit everything with a clear message:
   `test: add Loop 1 integration tests, CLI smoke tests, and acceptance checklist`

---

## Guidelines

- Integration tests are slow. Guard with `t.Short()`.
- Use `t.Parallel()` only if tests use different SOL_HOME and TMUX_TMPDIR.
  When testing the prefect, sequential execution is safer.
- Prefer polling with timeout over fixed sleeps. Use the `pollUntil`
  helper.
- If you discover bugs in Loop 1 code while writing integration tests,
  fix them. That's the point of integration tests.
- Don't mock anything in integration tests — use real SQLite, real tmux,
  real git. Mock-based tests belong in unit test files.
- Keep the prefect's heartbeat interval very short in tests (1-2
  seconds) to avoid slow tests. Use the package API directly instead of
  the CLI for fine control.
- If a test is flaky due to tmux timing, add a comment explaining why
  and use a generous timeout (15-30 seconds) with polling.
