# Prompt 04: Loop 0 — Integration Tests + Acceptance

You are writing the integration tests and performing final acceptance
verification for Loop 0 of the `sol` orchestration system. Loop 0 is
"single agent dispatch" — an operator dispatches work to one AI agent,
the agent executes it, and the result is verifiable.

**Working directory:** `~/sol-src/`
**Prerequisite:** Prompts 01, 02, and 03 are complete.

Read all existing code first. Understand the full pipeline: store, session
manager, tether, dispatch (cast/prime/done), agent protocol (CLAUDE.md,
tethers).

---

## Task 1: Fix Any Broken Tests

Run `make test`. If any tests fail, fix them before proceeding. The
previous prompts may have left inconsistencies. Get to green first.

---

## Task 2: Integration Test Suite

Create `test/integration/loop0_test.go`. These tests exercise the full
pipeline end-to-end with real SQLite, real tmux, and real git.

**Test environment setup:**

```go
func setupTestEnv(t *testing.T) (gtHome string, sourceRepo string) {
    // 1. Create temp dir for SOL_HOME
    // 2. Set SOL_HOME env var
    // 3. Create a temp git repo (git init, add a file, commit)
    //    to serve as the "source repo" for worktrees
    // 4. Set TMUX_TMPDIR to a test-specific dir for isolated tmux server
    // 5. t.Cleanup: kill all sol-* tmux sessions, remove temp dirs
    return gtHome, sourceRepo
}
```

**Test cases:**

### Test 1: Full Dispatch-Execute-Done Cycle

The happy path:

1. Create an agent: `sol agent create TestBot --world=testrig`
2. Create a work item: `sol store create --world=testrig --title="Test task"`
3. Cast: `sol cast <item-id> testrig --agent=TestBot`
4. Verify:
   - tmux session `sol-testrig-TestBot` exists
   - Work item status is `tethered`, assignee is `testrig/TestBot`
   - Agent state is `working`, hook_item is the work item ID
   - Tether file exists at `$SOL_HOME/testrig/outposts/TestBot/.tether`
   - Worktree exists at `$SOL_HOME/testrig/outposts/TestBot/world/`
   - `.claude/CLAUDE.md` exists in the worktree with correct content
5. Simulate agent work: inject a command into the session that creates
   a file and runs `sol resolve`:
   ```
   touch README.md && git add README.md && git commit -m "test" && sol resolve
   ```
   (Or: directly call the done logic programmatically if the tmux
   injection is flaky. The important thing is testing the done sequence.)
6. Wait for done to complete (poll work item status, max 30s)
7. Verify:
   - Work item status is `resolve`
   - Agent state is `idle`, hook_item is empty
   - Tether file does not exist
   - tmux session is gone
   - Branch `outpost/TestBot/<item-id>` exists in the source repo

### Test 2: Crash Recovery (Re-cast)

1. Create agent + work item, cast
2. Kill the tmux session directly: `tmux kill-session -t sol-testrig-TestBot`
3. Verify: work item is still `tethered`, tether file still exists (durability)
4. Re-cast the same work item to the same agent:
   `sol cast <item-id> testrig --agent=TestBot`
5. Verify: new tmux session exists, agent picks up the same work
6. The tether file should still contain the same work item ID

### Test 3: Double-Dispatch Prevention

1. Create agent + work item, cast
2. Create a second work item
3. Try to cast the second item to the same agent (already working)
4. Verify: error returned, second item remains `open`

### Test 4: Prime Output

1. Create agent, work item, cast
2. Run `sol prime --world=testrig --agent=TestBot`
3. Capture stdout, verify it contains:
   - The work item ID
   - The title
   - The description
   - "sol resolve" instructions

### Test 5: Prime Without Tether

1. Create an idle agent (no cast)
2. Run `sol prime --world=testrig --agent=TestBot`
3. Verify output says "No work tethered"

### Test 6: Store Inspection

Verify the operator can directly query the SQLite database:

1. Create work items, cast one
2. Open the world DB directly with `database/sql` (or shell out to sqlite3)
3. Run: `SELECT id, title, status, assignee FROM work_items`
4. Verify the result matches expectations

