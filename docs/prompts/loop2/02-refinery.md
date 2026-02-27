# Prompt 02: Loop 2 — Forge

You are building the forge for the `sol` orchestration system. The
forge is a per-world Go process that polls the merge queue, claims merge
requests, rebases onto the target branch, runs quality gates (tests), and
merges completed work. It is the core new component of Loop 2.

**Working directory:** `~/sol-src/`
**Prerequisite:** Prompt 01 (merge request store + done extension) is complete.

Read all existing code first. Understand the store merge request CRUD
(`internal/store/merge_requests.go`), the dispatch flock pattern
(`internal/dispatch/flock.go` — especially `MergeSlotLock`), the
prefect package (`internal/prefect/` — you'll extend it), the
session manager (`internal/session/manager.go`), and the config package
(`internal/config/config.go`).

Read `docs/target-architecture.md` Section 3.9 (Forge) and Section 5
(Loop 2 requirements) for design context. Note: the architecture describes
the forge sending mail messages (MERGED, MERGE_FAILED, REWORK_REQUEST)
— mail is deferred to Loop 3. Loop 2 updates database state only.

---

## Task 1: Forge Package — Core Types and Config

Create `internal/forge/` — the forge manages the merge pipeline
for a single world.

### Core Struct

```go
// internal/forge/forge.go
package forge

import (
    "context"
    "log/slog"
    "time"

    "github.com/nevinsm/sol/internal/dispatch"
    "github.com/nevinsm/sol/internal/store"
)

// WorldStore abstracts world store operations for testing.
type WorldStore interface {
    ClaimMergeRequest(claimerID string) (*store.MergeRequest, error)
    UpdateMergeRequestPhase(id, phase string) error
    ReleaseStaleClaims(ttl time.Duration) (int, error)
    GetWorkItem(id string) (*store.WorkItem, error)
    UpdateWorkItem(id string, updates store.WorkItemUpdates) error
    Close() error
}

// SphereStore abstracts sphere store operations for testing.
type SphereStore interface {
    CreateAgent(name, world, role string) (string, error)
    GetAgent(id string) (*store.Agent, error)
    UpdateAgentState(id, state, tetherItem string) error
    Close() error
}

// Config holds forge configuration.
type Config struct {
    PollInterval  time.Duration // how often to poll for ready MRs (default: 10s)
    ClaimTTL      time.Duration // TTL before stale claims are released (default: 30min)
    MaxAttempts   int           // max merge attempts before marking failed (default: 3)
    TargetBranch  string        // branch to merge into (default: "main")
    QualityGates  []string      // commands to run as quality gates
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
    return Config{
        PollInterval: 10 * time.Second,
        ClaimTTL:     30 * time.Minute,
        MaxAttempts:  3,
        TargetBranch: "main",
        QualityGates: []string{"go test ./..."},
    }
}

// Forge processes the merge queue for a single world.
type Forge struct {
    world        string
    agentID    string // "{world}/forge"
    sourceRepo string // path to the source git repo
    worktree   string // path to the forge's persistent worktree
    worldStore   WorldStore
    sphereStore  SphereStore
    logger     *slog.Logger
    cfg        Config
}

// New creates a new Forge.
func New(world, sourceRepo string, worldStore WorldStore, sphereStore SphereStore,
    cfg Config, logger *slog.Logger) *Forge

// Run starts the forge's merge loop. Blocks until ctx is cancelled.
func (r *Forge) Run(ctx context.Context) error
```

### Quality Gate Configuration

Quality gate commands are loaded from a file at
`$SOL_HOME/{world}/forge/quality-gates.txt`. Each non-empty, non-comment
line is a shell command to execute in the worktree.

```go
// LoadQualityGates reads quality gate commands from the given file path.
// If the file does not exist, returns the default gates (no error).
// Lines starting with "#" and blank lines are skipped.
func LoadQualityGates(path string, defaults []string) ([]string, error)
```

The file path is `config.RigDir(world) + "/forge/quality-gates.txt"`.
Example file:

```
# Quality gates for this world
go test ./...
go vet ./...
```

If the file doesn't exist, use the default: `["go test ./..."]`.

---

## Task 2: Persistent Worktree Setup

The forge uses a persistent git worktree for merge operations. This
avoids creating and removing worktrees for every merge.

### Worktree Location

`$SOL_HOME/{world}/forge/world/` — on a dedicated branch
`forge/{world}`.

```go
// RefineryWorktreePath returns the worktree directory for a world's forge.
func RefineryWorktreePath(world string) string {
    return filepath.Join(config.Home(), world, "forge", "world")
}

// RefineryBranch returns the branch name for a world's forge worktree.
func RefineryBranch(world string) string {
    return "forge/" + world
}
```

Add these to either `internal/forge/forge.go` or
`internal/dispatch/dispatch.go` (alongside the existing
`WorktreePath` and `SessionName` helpers).

### Setup Logic

The forge's `Run()` method calls `ensureWorktree()` on startup:

```go
func (r *Forge) ensureWorktree() error
```

If the worktree directory already exists, verify it's a valid git
worktree and return. If it doesn't exist:

1. Create parent directory: `os.MkdirAll(parentDir, 0o755)`
2. Create worktree:
   ```bash
   git -C <sourceRepo> worktree add -b forge/<world> <worktreeDir> HEAD
   ```
3. If the branch already exists (worktree was removed but branch
   persists): use `git worktree add <worktreeDir> forge/<world>`
   (no `-b` flag).

**Error messages:**
- `"failed to create forge worktree for world %q: %w"`
- `"failed to verify forge worktree for world %q: %w"`

---

## Task 3: Merge Loop

The forge's main loop polls for ready merge requests and processes
them one at a time.

### Run() Implementation

```go
func (r *Forge) Run(ctx context.Context) error {
    // 1. Ensure worktree exists
    if err := r.ensureWorktree(); err != nil {
        return err
    }

    // 2. Register forge agent in sphere store
    if err := r.registerAgent(); err != nil {
        return err
    }

    // 3. Log startup
    r.logger.Info("forge started", "world", r.world, "worktree", r.worktree)

    // 4. Main loop
    ticker := time.NewTicker(r.cfg.PollInterval)
    defer ticker.Stop()

    // Process immediately on startup, then on each tick
    r.poll()
    for {
        select {
        case <-ctx.Done():
            return r.shutdown()
        case <-ticker.C:
            r.poll()
        }
    }
}
```

### Agent Registration

```go
func (r *Forge) registerAgent() error
```

Check if the forge agent already exists in the sphere store
(`sphereStore.GetAgent(r.agentID)`). If not, create it:
`sphereStore.CreateAgent("forge", r.world, "forge")`.

Set agent state to "working":
`sphereStore.UpdateAgentState(r.agentID, "working", "")`.

The agent ID is `"{world}/forge"` (e.g., `myworld/forge`).

### Poll Cycle

```go
func (r *Forge) poll()
```

Each poll cycle:

1. **Release stale claims:** Call
   `r.worldStore.ReleaseStaleClaims(r.cfg.ClaimTTL)`. If any were
   released, log at WARN level.

2. **Claim next MR:** Call
   `r.worldStore.ClaimMergeRequest(r.agentID)`. If nil (no ready MRs),
   return immediately.

3. **Check max attempts:** If `mr.Attempts > r.cfg.MaxAttempts`, set
   phase to `"failed"` and log at ERROR. Return (pick up next MR on
   next tick).

4. **Acquire merge slot:** Call
   `dispatch.AcquireMergeSlotLock(r.world)`. If busy (shouldn't happen
   with a single forge, but defensive), log warning and release
   the claim (set phase back to `"ready"`). Return.

5. **Process the merge:** Call `r.processMerge(mr)`.

6. **Release merge slot.**

### Process Merge

```go
func (r *Forge) processMerge(mr *store.MergeRequest) error
```

This is the core merge pipeline:

1. **Sync worktree to target branch:**
   ```bash
   git -C <worktree> fetch origin
   git -C <worktree> checkout <refineryBranch>
   git -C <worktree> reset --hard origin/<targetBranch>
   ```

2. **Merge outpost's branch:**
   ```bash
   git -C <worktree> merge --no-ff origin/<mr.Branch>
   ```
   If the merge fails (conflict):
   - `git -C <worktree> merge --abort`
   - Set MR phase to `"failed"` (conflicts require human intervention;
     conflict resolution is deferred to Loop 4)
   - Log at ERROR: `"rebase conflict"` with MR and branch details
   - Return

3. **Run quality gates:**
   For each command in `r.cfg.QualityGates`:
   ```go
   cmd := exec.CommandContext(ctx, "sh", "-c", gate)
   cmd.Dir = r.worktree
   cmd.Env = append(os.Environ(),
       "SOL_HOME="+config.Home(),
       "SOL_WORLD="+r.world,
   )
   output, err := cmd.CombinedOutput()
   ```
   If any gate fails:
   - `git -C <worktree> reset --hard origin/<targetBranch>`
   - Set MR phase to `"ready"` (will be retried, up to MaxAttempts)
   - Log at WARN: `"quality gate failed"` with gate command and
     output snippet (first 500 bytes)
   - Return

4. **Push to target branch:**
   ```bash
   git -C <worktree> push origin HEAD:<targetBranch>
   ```
   If push fails (someone pushed to main since we fetched):
   - `git -C <worktree> reset --hard origin/<targetBranch>`
   - Set MR phase to `"ready"` (will be retried)
   - Log at WARN: `"push rejected, will retry"`
   - Return

5. **Success — update state:**
   - Set MR phase to `"merged"`
   - Update work item status to `"closed"`:
     `r.worldStore.UpdateWorkItem(mr.WorkItemID, store.WorkItemUpdates{Status: "closed"})`
   - Log at INFO: `"merged"` with MR, work item, and branch details
   - Clean up remote branch (best-effort, don't fail on error):
     ```bash
     git -C <worktree> push origin --delete <mr.Branch>
     ```

### Shutdown

```go
func (r *Forge) shutdown() error
```

On shutdown (context cancelled):
1. Set agent state to `"idle"`:
   `sphereStore.UpdateAgentState(r.agentID, "idle", "")`
2. Log: `"forge stopped"`
3. Return nil

---

## Task 4: Structured Logging

Reuse the prefect's logging pattern:

```go
// internal/forge/logging.go
package forge

// NewLogger creates an slog.Logger writing JSON to path.
// If path is empty, logs to stderr.
// Opens file with O_CREATE|O_APPEND|O_WRONLY.
func NewLogger(path string) (*slog.Logger, *os.File, error)
```

This is identical to `prefect.NewLogger()`. If you want to avoid
duplication, extract a shared logging helper — but it's fine to copy
the ~15 lines as well.

Log file location: `$SOL_HOME/.runtime/forge-{world}.log`

Use structured fields in all log calls:
```go
r.logger.Info("merged", "mr", mr.ID, "work_item", mr.WorkItemID,
    "branch", mr.Branch, "attempts", mr.Attempts)
r.logger.Warn("quality gate failed", "mr", mr.ID, "gate", gate,
    "output", truncated)
r.logger.Error("rebase conflict", "mr", mr.ID, "branch", mr.Branch)
r.logger.Warn("released stale claims", "count", n)
r.logger.Info("poll", "ready_mrs", 0)  // only log when there are items or periodically
```

---

## Task 5: Prefect Extension

Modify `internal/prefect/prefect.go` so the prefect can restart
forge agents when their sessions die. Currently, the prefect
respawns all crashed agents with `claude --dangerously-skip-permissions`.
Forges need a different command.

### Changes to Respawn Logic

In the prefect's respawn function, check the agent's role before
deciding the startup command:

```go
func (s *Prefect) respawnCommand(agent store.Agent) string {
    switch agent.Role {
    case "forge":
        return fmt.Sprintf("sol forge run %s", agent.World)
    default:
        return "claude --dangerously-skip-permissions"
    }
}
```

Use this in the `Start()` call instead of the hardcoded command string.

### Worktree Path by Role

The prefect computes the worktree path for respawns. Forges have
a different worktree location:

```go
func worktreeForAgent(agent store.Agent) string {
    switch agent.Role {
    case "forge":
        return forge.RefineryWorktreePath(agent.World)
    default:
        return dispatch.WorktreePath(agent.World, agent.Name)
    }
}
```

Import the forge package for the path helper, or add the helper to
the dispatch package alongside `WorktreePath` to avoid a circular
dependency.

**Important:** Keep the prefect changes minimal. Only modify the
respawn path — don't change heartbeat, mass-death detection, or any
other prefect behavior.

### SphereStore Interface

The prefect's `SphereStore` interface currently only needs `ListAgents`
and `UpdateAgentState`. Check that these are sufficient for handling
forge agents — they should be, since the forge registers itself
with `CreateAgent` during startup (not handled by the prefect).

---

## Task 6: Tests

### Forge Unit Tests

Create `internal/forge/refinery_test.go`:

Mock interfaces:

```go
type mockRigStore struct {
    mu     sync.Mutex
    mrs    []store.MergeRequest
    items  map[string]*store.WorkItem
    claims []string // IDs of claimed MRs
}

type mockTownStore struct {
    agents map[string]*store.Agent
}
```

Test cases:

```go
func TestPollClaimsAndProcesses(t *testing.T)
    // Mock store has one ready MR
    // Run one poll cycle
    // Verify: MR was claimed, processMerge was called
    // (Use a test flag or mock git operations)

func TestPollReleasesStale(t *testing.T)
    // Mock store has a stale claimed MR (claimed_at > TTL)
    // Run one poll cycle
    // Verify: ReleaseStaleClaims was called

func TestPollSkipsWhenEmpty(t *testing.T)
    // Mock store has no ready MRs
    // Run one poll cycle
    // Verify: no errors, no side effects

func TestMaxAttemptsExceeded(t *testing.T)
    // Mock store returns a MR with attempts > MaxAttempts
    // Run one poll cycle
    // Verify: MR phase set to "failed", not processed

func TestRegisterAgent(t *testing.T)
    // New forge, no agent in sphere store
    // Run registerAgent
    // Verify: agent created with role="forge", state="working"

func TestRegisterAgentIdempotent(t *testing.T)
    // Agent already exists in sphere store
    // Run registerAgent
    // Verify: no error, agent state set to "working"

func TestShutdown(t *testing.T)
    // Start forge, cancel context
    // Verify: agent state set to "idle"

func TestLoadQualityGates(t *testing.T)
    // Write a temp file with gate commands
    // Load -> verify commands match
    // Load non-existent path -> verify defaults returned
    // Load file with comments and blanks -> verify they're skipped

func TestLoadQualityGatesDefaults(t *testing.T)
    // Load with non-existent path
    // Verify: returns default gates
```

### Git Operations Tests

Testing the git merge pipeline requires a real git repo. Create helper
functions:

```go
func setupGitTest(t *testing.T) (sourceRepo, worktree string)
    // 1. Create a temp bare repo (git init --bare)
    // 2. Clone it to a working dir
    // 3. Make an initial commit on main
    // 4. Push main to origin
    // Return paths

func createBranchWithChanges(t *testing.T, repoDir, branch, filename, content string)
    // 1. git checkout -b <branch>
    // 2. Write a file
    // 3. git add + commit
    // 4. git push origin <branch>
    // 5. git checkout main
```

```go
func TestProcessMergeSuccess(t *testing.T)
    // Setup: git repo, branch with changes, forge worktree
    // Create MR for the branch
    // Call processMerge
    // Verify: branch merged into main, MR phase=merged

func TestProcessMergeConflict(t *testing.T)
    // Setup: git repo, two branches that modify the same file differently
    // Merge first branch to main
    // Try to merge second branch -> conflict
    // Verify: MR phase=failed, worktree reset to main

func TestProcessMergeQualityGateFail(t *testing.T)
    // Setup: git repo, branch with changes
    // Set quality gate to a command that fails (e.g., "exit 1")
    // Call processMerge
    // Verify: MR phase=ready (will retry), worktree reset

func TestProcessMergePushRejected(t *testing.T)
    // Setup: git repo, branch with changes
    // After merge+test but before push, push a commit to main directly
    // Verify: push fails, MR phase=ready (will retry), worktree reset
```

### Prefect Extension Tests

Add to `internal/prefect/supervisor_test.go`:

```go
func TestRespawnRefinery(t *testing.T)
    // Create an agent with role="forge" in state "working"
    // Kill the mock session
    // Run a heartbeat cycle
    // Verify: session restarted with "sol forge run <world>" command
    //   (not "claude --dangerously-skip-permissions")

func TestRespawnPolecatUnchanged(t *testing.T)
    // Create an agent with role="outpost" in state "working"
    // Kill the mock session
    // Run a heartbeat cycle
    // Verify: session restarted with "claude --dangerously-skip-permissions"
    //   (existing behavior preserved)
```

---

## Task 7: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test of the full pipeline:
   ```bash
   export SOL_HOME=/tmp/sol-test
   # Create a source repo for testing
   cd /tmp && git init --bare test-repo.git
   git clone test-repo.git test-repo && cd test-repo
   echo "package main" > main.go && git add . && git commit -m "init"
   git push origin main && cd ~/sol-src

   # Create work and dispatch
   bin/sol store create --world=testrig --title="Add feature X"
   bin/sol cast <id> testrig

   # Simulate outpost completing work
   # (in the outpost's worktree, make changes, then:)
   bin/sol done --world=testrig --agent=<name>
   # Should show: Merge request: mr-XXXXXXXX (queued)

   # Start the forge
   bin/sol forge run testrig
   # (The forge should pick up the MR, merge it)

   # Verify: MR is merged in the database
   sqlite3 /tmp/sol-test/.store/testrig.db \
     "SELECT id, phase, merged_at FROM merge_requests"
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Deferred to Later Loops

The following architecture features are **intentionally deferred**:

- **Mail messages** (MERGED, MERGE_FAILED, REWORK_REQUEST): The
  architecture shows the forge sending mail to sentinels. Mail is
  a Loop 3 feature. Loop 2 updates database state only.
- **Conflict resolution**: When a rebase conflict occurs, Loop 2 marks
  the MR as `"failed"`. Loop 4 adds the REWORK_REQUEST → re-dispatch
  pipeline for resolving conflicts.
- **Caravan awareness**: The forge may detect car-eligible MRs
  in later loops. Loop 2 processes each MR individually.
- **Heartbeat files**: The forge could write heartbeat files for
  liveness monitoring. Loop 2 relies on tmux session existence only
  (same as prefect for outposts in Loop 1).

---

## Guidelines

- The forge is a Go process, not an AI agent. The merge pipeline
  in Loop 2 is entirely mechanical (poll, rebase, test, merge). No
  Claude interaction needed. The "AI Agent" label in the architecture
  is for later loops where conflict resolution requires judgment.
- One forge per world. The merge slot lock is defensive — it
  prevents damage if two forges accidentally run for the same world.
- Git operations shell out to the `git` CLI. Don't use a git library.
  Match the existing dispatch pattern (see `dispatch.Done()` git calls).
- Quality gates run in the forge's worktree. Each gate is executed
  as `sh -c <command>`. Gates run sequentially — all must pass.
- The forge worktree is NOT cleaned up on shutdown. It persists
  between runs. This is intentional — avoids re-creating it on restart.
- The prefect extension must not break existing outpost respawn
  behavior. The `role` field on agents is the discriminator.
- Use `dispatch.SessionName(world, "forge")` for the forge's
  session name: `sol-{world}-forge`.
- Commit after tests pass with message:
  `feat(forge): add merge queue processor with quality gates and prefect integration`
