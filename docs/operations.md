# Sol Operations Guide

This guide covers day-to-day operation of a running sol instance. It assumes you have completed the README quick start and have at least one world initialized.

---

## Table of Contents

1. [Starting supervision](#starting-supervision)
2. [Reading sol status](#reading-sol-status)
3. [Managing worlds](#managing-worlds)
4. [Dispatching work](#dispatching-work)
5. [Envoys vs outposts](#envoys-vs-outposts)
6. [Configuring the forge](#configuring-the-forge)
7. [Monitoring health](#monitoring-health)
8. [Cost tracking](#cost-tracking)

---

## Starting supervision

### The prefect: sphere-wide supervisor

The prefect is the primary supervision process. It monitors all agent sessions across all worlds and restarts anything that crashes.

**Start the prefect (foreground):**
```sh
sol prefect run --consul
```

`--consul` enables the consul patrol process, which handles stale tether recovery and caravan feeding. For production use, always pass `--consul`.

The prefect runs a heartbeat every 3 minutes and:
- Detects dead agent sessions (tmux sessions that exited) and respawns them
- Ensures forge and sentinel are running for every non-sleeping world
- Keeps sphere daemons alive: ledger, broker, chronicle
- When `--consul` is set: starts and monitors the consul

**Start the prefect as a background daemon:**
```sh
sol prefect start           # start in background
sol prefect stop            # send SIGTERM
sol prefect restart         # stop then start
sol prefect status          # check if running
```

**What happens when you stop the prefect:**

On graceful shutdown (SIGTERM/SIGINT), the prefect stops all working and stalled agent sessions, stops consul (if enabled), stops forge and sentinel for each world, and stops ledger, broker, and chronicle. All agents are marked stalled in the database — the consul will recover their writs on the next patrol after restart.

Envoys and governors are human-supervised and are **not** stopped by prefect shutdown.

**Logs:** `$SOL_HOME/.runtime/prefect.log`

### Individual component control

You can start and stop per-world services directly without the prefect:

```sh
sol forge start --world=myworld
sol forge stop --world=myworld

sol sentinel start --world=myworld
sol sentinel stop --world=myworld
```

Both commands are idempotent — starting an already-running service prints a message and exits 0. These are the same commands the prefect uses internally when it detects a service is missing.

---

## Reading sol status

```sh
sol status              # sphere overview
sol status myworld      # per-world detail
```

### Sphere overview

The sphere overview shows every world at a glance:

- **Prefect**: running or stopped
- **Consul**: running, heartbeat age, patrol count
- **Chronicle / Ledger / Broker**: sphere-wide daemons, running status and heartbeat age
- **Worlds table**: per-world summary with agent counts, forge and sentinel status, merge request queue, and overall health
- **Token usage**: 24-hour rolling totals

### Per-world detail

`sol status myworld` shows full detail for one world:

**Agents section** — one row per outpost agent:
- `idle` — registered but no active writ
- `working` — has a writ, session alive
- `working (dead)` — has a writ but the tmux session has exited; prefect will respawn
- `stalled` — prefect exceeded max respawn attempts (5 by default); consul will recover the writ after 15 minutes

**Forge section** — merge pipeline state:
- Running/stopped and PID
- Queue depth: `ready` (waiting to merge), `claimed` (currently merging), `failed`, `merged`
- Heartbeat age — how recently the forge wrote a heartbeat; `stale` if >5 minutes
- Current MR being processed (if any)
- Paused flag

**Sentinel section** — health monitor state:
- Running/stopped and PID
- Patrol count, agents checked, stalled and reaped counts
- Heartbeat age; `stale` if >15 minutes

**Merge requests section** — individual MR details with writ title and phase

**Caravans section** — batch work progress: total/ready/dispatched/done/closed items per phase

**Tokens section** — 24-hour rolling input, output, and cache tokens with agent count

### Health levels

The world health field shows:
- `healthy` — all agent sessions alive, no failed merge requests
- `unhealthy` — at least one dead session or a failed MR needs attention
- `degraded` — prefect is not running; crashed sessions cannot be respawned

---

## Managing worlds

### Creating additional worlds

```sh
sol world init myworld --source-repo=/path/to/repo
```

This creates:
- `$SOL_HOME/myworld/` directory with `outposts/` subdirectory
- `$SOL_HOME/myworld/world.toml` with default configuration
- A managed repo clone at `$SOL_HOME/myworld/repo/`
- The world database registered in `sphere.db`

Each world is independent — separate agent pool, separate merge queue, separate database.

### World sync

When the upstream repo receives new commits, you need to pull them into the managed repo so agent worktrees can rebase on top of them:

```sh
sol world sync myworld
```

This fetches from origin and resets the managed repo to the target branch. It also syncs the forge worktree. Run this periodically or after upstream merges.

To sync all worlds at once:
```sh
sol world sync --all
```

### Sleeping worlds

Set `sleeping = true` in `$SOL_HOME/myworld/world.toml` to suspend a world without deleting it:

```toml
[world]
sleeping = true
```

When a world is sleeping:
- The prefect skips it during heartbeat — it will not respawn crashed agents or restart forge/sentinel
- The consul skips dispatching caravan items to sleeping worlds
- `sol forge start` refuses to start for a sleeping world

This is useful for temporarily pausing work on a world without losing its state. To wake it, set `sleeping = false` and restart the prefect, or manually run `sol forge start` and `sol sentinel start`.

### Deleting a world

```sh
sol world delete myworld --confirm
```

Without `--confirm`, the command previews what would be deleted and exits 1. Pass `--confirm` to proceed. Use `--force` to stop active sessions before deleting (otherwise the command refuses if sessions are running).

This removes the world directory, its database, worktrees, and the sphere registration. It cannot be undone.

---

## Dispatching work

### Single writs

Create a writ, then dispatch it to an agent:

```sh
# Create a writ
sol writ create --world=myworld --title="Fix the login bug" --kind=code

# Dispatch to an idle agent (auto-selects the agent)
sol cast --world=myworld --writ=<writ-id>
```

`sol cast` finds an idle agent, creates a git worktree, writes the agent's context, and starts a tmux session. When the agent calls `sol resolve`, the worktree is submitted as a merge request.

You can create writs with a description file:
```sh
sol writ create --world=myworld --title="Refactor auth module" --kind=code --description=task.md
```

### Batch work with caravans

A caravan is a named batch of related writs, potentially spanning multiple worlds, dispatched automatically by the consul.

```sh
# Create a caravan (positional name, not --name flag)
sol caravan create "Q4 cleanup"

# Create writs first
sol writ create --world=myworld --title="Remove deprecated API" --kind=code
sol writ create --world=myworld --title="Update docs" --kind=code
sol writ create --world=otherworld --title="Sync schema" --kind=code

# Add writs to caravan by ID (command is "add", not "add-item")
sol caravan add <caravan-id> <writ-id> [<writ-id> ...] --phase=0

# Commission — lock the caravan and make items available for dispatch
sol caravan commission <caravan-id>
```

Once commissioned, the consul's next patrol detects the ready items and dispatches them to available agents. You don't need to call `sol cast` manually.

**Phase-based sequencing:** Caravan items have a phase number (default 0). The consul only dispatches phase N+1 items after all phase N items are closed (merged). This lets you sequence dependent work:

```sh
# Create writs
sol writ create --world=myworld --title="Step 1: migrate DB" --kind=code
sol writ create --world=myworld --title="Step 2: update app" --kind=code

# Add with phase assignment
sol caravan add <caravan-id> <step1-writ-id> --phase=0
sol caravan add <caravan-id> <step2-writ-id> --phase=1
```

### Workflows

For multi-step work where a single agent follows a structured sequence of steps, see [docs/workflows.md](workflows.md).

---

## Envoys vs outposts

Sol has two agent roles for human-directed work.

### Outposts

Outposts are **ephemeral worker agents**. Each outpost:
- Works on exactly one writ at a time
- Lives in an isolated git worktree created for that writ
- Ends its session when it calls `sol resolve` or `sol escalate`
- Is respawned by the prefect if it crashes (up to 5 times)
- Has its writ recovered by the consul if it stays stalled for 15+ minutes

Use outposts for discrete, bounded coding tasks where the scope is well-defined in a writ description.

### Envoys

Envoys are **persistent, human-directed agents**. Each envoy:
- Lives in a dedicated, persistent worktree (not per-writ)
- Maintains a brief (`memory.md`) that persists across sessions
- Can hold multiple tethered writs
- Is **not** automatically respawned by the prefect — envoys are under human supervision
- Is not stopped by `prefect stop`

Use envoys for ongoing collaborative work, research, or tasks that need continuity across multiple sessions. An envoy is a long-lived partner; an outpost is a contractor hired for one job.

**Starting an envoy session:**
```sh
sol envoy start MyEnvoy --world=myworld
```

---

## Configuring the forge

The forge is the merge pipeline. It runs as a background process, polls for ready merge requests, runs quality gates, and merges passing branches.

### Quality gates

Quality gates are commands the forge runs against each agent's branch before merging. All gates must exit 0 for the merge to proceed.

Configure them in `$SOL_HOME/myworld/world.toml`:

```toml
[forge]
quality_gates = [
  "make build",
  "make test",
]
```

Gates run in the forge's worktree with the agent's branch checked out. If any gate fails, the MR is marked `failed` and the writ is re-opened for re-dispatch (up to the max attempt limit).

The default gate if none are configured is `go test ./...`.

### Gate timeout

Each gate has a maximum execution time. If a gate exceeds it, the forge treats it as a failure:

```toml
[forge]
gate_timeout = "10m"    # default: 5m
```

If the timeout is exceeded, the forge releases the MR back to `ready` (or marks it `failed` if max attempts are exhausted) and moves on.

### Pausing and resuming the forge

Pause the forge to stop it from claiming new merge requests without stopping the process:

```sh
sol forge pause --world=myworld
sol forge resume --world=myworld
```

A paused forge stays running and writes heartbeats — it just won't claim new MRs. Useful when you need to push urgent manual changes to the target branch.

### Manual forge operations

When things go wrong, you can intervene directly:

```sh
# View the merge queue
sol forge queue --world=myworld

# View only ready (unblocked) MRs
sol forge ready --world=myworld

# Mark an MR as successfully merged (e.g., you merged it manually)
sol forge mark-merged <mr-id> --world=myworld

# Mark an MR as permanently failed (removes it from retry cycle)
sol forge mark-failed <mr-id> --world=myworld

# Watch forge output in real time
sol forge log --world=myworld --follow
```

For full configuration reference, see [docs/configuration.md](configuration.md).

---

## Monitoring health

### Heartbeat files

All sol processes write periodic heartbeat files — JSON files that record the process's last-known state. The prefect uses these to detect hung processes (process alive but not making progress) and restart them.

| Component | Heartbeat path | Stale after |
|-----------|----------------|-------------|
| Forge | `$SOL_HOME/{world}/forge/heartbeat.json` | 5 minutes |
| Sentinel | `$SOL_HOME/{world}/sentinel.heartbeat` | 15 minutes |
| Consul | `$SOL_HOME/consul/heartbeat.json` | 15 minutes |
| Ledger | `$SOL_HOME/.runtime/ledger.heartbeat` | 5 minutes |
| Broker | `$SOL_HOME/.runtime/broker.heartbeat` | 5 minutes |
| Chronicle | `$SOL_HOME/.runtime/chronicle.heartbeat` | 5 minutes |

Heartbeat files are JSON. You can read them directly to see what a process last reported:
```sh
cat $SOL_HOME/myworld/sentinel.heartbeat
```

### Sentinel

The sentinel patrols its world every 3 minutes. Each patrol it:
1. Lists all working and stalled agents
2. Checks their tmux sessions
3. For agents whose sessions have been dead longer than the stall detection threshold, captures recent tmux output and submits it for AI assessment
4. Based on the assessment: does nothing (still progressing), nudges the agent, or escalates
5. Reaps idle agents that have been idle longer than the idle reap timeout (default 10 minutes)
6. Releases stale merge request claims older than 30 minutes
7. Writes a heartbeat

The sentinel's heartbeat records: patrol count, agents checked, stalled agents found, and reaped agents. You can see this in `sol status myworld` under the Sentinel section.

The sentinel attempts up to 2 respawns per writ before escalating. The prefect allows up to 5 consecutive respawn attempts before permanently stalling an agent.

When the sentinel marks an agent as stuck, it creates an escalation visible in `sol status`. You can view open escalations with `sol escalation list`.

### Consul

The consul patrols sphere-wide every 5 minutes. Each patrol it:

1. **Recovers stale tethers** — finds agents in `working` or `stalled` state whose sessions are gone and whose `updated_at` is older than 15 minutes. Reopens the writ and sets the agent idle. This is the backstop for agents that crash beyond the prefect's respawn limit.

2. **Feeds stranded caravans** — finds open caravans with ready, undispatched items and dispatches them. This handles cases where an item becomes ready (its phase-0 dependencies closed) after the caravan was commissioned.

3. **Detects orphaned sessions** — finds tmux sessions matching `sol-*` that have no corresponding agent record. Sessions must be seen as orphaned across 2 consecutive patrols and be at least 30 minutes old before the consul stops them. This prevents a race where a session is being set up.

The consul's heartbeat at `$SOL_HOME/consul/heartbeat.json` records: patrol count, stale tethers recovered, caravan items dispatched, and open escalation count.

For troubleshooting degraded state, see [docs/troubleshooting.md](troubleshooting.md).

---

## Cost tracking

### sol cost

`sol cost` shows token usage and estimated spend:

```sh
sol cost                          # sphere-wide totals per world
sol cost --world=myworld          # per-agent breakdown within a world
sol cost --agent=Toast --world=myworld   # per-writ breakdown for an agent
sol cost --writ=sol-abc123 --world=myworld  # per-model breakdown for a writ
sol cost --caravan=my-caravan     # per-writ breakdown across worlds
sol cost --since=7d               # filter to last 7 days
sol cost --since=2026-01-01       # filter to since a date
```

Token data is collected by the ledger (an OTLP receiver) and stored in the world database. Costs are computed from the pricing configuration in `sol.toml`.

Without pricing configuration, `sol cost` shows raw token counts only. Add pricing to see dollar estimates.

### Pricing configuration

In `$SOL_HOME/sol.toml`:

```toml
[pricing]
"claude-sonnet-4-5" = { input = 3.00, output = 15.00, cache_read = 0.30, cache_creation = 3.75 }
"claude-opus-4-5"   = { input = 15.00, output = 75.00, cache_read = 1.50, cache_creation = 18.75 }
```

Prices are in USD per million tokens. Model names must match exactly what Claude Code reports in its telemetry.

If `sol cost` shows `unpriced` for a model, it means that model name is not in your `[pricing]` table. The output will tell you which models are unpriced.

For full pricing configuration reference, see [docs/configuration.md](configuration.md).
