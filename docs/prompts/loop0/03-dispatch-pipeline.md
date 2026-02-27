# Prompt 03: Loop 0 — Dispatch Pipeline (cast, prime, done)

You are building the dispatch pipeline for the `sol` orchestration system —
the commands that assign work to an agent, inject execution context, and
handle work completion. This is what makes the system actually do something.

**Working directory:** `~/sol-src/`
**Prerequisite:** Prompts 01 (store) and 02 (session manager) are complete.

Read the existing code first — understand the store package (work items,
agents), session manager (tmux operations), config package (SOL_HOME), and
cobra setup. Build on what's there.

---

## Task 1: Tether Package

Create `internal/tether/tether.go`. The tether is the durability primitive — a
file that records which work item is assigned to an agent. It survives
session crashes, restarts, and context loss.

```go
package tether

// TetherPath returns the path to an agent's tether file.
// Path: $SOL_HOME/{world}/outposts/{name}/.tether
func TetherPath(world, agentName string) string

// Read reads the tether file and returns the work item ID.
// Returns ("", nil) if no tether file exists (agent is idle).
func Read(world, agentName string) (string, error)

// Write writes a work item ID to the tether file.
// Creates parent directories if needed.
func Write(world, agentName, workItemID string) error

// Clear removes the tether file. No-op if it doesn't exist.
func Clear(world, agentName string) error

// IsTethered returns true if a tether file exists for the agent.
func IsTethered(world, agentName string) bool
```

The tether file is a plain text file containing just the work item ID
(e.g., `sol-a1b2c3d4`), no newline.

Create `internal/tether/hook_test.go` with tests for Read/Write/Clear/IsTethered,
including the "no tether" case.

---

## Task 2: Protocol Package

Create `internal/protocol/`. This package generates the files that teach
an AI agent how to interact with the orchestration system.

### CLAUDE.md Generation

`internal/protocol/claudemd.go`:

```go
package protocol

type ClaudeMDContext struct {
    AgentName   string
    World         string
    WorkItemID  string
    Title       string
    Description string
}

// GenerateClaudeMD returns the contents of a CLAUDE.md file for a outpost
// agent. This file is the agent's entire understanding of the system.
func GenerateClaudeMD(ctx ClaudeMDContext) string

// InstallClaudeMD writes .claude/CLAUDE.md into the given worktree directory.
// Creates .claude/ if it doesn't exist.
func InstallClaudeMD(worktreeDir string, ctx ClaudeMDContext) error
```

The generated CLAUDE.md content:

```markdown
# Outpost Agent: {AgentName} (world: {World})

You are a outpost agent in a multi-agent orchestration system.
Your job is to execute the assigned work item.

## Your Assignment
- Work item: {WorkItemID}
- Title: {Title}
- Description: {Description}

## Commands
- `sol resolve` — Signal that your work is complete. This pushes your branch,
  clears your tether, and ends your session. Only run this when you are
  confident the work is done.
- `sol escalate` — Request help if you are stuck. Describe the problem.

## Protocol
1. Read your assignment above carefully.
2. Execute the work in this worktree.
3. When finished, run `sol resolve`.
4. If you cannot complete the work, run `sol escalate "description of problem"`.

## Important
- You are working in an isolated git worktree. Commit your changes normally.
- Do not modify files outside this worktree.
- Do not attempt to interact with other agents directly.
```

### Claude Code Tethers

`internal/protocol/tethers.go`:

```go
package protocol

// InstallTethers writes Claude Code tether scripts into the worktree.
// Creates .claude/tethers/ directory.
//
// Tethers installed:
//   SessionStart: runs "sol prime --world={world} --agent={name}" and outputs
//                 the result as initial context
//
// The tether scripts are shell scripts that call sol subcommands.
func InstallTethers(worktreeDir, world, agentName string) error
```

The SessionStart tether script (`.claude/tethers/session-start.sh`):

```bash
#!/bin/bash
# SessionStart tether — inject execution context via sol prime
exec sol prime --world="$SOL_WORLD" --agent="$SOL_AGENT"
```

Note: The tether should be registered in `.claude/settings.local.json`
per Claude Code's tether configuration format. Research the correct format
if needed — it typically looks like:

