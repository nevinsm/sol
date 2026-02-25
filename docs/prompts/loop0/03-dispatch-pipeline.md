# Prompt 03: Loop 0 — Dispatch Pipeline (sling, prime, done)

You are building the dispatch pipeline for the `gt` orchestration system —
the commands that assign work to an agent, inject execution context, and
handle work completion. This is what makes the system actually do something.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompts 01 (store) and 02 (session manager) are complete.

Read the existing code first — understand the store package (work items,
agents), session manager (tmux operations), config package (GT_HOME), and
cobra setup. Build on what's there.

---

## Task 1: Hook Package

Create `internal/hook/hook.go`. The hook is the durability primitive — a
file that records which work item is assigned to an agent. It survives
session crashes, restarts, and context loss.

```go
package hook

// HookPath returns the path to an agent's hook file.
// Path: $GT_HOME/{rig}/polecats/{name}/.hook
func HookPath(rig, agentName string) string

// Read reads the hook file and returns the work item ID.
// Returns ("", nil) if no hook file exists (agent is idle).
func Read(rig, agentName string) (string, error)

// Write writes a work item ID to the hook file.
// Creates parent directories if needed.
func Write(rig, agentName, workItemID string) error

// Clear removes the hook file. No-op if it doesn't exist.
func Clear(rig, agentName string) error

// IsHooked returns true if a hook file exists for the agent.
func IsHooked(rig, agentName string) bool
```

The hook file is a plain text file containing just the work item ID
(e.g., `gt-a1b2c3d4`), no newline.

Create `internal/hook/hook_test.go` with tests for Read/Write/Clear/IsHooked,
including the "no hook" case.

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
    Rig         string
    WorkItemID  string
    Title       string
    Description string
}

// GenerateClaudeMD returns the contents of a CLAUDE.md file for a polecat
// agent. This file is the agent's entire understanding of the system.
func GenerateClaudeMD(ctx ClaudeMDContext) string

// InstallClaudeMD writes .claude/CLAUDE.md into the given worktree directory.
// Creates .claude/ if it doesn't exist.
func InstallClaudeMD(worktreeDir string, ctx ClaudeMDContext) error
```

The generated CLAUDE.md content:

```markdown
# Polecat Agent: {AgentName} (rig: {Rig})

You are a polecat agent in a multi-agent orchestration system.
Your job is to execute the assigned work item.

## Your Assignment
- Work item: {WorkItemID}
- Title: {Title}
- Description: {Description}

## Commands
- `gt done` — Signal that your work is complete. This pushes your branch,
  clears your hook, and ends your session. Only run this when you are
  confident the work is done.
- `gt escalate` — Request help if you are stuck. Describe the problem.

## Protocol
1. Read your assignment above carefully.
2. Execute the work in this worktree.
3. When finished, run `gt done`.
4. If you cannot complete the work, run `gt escalate "description of problem"`.

## Important
- You are working in an isolated git worktree. Commit your changes normally.
- Do not modify files outside this worktree.
- Do not attempt to interact with other agents directly.
```

### Claude Code Hooks

`internal/protocol/hooks.go`:

```go
package protocol

// InstallHooks writes Claude Code hook scripts into the worktree.
// Creates .claude/hooks/ directory.
//
// Hooks installed:
//   SessionStart: runs "gt prime --rig={rig} --agent={name}" and outputs
//                 the result as initial context
//
// The hook scripts are shell scripts that call gt subcommands.
func InstallHooks(worktreeDir, rig, agentName string) error
```

The SessionStart hook script (`.claude/hooks/session-start.sh`):

```bash
#!/bin/bash
# SessionStart hook — inject execution context via gt prime
exec gt prime --rig="$GT_RIG" --agent="$GT_AGENT"
```

Note: The hook should be registered in `.claude/settings.local.json`
per Claude Code's hook configuration format. Research the correct format
if needed — it typically looks like:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "type": "command",
        "command": ".claude/hooks/session-start.sh"
      }
    ]
  }
}
```

