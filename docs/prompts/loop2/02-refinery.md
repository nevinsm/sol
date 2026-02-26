# Prompt 02: Loop 2 — Refinery

You are building the refinery for the `gt` orchestration system. The
refinery is a per-rig Go process that polls the merge queue, claims merge
requests, rebases onto the target branch, runs quality gates (tests), and
merges completed work. It is the core new component of Loop 2.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 01 (merge request store + done extension) is complete.

Read all existing code first. Understand the store merge request CRUD
(`internal/store/merge_requests.go`), the dispatch flock pattern
(`internal/dispatch/flock.go` — especially `MergeSlotLock`), the
supervisor package (`internal/supervisor/` — you'll extend it), the
session manager (`internal/session/manager.go`), and the config package
(`internal/config/config.go`).

Read `docs/target-architecture.md` Section 3.9 (Refinery) and Section 5
(Loop 2 requirements) for design context. Note: the architecture describes
the refinery sending mail messages (MERGED, MERGE_FAILED, REWORK_REQUEST)
— mail is deferred to Loop 3. Loop 2 updates database state only.

---

## Task 1: Refinery Package — Core Types and Config

Create `internal/refinery/` — the refinery manages the merge pipeline
for a single rig.

### Core Struct

```go
// internal/refinery/refinery.go
package refinery

import (
    "context"
    "log/slog"
    "time"

    "github.com/nevinsm/gt/internal/dispatch"
    "github.com/nevinsm/gt/internal/store"
)

// RigStore abstracts rig store operations for testing.
type RigStore interface {
    ClaimMergeRequest(claimerID string) (*store.MergeRequest, error)
    UpdateMergeRequestPhase(id, phase string) error
    ReleaseStaleClaims(ttl time.Duration) (int, error)
    GetWorkItem(id string) (*store.WorkItem, error)
    UpdateWorkItem(id string, updates store.WorkItemUpdates) error
    Close() error
}

// TownStore abstracts town store operations for testing.
type TownStore interface {
    CreateAgent(name, rig, role string) (string, error)
    GetAgent(id string) (*store.Agent, error)
    UpdateAgentState(id, state, hookItem string) error
    Close() error
}

// Config holds refinery configuration.
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

// Refinery processes the merge queue for a single rig.
type Refinery struct {
    rig        string
    agentID    string // "{rig}/refinery"
    sourceRepo string // path to the source git repo
    worktree   string // path to the refinery's persistent worktree
    rigStore   RigStore
    townStore  TownStore
    logger     *slog.Logger
    cfg        Config
}

// New creates a new Refinery.
func New(rig, sourceRepo string, rigStore RigStore, townStore TownStore,
    cfg Config, logger *slog.Logger) *Refinery

// Run starts the refinery's merge loop. Blocks until ctx is cancelled.
func (r *Refinery) Run(ctx context.Context) error
```

### Quality Gate Configuration

Quality gate commands are loaded from a file at
`$GT_HOME/{rig}/refinery/quality-gates.txt`. Each non-empty, non-comment
line is a shell command to execute in the worktree.

```go
// LoadQualityGates reads quality gate commands from the given file path.
// If the file does not exist, returns the default gates (no error).
// Lines starting with "#" and blank lines are skipped.
func LoadQualityGates(path string, defaults []string) ([]string, error)
```

The file path is `config.RigDir(rig) + "/refinery/quality-gates.txt"`.
Example file:

```
# Quality gates for this rig
go test ./...
go vet ./...
```

If the file doesn't exist, use the default: `["go test ./..."]`.

---

## Task 2: Persistent Worktree Setup

The refinery uses a persistent git worktree for merge operations. This
avoids creating and removing worktrees for every merge.

### Worktree Location

`$GT_HOME/{rig}/refinery/rig/` — on a dedicated branch
`refinery/{rig}`.

```go
// RefineryWorktreePath returns the worktree directory for a rig's refinery.
func RefineryWorktreePath(rig string) string {
    return filepath.Join(config.Home(), rig, "refinery", "rig")
}

// RefineryBranch returns the branch name for a rig's refinery worktree.
func RefineryBranch(rig string) string {
    return "refinery/" + rig
}
```

Add these to either `internal/refinery/refinery.go` or
`internal/dispatch/dispatch.go` (alongside the existing
`WorktreePath` and `SessionName` helpers).

### Setup Logic

The refinery's `Run()` method calls `ensureWorktree()` on startup:

```go
func (r *Refinery) ensureWorktree() error
```

If the worktree directory already exists, verify it's a valid git
worktree and return. If it doesn't exist:

1. Create parent directory: `os.MkdirAll(parentDir, 0o755)`
2. Create worktree:
   ```bash
   git -C <sourceRepo> worktree add -b refinery/<rig> <worktreeDir> HEAD
   ```
3. If the branch already exists (worktree was removed but branch
   persists): use `git worktree add <worktreeDir> refinery/<rig>`
   (no `-b` flag).

**Error messages:**
- `"failed to create refinery worktree for rig %q: %w"`
- `"failed to verify refinery worktree for rig %q: %w"`

---

## Task 3: Merge Loop

The refinery's main loop polls for ready merge requests and processes
them one at a time.

### Run() Implementation

```go
func (r *Refinery) Run(ctx context.Context) error {
    // 1. Ensure worktree exists
    if err := r.ensureWorktree(); err != nil {
        return err
    }

    // 2. Register refinery agent in town store
    if err := r.registerAgent(); err != nil {
        return err
    }

    // 3. Log startup
    r.logger.Info("refinery started", "rig", r.rig, "worktree", r.worktree)

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
func (r *Refinery) registerAgent() error
```

Check if the refinery agent already exists in the town store
(`townStore.GetAgent(r.agentID)`). If not, create it:
`townStore.CreateAgent("refinery", r.rig, "refinery")`.

Set agent state to "working":
`townStore.UpdateAgentState(r.agentID, "working", "")`.

The agent ID is `"{rig}/refinery"` (e.g., `myrig/refinery`).

### Poll Cycle

```go
func (r *Refinery) poll()
```

Each poll cycle:

1. **Release stale claims:** Call
   `r.rigStore.ReleaseStaleClaims(r.cfg.ClaimTTL)`. If any were
   released, log at WARN level.

2. **Claim next MR:** Call
   `r.rigStore.ClaimMergeRequest(r.agentID)`. If nil (no ready MRs),
   return immediately.

3. **Check max attempts:** If `mr.Attempts > r.cfg.MaxAttempts`, set
   phase to `"failed"` and log at ERROR. Return (pick up next MR on
   next tick).

4. **Acquire merge slot:** Call
   `dispatch.AcquireMergeSlotLock(r.rig)`. If busy (shouldn't happen
   with a single refinery, but defensive), log warning and release
   the claim (set phase back to `"ready"`). Return.

5. **Process the merge:** Call `r.processMerge(mr)`.

6. **Release merge slot.**

### Process Merge

```go
func (r *Refinery) processMerge(mr *store.MergeRequest) error
```

This is the core merge pipeline:

1. **Sync worktree to target branch:**
   ```bash
   git -C <worktree> fetch origin
   git -C <worktree> checkout <refineryBranch>
   git -C <worktree> reset --hard origin/<targetBranch>
   ```

2. **Merge polecat's branch:**
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
       "GT_HOME="+config.Home(),
       "GT_RIG="+r.rig,
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
     `r.rigStore.UpdateWorkItem(mr.WorkItemID, store.WorkItemUpdates{Status: "closed"})`
   - Log at INFO: `"merged"` with MR, work item, and branch details
   - Clean up remote branch (best-effort, don't fail on error):
     ```bash
     git -C <worktree> push origin --delete <mr.Branch>
     ```

### Shutdown

```go
func (r *Refinery) shutdown() error
```

On shutdown (context cancelled):
1. Set agent state to `"idle"`:
   `townStore.UpdateAgentState(r.agentID, "idle", "")`
2. Log: `"refinery stopped"`
3. Return nil

---

## Task 4: Structured Logging

Reuse the supervisor's logging pattern:

```go
// internal/refinery/logging.go
package refinery

// NewLogger creates an slog.Logger writing JSON to path.
// If path is empty, logs to stderr.
// Opens file with O_CREATE|O_APPEND|O_WRONLY.
func NewLogger(path string) (*slog.Logger, *os.File, error)
```

This is identical to `supervisor.NewLogger()`. If you want to avoid
duplication, extract a shared logging helper — but it's fine to copy
the ~15 lines as well.

Log file location: `$GT_HOME/.runtime/refinery-{rig}.log`

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

## Task 5: Supervisor Extension

Modify `internal/supervisor/supervisor.go` so the supervisor can restart
refinery agents when their sessions die. Currently, the supervisor
respawns all crashed agents with `claude --dangerously-skip-permissions`.
Refineries need a different command.

### Changes to Respawn Logic

In the supervisor's respawn function, check the agent's role before
deciding the startup command:

```go
func (s *Supervisor) respawnCommand(agent store.Agent) string {
    switch agent.Role {
    case "refinery":
        return fmt.Sprintf("gt refinery run %s", agent.Rig)
    default:
        return "claude --dangerously-skip-permissions"
    }
}
```

Use this in the `Start()` call instead of the hardcoded command string.

### Worktree Path by Role

The supervisor computes the worktree path for respawns. Refineries have
a different worktree location:

```go
func worktreeForAgent(agent store.Agent) string {
    switch agent.Role {
    case "refinery":
        return refinery.RefineryWorktreePath(agent.Rig)
    default:
        return dispatch.WorktreePath(agent.Rig, agent.Name)
    }
}
```

Import the refinery package for the path helper, or add the helper to
the dispatch package alongside `WorktreePath` to avoid a circular
dependency.

**Important:** Keep the supervisor changes minimal. Only modify the
respawn path — don't change heartbeat, mass-death detection, or any
other supervisor behavior.

### TownStore Interface

The supervisor's `TownStore` interface currently only needs `ListAgents`
and `UpdateAgentState`. Check that these are sufficient for handling
refinery agents — they should be, since the refinery registers itself
with `CreateAgent` during startup (not handled by the supervisor).

---

## Task 6: Tests

### Refinery Unit Tests

Create `internal/refinery/refinery_test.go`:

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
    // New refinery, no agent in town store
    // Run registerAgent
    // Verify: agent created with role="refinery", state="working"

func TestRegisterAgentIdempotent(t *testing.T)
    // Agent already exists in town store
    // Run registerAgent
    // Verify: no error, agent state set to "working"

func TestShutdown(t *testing.T)
    // Start refinery, cancel context
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
    // Setup: git repo, branch with changes, refinery worktree
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

### Supervisor Extension Tests

Add to `internal/supervisor/supervisor_test.go`:

```go
func TestRespawnRefinery(t *testing.T)
    // Create an agent with role="refinery" in state "working"
    // Kill the mock session
    // Run a heartbeat cycle
    // Verify: session restarted with "gt refinery run <rig>" command
    //   (not "claude --dangerously-skip-permissions")

func TestRespawnPolecatUnchanged(t *testing.T)
    // Create an agent with role="polecat" in state "working"
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
   export GT_HOME=/tmp/gt-test
   # Create a source repo for testing
   cd /tmp && git init --bare test-repo.git
   git clone test-repo.git test-repo && cd test-repo
   echo "package main" > main.go && git add . && git commit -m "init"
   git push origin main && cd ~/gt-src

   # Create work and dispatch
   bin/gt store create --db=testrig --title="Add feature X"
   bin/gt sling <id> testrig

   # Simulate polecat completing work
   # (in the polecat's worktree, make changes, then:)
   bin/gt done --rig=testrig --agent=<name>
   # Should show: Merge request: mr-XXXXXXXX (queued)

   # Start the refinery
   bin/gt refinery run testrig
   # (The refinery should pick up the MR, merge it)

   # Verify: MR is merged in the database
   sqlite3 /tmp/gt-test/.store/testrig.db \
     "SELECT id, phase, merged_at FROM merge_requests"
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Deferred to Later Loops

The following architecture features are **intentionally deferred**:

- **Mail messages** (MERGED, MERGE_FAILED, REWORK_REQUEST): The
  architecture shows the refinery sending mail to witnesses. Mail is
  a Loop 3 feature. Loop 2 updates database state only.
- **Conflict resolution**: When a rebase conflict occurs, Loop 2 marks
  the MR as `"failed"`. Loop 4 adds the REWORK_REQUEST → re-dispatch
  pipeline for resolving conflicts.
- **Convoy awareness**: The refinery may detect convoy-eligible MRs
  in later loops. Loop 2 processes each MR individually.
- **Heartbeat files**: The refinery could write heartbeat files for
  liveness monitoring. Loop 2 relies on tmux session existence only
  (same as supervisor for polecats in Loop 1).

---

## Guidelines

- The refinery is a Go process, not an AI agent. The merge pipeline
  in Loop 2 is entirely mechanical (poll, rebase, test, merge). No
  Claude interaction needed. The "AI Agent" label in the architecture
  is for later loops where conflict resolution requires judgment.
- One refinery per rig. The merge slot lock is defensive — it
  prevents damage if two refineries accidentally run for the same rig.
- Git operations shell out to the `git` CLI. Don't use a git library.
  Match the existing dispatch pattern (see `dispatch.Done()` git calls).
- Quality gates run in the refinery's worktree. Each gate is executed
  as `sh -c <command>`. Gates run sequentially — all must pass.
- The refinery worktree is NOT cleaned up on shutdown. It persists
  between runs. This is intentional — avoids re-creating it on restart.
- The supervisor extension must not break existing polecat respawn
  behavior. The `role` field on agents is the discriminator.
- Use `dispatch.SessionName(rig, "refinery")` for the refinery's
  session name: `gt-{rig}-refinery`.
- Commit after tests pass with message:
  `feat(refinery): add merge queue processor with quality gates and supervisor integration`
