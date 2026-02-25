# Prompt 04: Loop 1 — Integration Tests + Acceptance

You are writing the integration tests and performing final acceptance
verification for Loop 1 of the `gt` orchestration system. Loop 1 is
"multi-agent with supervision" — multiple agents can be dispatched
concurrently, a supervisor monitors and restarts crashed sessions, and
the operator can observe everything with `gt status`.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompts 01, 02, and 03 are complete.

Read all existing code first. Understand the full Loop 1 pipeline:
name pool (`internal/namepool/`), dispatch flock serialization
(`internal/dispatch/flock.go`), auto-provisioning in sling, supervisor
(`internal/supervisor/`), and status (`internal/status/`). Also review
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
    // Same as loop0: temp GT_HOME, temp git repo, isolated TMUX_TMPDIR
    // Cleanup: kill all gt-* tmux sessions

func openStores(t *testing.T, rig string) (*store.Store, *store.Store)

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

Two work items dispatched to the same rig, each auto-provisioned to a
different agent.

1. Create two work items:
   ```
   item1 := gt store create --db=testrig --title="Task Alpha"
   item2 := gt store create --db=testrig --title="Task Beta"
   ```
2. Sling both without specifying agents:
   ```
   gt sling <item1> testrig
   gt sling <item2> testrig
   ```
3. Verify:
   - Two different agents were auto-provisioned (different names)
   - Both agents are in "working" state
   - Both tmux sessions exist (different session names)
   - Each work item has a different assignee
   - Both hook files exist with their respective work item IDs
   - Both worktrees exist at different paths

### Test 2: Flock Serialization

Two concurrent sling attempts for the same work item — only one wins.

1. Create one work item
2. Create two idle agents manually:
   ```
   gt agent create Alpha --rig=testrig
   gt agent create Beta --rig=testrig
   ```
3. Acquire the work item lock manually using `dispatch.AcquireWorkItemLock`
4. Attempt `gt sling <item> testrig --agent=Alpha` in the main goroutine
5. Verify: sling returns a contention error
6. Release the lock
7. Sling again: should succeed now
8. Verify: work item assigned to Alpha, session running

Alternatively, if you can test with goroutines:
1. Create one work item and two idle agents
2. Launch two goroutines, each calling `dispatch.Sling()` for the same
   work item with a different agent
3. Verify: exactly one succeeds, the other gets a contention error
4. The winning agent has the work item hooked

### Test 3: Supervisor Session Restart

The supervisor detects a dead session and restarts it.

1. Create a work item and sling it (auto-provisions an agent)
2. Start the supervisor with a short heartbeat (use the supervisor
   package directly, not the CLI, for test control):
   ```go
   cfg := supervisor.DefaultConfig()
   cfg.HeartbeatInterval = 2 * time.Second  // fast for testing
   sup := supervisor.New(cfg, townStore, session.New(), logger)
   ctx, cancel := context.WithCancel(context.Background())
   go sup.Run(ctx)
   defer cancel()
   ```
3. Kill the agent's tmux session directly:
   ```go
   exec.Command("tmux", "kill-session", "-t", sessionName).Run()
   ```
4. Wait for the supervisor to restart it (poll for session existence,
   timeout 15 seconds):
   ```go
   pollUntil(15*time.Second, 500*time.Millisecond, func() bool {
       return mgr.Exists(sessionName)
   })
   ```
5. Verify:
   - Session exists again
   - Agent state is "working" (not "stalled")
   - Hook file still contains the same work item ID
   - The restarted session has the same name

### Test 4: Mass-Death Degradation

The supervisor enters degraded mode when too many sessions die at once.

1. Create and sling 5 work items (auto-provisions 5 agents)
2. Start the supervisor with short heartbeat and a mass-death window:
   ```go
   cfg.HeartbeatInterval = 1 * time.Second
   cfg.MassDeathThreshold = 3
   cfg.MassDeathWindow = 30 * time.Second
   cfg.DegradedCooldown = 10 * time.Second  // short for testing
   ```
3. Kill all 5 tmux sessions at once
4. Wait for the supervisor to detect deaths (poll `sup.IsDegraded()`,
   timeout 10 seconds)
5. Verify:
   - Supervisor is in degraded mode
   - No sessions were restarted (degraded = no respawns)
   - All agents are in "stalled" state
6. Wait for degraded cooldown to expire (poll `!sup.IsDegraded()`,
   timeout 15 seconds)
7. After recovery, kill one more session
8. Verify: supervisor respawns it (not degraded anymore)

### Test 5: GUPP Recovery

A restarted agent picks up its hooked work — the hook file and worktree
persist across session death and restart.

1. Create a work item, sling it
2. Verify: hook file exists, CLAUDE.md in worktree has work item context
3. Kill the tmux session
4. Verify: hook file still exists (durability)
5. Re-sling the same work item to the same agent (or let the supervisor
   restart it)
6. Verify:
   - New session is running
   - The session environment includes GT_AGENT and GT_RIG
   - `gt prime --rig=testrig --agent=<name>` returns the work item context
   - Hook file still contains the same work item ID