Write tests for CLAUDE.md generation (verify output contains the expected
fields) and hook installation (verify files are created with correct content).

---

## Task 3: `gt sling` Command

This is the main dispatch command. It assigns a work item to a polecat
agent and starts its session.

Create `cmd/sling.go`:

```
gt sling <work-item-id> <rig> [--agent=<name>]
```

**Sequence:**

1. **Open rig store**, get the work item. Fail if not found or not `open`.
2. **Open town store**, find the agent:
   - If `--agent` specified: use that agent. Fail if not found or not idle.
   - If not specified: find the first idle polecat for the rig.
   - If no idle agents: fail with a clear message.
3. **Create worktree directory**: `$GT_HOME/{rig}/polecats/{name}/rig/`
   - If directory already exists from a previous assignment, remove it first.
   - Initialize as a git worktree:
     `git worktree add <path> -b polecat/{name}/{work-item-id} HEAD`
   - This creates a new branch from the current HEAD of the rig's repo.
   - The rig's repo path must be configured. For Loop 0, use the current
     working directory (`$PWD`) as the source repo. (The operator runs
     `gt sling` from within the project repo.)
4. **Write hook file**: `$GT_HOME/{rig}/polecats/{name}/.hook` ← work item ID
5. **Update work item**: status → `hooked`, assignee → agent ID
6. **Update agent**: state → `working`, hook_item → work item ID
7. **Install CLAUDE.md** in the worktree
8. **Install Claude Code hooks** in the worktree
9. **Start tmux session**:
   - Name: `gt-{rig}-{agentName}` (e.g., `gt-myrig-Toast`)
   - Workdir: the worktree path
   - Cmd: `claude --dangerously-skip-permissions` (the AI agent)
   - Env: `GT_HOME`, `GT_RIG={rig}`, `GT_AGENT={name}`

**Rollback on failure:** If any step after the work item update fails
(e.g., tmux start fails), roll back: clear hook, restore work item to
`open`, restore agent to `idle`.

**Output:** On success, print:
```
Slung gt-a1b2c3d4 → Toast (gt-myrig-Toast)
  Worktree: /home/user/gt/myrig/polecats/Toast/rig
  Session:  gt-myrig-Toast
  Attach:   gt session attach gt-myrig-Toast
```

---

## Task 4: `gt prime` Command

This command assembles execution context from durable state. It's called
by the SessionStart hook when an agent session starts (or restarts).

Create `cmd/prime.go`:

```
gt prime --rig=<rig> --agent=<name>
```

**Sequence:**

1. Read the hook file for this agent. If no hook, print "No work hooked"
   and exit 0.
2. Open rig store, get the work item by ID.
3. Format and print the execution context to stdout:

```
=== WORK CONTEXT ===
Agent: {name} (rig: {rig})
Work Item: {id}
Title: {title}
Status: {status}

Description:
{description}

Instructions:
Execute this work item. When complete, run: gt done
If stuck, run: gt escalate "description"
=== END CONTEXT ===
```

This is intentionally simple for Loop 0. Later loops add workflow step
context and pending message summaries.

---

## Task 5: `gt done` Command

This command signals work completion. Called by the polecat agent when
it finishes executing a work item.

Create `cmd/done.go`:

```
gt done [--rig=<rig>] [--agent=<name>]
```

If `--rig` and `--agent` are not provided, infer them from the `GT_RIG`
and `GT_AGENT` environment variables (set by `gt sling` when starting
the session).

**Sequence:**

1. **Read hook**: Get the work item ID from the hook file. Fail if no hook.
2. **Git operations** (in the worktree):
   - `git add -A`
   - `git commit -m "gt done: {work-item-title}"` (skip if nothing to commit)
   - `git push origin HEAD` (push the branch to remote; warn but don't
     fail if push fails — the branch exists locally either way)
3. **Update work item**: status → `done`
4. **Update agent**: state → `idle`, hook_item → clear
5. **Clear hook file**
6. **Stop session**: Kill the tmux session for this agent.
   The session name is `gt-{rig}-{agentName}`.