---

## Task 3: CLI Smoke Tests

Create `test/integration/cli_test.go` that tests the CLI binary directly
(shelling out to the built binary). This catches cobra wiring issues that
unit tests might miss.

Build the binary first (`go build -o bin/sol .`), then:

1. `bin/sol --help` exits 0, output contains "sol"
2. `bin/sol store --help` exits 0
3. `bin/sol session --help` exits 0
4. `bin/sol cast --help` exits 0
5. `bin/sol store create --world=testrig --title="test"` exits 0, prints an ID
6. `bin/sol store list --world=testrig --json` exits 0, output is valid JSON
7. `bin/sol agent create Smoke --world=testrig` exits 0
8. `bin/sol agent list --world=testrig` exits 0, contains "Smoke"

Each test should use a unique `SOL_HOME` temp directory.

---

## Task 4: Definition of Done Checklist

After all tests pass, verify each Loop 0 acceptance criterion manually.
Add a file `test/integration/LOOP0_ACCEPTANCE.md` documenting the results:

```markdown
# Loop 0 Acceptance Criteria

## 1. Create work item via store
- [ ] `sol store create --title="Add tests for login" --world=myworld` prints an ID
- [ ] ID format: sol-[0-9a-f]{8}

## 2. Dispatch to outpost
- [ ] `sol cast <id> myworld` spawns a outpost in a fresh worktree
- [ ] Worktree is at $SOL_HOME/myworld/outposts/{name}/world/
- [ ] .claude/CLAUDE.md exists with work item details

## 3. GUPP — work context injected on start
- [ ] Session starts with execution context visible
- [ ] `sol prime` output includes work item title and instructions

## 4. sol resolve completes work
- [ ] Branch pushed (or push attempted)
- [ ] Tether file cleared
- [ ] Work item status → resolve
- [ ] Agent state → idle

## 5. Operator observability
- [ ] `tmux attach -t sol-myworld-{name}` shows the agent working
- [ ] `sqlite3 $SOL_HOME/.store/myworld.db "SELECT * FROM work_items"` shows state

## 6. Crash recovery
- [ ] Kill tmux session → tether file persists
- [ ] Re-cast same item → agent picks up work

## 7. All tests pass
- [ ] `make test` exits 0
- [ ] Integration tests pass
- [ ] CLI smoke tests pass
```

---

## Task 5: Documentation

Add a brief `README.md` to the project root:

```markdown
# sol — Multi-Agent Orchestration

A production-ready system for coordinating concurrent AI coding agents.

## Quick Start (Loop 0)

```bash
# Build
make build

# Create a world and an agent
export SOL_HOME=~/sol
sol agent create Toast --world=myworld

# Create a work item
sol store create --world=myworld --title="Implement feature X" --description="..."

# Dispatch to the agent
sol cast <work-item-id> myworld

# Watch the agent work
sol session attach sol-myworld-Toast

# Check status
sol store list --world=myworld
sol session list
```

## Architecture

See the design documents in the original Gastown repository for the full
target architecture.

## Current Status

Loop 0: Single agent dispatch. One operator, one agent, one work item at
a time. Crash recovery via tether durability.
```

---

## Task 6: Final Verification

1. `make test` — all pass
2. `make build` — succeeds
3. Run the integration test suite: `go test ./test/integration/ -v -count=1`
4. Commit everything with a clear message.

---

## Guidelines

- Integration tests are slow. Use `t.Short()` to skip them in fast runs.
  Guard with `if testing.Short() { t.Skip("skipping integration test") }`.
- Use `t.Parallel()` only if tests use different SOL_HOME directories and
  different tmux servers. When in doubt, run sequentially.
- If a test is flaky due to tmux timing, add a reasonable sleep with a
  comment explaining why. Prefer polling with timeout over fixed sleeps.
- Don't mock anything in integration tests — use real SQLite, real tmux,
  real git. That's the point.
- If you discover bugs in the store, session, or dispatch code while
  writing integration tests, fix them.