```json
{
  "tethers": {
    "SessionStart": [
      {
        "type": "command",
        "command": ".claude/tethers/session-start.sh"
      }
    ]
  }
}
```

Write tests for CLAUDE.md generation (verify output contains the expected
fields) and tether installation (verify files are created with correct content).

---

## Task 3: `sol cast` Command

This is the main dispatch command. It assigns a work item to a outpost
agent and starts its session.

Create `cmd/cast.go`:

```
sol cast <work-item-id> <world> [--agent=<name>]
```

**Sequence:**

1. **Open world store**, get the work item. Fail if not found or not `open`.
2. **Open sphere store**, find the agent:
   - If `--agent` specified: use that agent. Fail if not found or not idle.
   - If not specified: find the first idle outpost for the world.
   - If no idle agents: fail with a clear message.
3. **Create worktree directory**: `$SOL_HOME/{world}/outposts/{name}/world/`
   - If directory already exists from a previous assignment, remove it first.
   - Initialize as a git worktree:
     `git worktree add <path> -b outpost/{name}/{work-item-id} HEAD`
   - This creates a new branch from the current HEAD of the world's repo.
   - The world's repo path must be configured. For Loop 0, use the current
     working directory (`$PWD`) as the source repo. (The operator runs
     `sol cast` from within the project repo.)
4. **Write tether file**: `$SOL_HOME/{world}/outposts/{name}/.tether` ← work item ID
5. **Update work item**: status → `tethered`, assignee → agent ID
6. **Update agent**: state → `working`, hook_item → work item ID
7. **Install CLAUDE.md** in the worktree
8. **Install Claude Code tethers** in the worktree
9. **Start tmux session**:
   - Name: `sol-{world}-{agentName}` (e.g., `sol-myworld-Toast`)
   - Workdir: the worktree path
   - Cmd: `claude --dangerously-skip-permissions` (the AI agent)
   - Env: `SOL_HOME`, `SOL_WORLD={world}`, `SOL_AGENT={name}`

**Rollback on failure:** If any step after the work item update fails
(e.g., tmux start fails), roll back: clear tether, restore work item to
`open`, restore agent to `idle`.

**Output:** On success, print:
```
Cast sol-a1b2c3d4 → Toast (sol-myworld-Toast)
  Worktree: /home/user/sol/myworld/outposts/Toast/world
  Session:  sol-myworld-Toast
  Attach:   sol session attach sol-myworld-Toast
```

---

## Task 4: `sol prime` Command

This command assembles execution context from durable state. It's called
by the SessionStart tether when an agent session starts (or restarts).

Create `cmd/prime.go`:

```
sol prime --world=<world> --agent=<name>
```

**Sequence:**

1. Read the tether file for this agent. If no tether, print "No work tethered"
   and exit 0.
2. Open world store, get the work item by ID.
3. Format and print the execution context to stdout:

```
=== WORK CONTEXT ===
Agent: {name} (world: {world})
Work Item: {id}
Title: {title}
Status: {status}

Description:
{description}

Instructions:
Execute this work item. When complete, run: sol resolve
If stuck, run: sol escalate "description"
=== END CONTEXT ===
```

This is intentionally simple for Loop 0. Later loops add workflow step
context and pending message summaries.

---

## Task 5: `sol resolve` Command

This command signals work completion. Called by the outpost agent when
it finishes executing a work item.

Create `cmd/done.go`:

```
sol resolve [--world=<world>] [--agent=<name>]
```

If `--world` and `--agent` are not provided, infer them from the `SOL_WORLD`
and `SOL_AGENT` environment variables (set by `sol cast` when starting
the session).

**Sequence:**

1. **Read tether**: Get the work item ID from the tether file. Fail if no tether.
2. **Git operations** (in the worktree):
   - `git add -A`
   - `git commit -m "sol resolve: {work-item-title}"` (skip if nothing to commit)
   - `git push origin HEAD` (push the branch to remote; warn but don't
     fail if push fails — the branch exists locally either way)