**Output:**
```
Done: gt-a1b2c3d4 ({title})
  Branch: polecat/{name}/{work-item-id}
  Agent {name} is now idle.
```

**Important:** The `gt done` command runs inside the tmux session (the
agent calls it). After it kills its own session (step 6), the command
won't finish printing. That's OK — the important state changes (steps
1-5) happen before the session kill. Use a brief delay (1s) between the
last state update and session kill to let output flush. Alternatively,
send the session kill as a background operation so `gt done` can exit
cleanly before the session terminates.

---

## Task 6: Agent Bootstrap

For Loop 0, agents need to exist in the town DB before they can be
assigned work. Add a utility command:

```
gt agent create <name> --rig=<rig> [--role=polecat]
```

Creates an agent record in the town DB. Default role: `polecat`.
Default state: `idle`.

Add this in `cmd/agent.go`. It's a simple wrapper around
`store.CreateAgent`.

Also add `gt agent list --rig=<rig> [--json]` to see registered agents.

---

## Task 7: Tests

Add tests for the new packages:

### hook_test.go
- Write, Read, Clear, IsHooked
- Read when no hook exists → empty string, no error

### protocol_test.go
- GenerateClaudeMD includes all context fields
- InstallClaudeMD creates .claude/CLAUDE.md with correct content
- InstallHooks creates hook scripts with correct content

### Command integration tests
For sling, prime, and done, add focused tests in
`internal/dispatch/dispatch_test.go` (a new package) that test the
core logic without needing tmux:

```go
package dispatch

// SlingContext holds the inputs for a sling operation.
// Extract the core logic from cmd/sling.go into this package
// so it can be tested without cobra and tmux.

// The cmd/sling.go file becomes a thin wrapper that parses flags,
// creates a SlingContext, and calls dispatch.Sling(ctx).
```

Specifically, extract the dispatch logic into an `internal/dispatch/`
package that the cobra commands call. This makes the logic testable
without tmux or cobra. The dispatch package takes interfaces for
store and session manager, allowing stubs in tests.

Test cases:
1. **Sling happy path**: Mock store with open work item + idle agent →
   verify hook written, work item updated, agent updated
2. **Sling no idle agents**: Mock store with no idle agents → verify error
3. **Sling item not open**: Mock store with closed work item → verify error
4. **Prime with hook**: Write a hook file, mock store → verify output format
5. **Prime without hook**: No hook file → verify "No work hooked" output
6. **Done happy path**: Hook exists, mock store → verify hook cleared,
   item updated, agent updated

Run all tests (`make test`) and fix any issues.

---

## Task 8: Verify

After implementation:

1. `make build` succeeds
2. `make test` passes all tests
3. Manual end-to-end test (run from a git repo):

```bash
export GT_HOME=/tmp/gt-test

# Create an agent
bin/gt agent create Toast --rig=myrig

# Create a work item
ITEM=$(bin/gt store create --db=myrig --title="Add a README" --description="Create a README.md file")
echo "Created: $ITEM"

# Dispatch
bin/gt sling $ITEM myrig --agent=Toast

# Verify session is running
bin/gt session list

# Verify work item is hooked
bin/gt store get $ITEM --db=myrig

# Attach and observe (detach with Ctrl-B d)
# bin/gt session attach gt-myrig-Toast

# After agent completes (or manually):
# The agent would run "gt done" — for manual testing:
# GT_RIG=myrig GT_AGENT=Toast bin/gt done
```

Clean up `/tmp/gt-test` after verification.

---

## Guidelines

- Extract core dispatch logic into `internal/dispatch/` for testability.
  The cobra commands in `cmd/` are thin wrappers.
- For git operations in `gt done`, use `os/exec` to shell out to git.
  Don't use a git library.
- The worktree git operations in sling need a source repo. For Loop 0,
  the source repo is discovered by running `git rev-parse --show-toplevel`
  in the current directory. If not in a git repo, sling fails with a
  clear error.
- Session names are `gt-{rig}-{agentName}` — predictable and greppable.
- Commit after tests pass.