### Test 6: Status Accuracy

The status command reflects the actual state of the system.

1. Create 3 work items, sling all 3 (auto-provisions 3 agents)
2. Kill one agent's tmux session
3. Run `status.Gather()`:
   ```go
   result, err := status.Gather(rig, townStore, rigStore, session.New())
   ```
4. Verify:
   - `result.Summary.Total == 3`
   - `result.Summary.Working == 3` (agent state hasn't changed yet)
   - `result.Summary.Dead == 1` (session is dead)
   - `result.Health() == 1` (unhealthy)
   - The dead agent's `AgentStatus.SessionAlive == false`
   - Each agent's `WorkTitle` matches their work item title

5. Start the supervisor, let it restart the dead session
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
   Write it to `$GT_HOME/testrig/names.txt`
2. Create and sling 2 work items (exhausts the pool)
3. Create a third work item and attempt to sling it
4. Verify: sling returns an error containing "exhausted"
5. The third work item remains in "open" status, unassigned

---

## Task 3: CLI Smoke Tests

Add to `test/integration/cli_test.go` (or create a new file
`test/integration/cli_loop1_test.go`):

```go
func TestCLISupervisorRun(t *testing.T)
    // bin/gt supervisor run --help exits 0

func TestCLISupervisorStop(t *testing.T)
    // bin/gt supervisor stop --help exits 0

func TestCLIStatus(t *testing.T)
    // bin/gt status --help exits 0
    // bin/gt status testrig exits non-zero (no agents, but should not crash)

func TestCLIStatusJSON(t *testing.T)
    // Create an agent and work item, sling
    // bin/gt status testrig --json
    // Verify output is valid JSON
    // Verify JSON has expected fields: rig, supervisor, agents, summary

func TestCLISlingAutoProvision(t *testing.T)
    // No agent pre-created
    // bin/gt store create --db=testrig --title="test"
    // bin/gt sling <id> testrig (no --agent flag)
    // Verify exit 0
    // bin/gt agent list --rig=testrig -> contains an auto-provisioned agent name
```

Each test should use a unique `GT_HOME` temp directory.

---

## Task 4: Loop 1 Acceptance Checklist

Create `test/integration/LOOP1_ACCEPTANCE.md`:

```markdown
# Loop 1 Acceptance Criteria

## 1. Multi-agent dispatch
- [ ] `gt sling <item1> myrig && gt sling <item2> myrig` dispatches to two different polecats
- [ ] Each polecat has a unique name from the name pool
- [ ] Each polecat runs in its own worktree and tmux session

## 2. Dispatch serialization
- [ ] No two polecats get the same work item (flock prevents races)
- [ ] Concurrent `gt sling` for the same item: one wins, one fails with contention error

## 3. Name pool
- [ ] Auto-provisioned agents get names from the embedded pool (Toast, Jasper, ...)
- [ ] Custom `$GT_HOME/{rig}/names.txt` overrides the default pool
- [ ] Pool exhaustion returns a clear error

## 4. Supervisor — crash detection and restart
- [ ] `gt supervisor run` starts the supervisor (foreground, PID file written)
- [ ] Supervisor is town-level — monitors all rigs from one process
- [ ] Kill a polecat's tmux session → supervisor detects and restarts within heartbeat interval
- [ ] Restarted polecat picks up hooked work (GUPP principle)

## 5. Supervisor — backoff
- [ ] Repeated crashes of the same agent increase restart delay
- [ ] Backoff resets when agent completes work normally

## 6. Supervisor — mass-death protection
- [ ] Kill 3+ sessions in 30s → supervisor enters degraded mode (no respawns)
- [ ] Degraded mode auto-recovers after 5 minutes of quiet

## 7. Supervisor — lifecycle
- [ ] `gt supervisor stop` sends SIGTERM, supervisor stops all sessions gracefully
- [ ] Only one supervisor instance (PID file guard)
- [ ] Stale PID files from crashed supervisors are detected and overwritten

## 8. Status command
- [ ] `gt status myrig` shows agents, sessions, hooked work, supervisor state
- [ ] `gt status myrig --json` outputs valid JSON with all fields
- [ ] Exit code 0 = healthy, 1 = dead sessions, 2 = degraded/no supervisor

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
4. Verify that `gt sling` with a pre-created idle agent still prefers
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
- Use `t.Parallel()` only if tests use different GT_HOME and TMUX_TMPDIR.
  When testing the supervisor, sequential execution is safer.
- Prefer polling with timeout over fixed sleeps. Use the `pollUntil`
  helper.
- If you discover bugs in Loop 1 code while writing integration tests,
  fix them. That's the point of integration tests.
- Don't mock anything in integration tests — use real SQLite, real tmux,
  real git. Mock-based tests belong in unit test files.
- Keep the supervisor's heartbeat interval very short in tests (1-2
  seconds) to avoid slow tests. Use the package API directly instead of
  the CLI for fine control.
- If a test is flaky due to tmux timing, add a comment explaining why
  and use a generous timeout (15-30 seconds) with polling.