3. **Update work item**: status → `resolve`
4. **Update agent**: state → `idle`, hook_item → clear
5. **Clear tether file**
6. **Stop session**: Kill the tmux session for this agent.
   The session name is `sol-{world}-{agentName}`.

**Output:**
```
Done: sol-a1b2c3d4 ({title})
  Branch: outpost/{name}/{work-item-id}
  Agent {name} is now idle.
```

**Important:** The `sol resolve` command runs inside the tmux session (the
agent calls it). After it kills its own session (step 6), the command
won't finish printing. That's OK — the important state changes (steps
1-5) happen before the session kill. Use a brief delay (1s) between the
last state update and session kill to let output flush. Alternatively,
send the session kill as a background operation so `sol resolve` can exit
cleanly before the session terminates.

---

## Task 6: Agent Bootstrap

For Loop 0, agents need to exist in the sphere DB before they can be
assigned work. Add a utility command:

```
sol agent create <name> --world=<world> [--role=outpost]
```

Creates an agent record in the sphere DB. Default role: `outpost`.
Default state: `idle`.

Add this in `cmd/agent.go`. It's a simple wrapper around
`store.CreateAgent`.

Also add `sol agent list --world=<world> [--json]` to see registered agents.

---

## Task 7: Tests

Add tests for the new packages:

### hook_test.go
- Write, Read, Clear, IsTethered
- Read when no tether exists → empty string, no error

### protocol_test.go
- GenerateClaudeMD includes all context fields
- InstallClaudeMD creates .claude/CLAUDE.md with correct content
- InstallTethers creates tether scripts with correct content

### Command integration tests
For cast, prime, and done, add focused tests in
`internal/dispatch/dispatch_test.go` (a new package) that test the
core logic without needing tmux:

```go
package dispatch

// SlingContext holds the inputs for a cast operation.
// Extract the core logic from cmd/cast.go into this package
// so it can be tested without cobra and tmux.

// The cmd/cast.go file becomes a thin wrapper that parses flags,
// creates a SlingContext, and calls dispatch.Cast(ctx).
```

Specifically, extract the dispatch logic into an `internal/dispatch/`
package that the cobra commands call. This makes the logic testable
without tmux or cobra. The dispatch package takes interfaces for
store and session manager, allowing stubs in tests.

Test cases:
1. **Cast happy path**: Mock store with open work item + idle agent →
   verify tether written, work item updated, agent updated
2. **Cast no idle agents**: Mock store with no idle agents → verify error
3. **Cast item not open**: Mock store with closed work item → verify error
4. **Prime with tether**: Write a tether file, mock store → verify output format
5. **Prime without tether**: No tether file → verify "No work tethered" output
6. **Done happy path**: Tether exists, mock store → verify tether cleared,
   item updated, agent updated

Run all tests (`make test`) and fix any issues.

---

## Task 8: Verify

After implementation:

1. `make build` succeeds
2. `make test` passes all tests
3. Manual end-to-end test (run from a git repo):

```bash
export SOL_HOME=/tmp/sol-test

# Create an agent
bin/sol agent create Toast --world=myworld

# Create a work item
ITEM=$(bin/sol store create --world=myworld --title="Add a README" --description="Create a README.md file")
echo "Created: $ITEM"

# Dispatch
bin/sol cast $ITEM myworld --agent=Toast

# Verify session is running
bin/sol session list

# Verify work item is tethered
bin/sol store get $ITEM --world=myworld

# Attach and observe (detach with Ctrl-B d)
# bin/sol session attach sol-myworld-Toast

# After agent completes (or manually):
# The agent would run "sol resolve" — for manual testing:
# SOL_WORLD=myworld SOL_AGENT=Toast bin/sol done
```

Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- Extract core dispatch logic into `internal/dispatch/` for testability.
  The cobra commands in `cmd/` are thin wrappers.
- For git operations in `sol resolve`, use `os/exec` to shell out to git.
  Don't use a git library.
- The worktree git operations in cast need a source repo. For Loop 0,
  the source repo is discovered by running `git rev-parse --show-toplevel`
  in the current directory. If not in a git repo, cast fails with a
  clear error.
- Session names are `sol-{world}-{agentName}` — predictable and greppable.
- Commit after tests pass.
