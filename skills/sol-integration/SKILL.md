---
name: sol-integration
description: Interact with the sol multi-agent orchestration system as an external collaborator. Create writs, dispatch work to sol agents, check status, send messages, manage caravans.
when_to_use: When working alongside sol-managed AI agents. When you need to create work items (writs) for other agents, check system status, dispatch tasks, manage multi-step work sequences (caravans), or communicate with sol agents via mail.
license: MIT
compatibility: Requires sol CLI in PATH and an active sol sphere (SOL_HOME set)
metadata:
  author: nevinsm
  version: "1.0"
allowed-tools: Bash(sol *)
---

# Sol Integration — External Collaborator

You are working alongside a system called **sol** that coordinates AI coding agents. You are **NOT** a sol-managed agent — you are an external collaborator. Sol handles its own git workflow, merge pipeline, and agent lifecycle. Your role is to create work, dispatch it, and communicate.

## What You Can Do

### Check Status

```bash
sol status              # Sphere overview — lists worlds, agents, processes
sol status <world>      # World detail — writs, agents, forge, sentinel
sol writ list --world=<world>    # List writs in a world
sol writ status <id> --world=<world>  # Writ detail
```

### Create Work

```bash
sol writ create --world=<world> --title="Fix login bug" --description="..." --kind=code --priority=2
```

Writs are units of work. Code writs produce branches and merge requests. Analysis writs produce reports.

### Dispatch Work

```bash
sol cast <writ-id> --world=<world>
```

This assigns the writ to an available agent, creates a worktree, and starts an AI session. Sol picks the agent automatically unless you specify `--agent=<name>`.

### Manage Caravans

Caravans are batches of related writs with phase-based sequencing:

```bash
sol caravan create "API refactor" <id1> <id2> <id3> --world=<world>
sol caravan set-phase <caravan-id> <item-id> <phase>   # Assign phases (0, 1, 2...)
sol caravan commission <caravan-id>                      # Make it live
sol caravan list                                         # List all caravans
sol caravan status <caravan-id>                           # Caravan detail
```

Items in the same phase run in parallel. Phase N waits until all prior-phase items are closed (merged).

### Communicate

```bash
sol mail send --to=<agent> --subject="Question about API" --body="..."
sol mail send --to=<agent> --subject="Urgent" --body="..." --priority=1
sol escalate "Blocked on database migration"    # Escalate to the operator
```

### Check the Merge Pipeline

```bash
sol forge queue --world=<world>   # View pending merge requests
```

## What You Must NOT Do

These constraints are critical. Violating them will break sol's internal state.

- **Never use `sol resolve`** — this is for sol-managed agents only (clears tethers, pushes branches)
- **Never use `sol tether` or `sol untether`** — internal agent binding
- **Never use `sol handoff`** — internal session cycling
- **Never use `sol agent reset`** — internal agent recovery
- **Never git push to sol-managed repos** — the forge handles all merges
- **Never interact with tmux sessions directly** — sol manages agent sessions
- **Never modify files in SOL_HOME directly** — use CLI commands

## Quick Start

1. **Verify sol is available:**

   ```bash
   sol status
   ```

   If this fails, sol is not installed or the sphere is not running.

2. **Pick a world** (each world maps to a repository):

   ```bash
   sol status <world>
   ```

3. **Create a writ:**

   ```bash
   sol writ create --world=myworld --title="Add rate limiting" \
     --description="Implement token bucket rate limiting on API endpoints" \
     --kind=code --priority=2
   ```

4. **Dispatch it:**

   ```bash
   sol cast <writ-id> --world=myworld
   ```

   Sol assigns an outpost agent, creates a worktree, and starts an AI session. The agent works autonomously and resolves when done — the forge then merges the result.

## Conceptual Model

Sol organizes work in a hierarchy: a **sphere** is the top-level runtime (one per machine), containing **worlds** (each mapping to a git repository). Within a world, **writs** represent units of work, **agents** are AI coding sessions that execute writs, and the **forge** is a merge pipeline that takes completed work through quality gates and merges it.

Agents come in two flavors: **envoys** are persistent, human-directed agents; **outposts** are disposable, single-writ executors spawned by `sol cast`. When you dispatch work, sol creates an outpost, gives it a worktree, and lets it run. When the agent finishes, it resolves, the forge picks up the branch, and the work merges.

**Caravans** let you batch related writs with phase-based sequencing — useful for multi-step projects where some work must complete before other work can begin. See `references/concepts.md` for the full conceptual model.

For the complete command reference, see `references/commands.md`.
