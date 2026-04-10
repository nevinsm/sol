# CLI Reference

Auto-generated from the Cobra command tree. Do not edit manually.

Run `sol docs generate` to regenerate this file.

---

## Dispatch:

### `sol cast`

Assign a writ to an agent and start its session

Dispatch a writ to an outpost agent: create a worktree, tether the writ,
and launch a Claude session.

Selects an idle agent automatically unless --agent is specified. Respects
world max_active limits and dispatch gates (sleeping worlds are rejected).

With --guidelines, selects a specific guidelines template for the agent.
Without it, the template is auto-selected by writ kind (code→default,
analysis→analysis) with optional world.toml overrides. Variables can be
passed with --var key=val. With --account, uses specific Claude OAuth
credentials instead of the world's default_account.

**Usage:** `sol cast <writ-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--account` | string | "" | account to use for credentials (overrides world.toml default_account) |
| `--agent` | string | "" | agent name (auto-selects idle agent if omitted) |
| `--guidelines` | string | "" | guidelines template name (auto-selected by writ kind if omitted) |
| `--var` | stringSlice | [] | template variable (key=val, repeatable) |
| `--world` | string | "" | world name |

### `sol cost`

Show token usage and cost across worlds

Show token usage and estimated cost.

Without flags, shows sphere-wide per-world cost totals (no world detection
is applied — sphere-wide is the explicit default).
With --world, shows per-agent breakdown within a world.
With --agent, shows per-writ breakdown for an agent in a world.
With --writ, shows per-model breakdown for a specific writ.
With --caravan, shows per-writ breakdown across worlds for a caravan.
With --since, filters by time window (relative duration or absolute date).

For the --world, --agent, and --writ branches, the world is resolved using
the standard precedence: explicit --world flag > SOL_WORLD env var > cwd
detection (when run from inside a world directory under $SOL_HOME).

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | show per-writ breakdown for an agent (requires --world) |
| `--caravan` | string | "" | show per-writ breakdown for a caravan (ID or name) |
| `--json` | bool | false | output as JSON |
| `--since` | string | "" | time window: relative duration (24h) or absolute date (2006-01-02) |
| `--world` | string | "" | world name |
| `--writ` | string | "" | show per-model breakdown for a writ (requires --world) |

### `sol dash`

Live TUI dashboard

Launch a live terminal dashboard.

Without arguments, auto-detects the current world from SOL_WORLD or the
working directory. Falls back to the sphere-level overview if no world
is detected. With a world name, shows detailed status for that world.

The dashboard refreshes every 3 seconds. Press r to force refresh.

**Usage:** `sol dash [world]`

### `sol handoff`

Hand off to a fresh session with context preservation

Stop the current agent session and start a new one for the same writ.

The agent's tether, worktree, and writ assignment are preserved. Committed
code and the git history carry over as the primary context for the successor
session. Use --summary to pass additional context.

Common reasons: context exhaustion (compact), autarch-initiated (manual),
or health-check triggered restart. Uses SOL_WORLD and SOL_AGENT environment
variables when flags are not provided.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--reason` | string | "" | handoff reason (compact, manual, health-check) |
| `--summary` | string | "" | summary of current progress |
| `--world` | string | "" | world name |

### `sol resolve`

Signal work completion — code writs push branch and create MR; non-code writs close directly

Mark the current writ as done and clean up the agent's tether.

For code writs: pushes the worktree branch, creates a merge request in the
forge queue, and sets the writ to "done" (awaiting merge).

For non-code writs: closes the writ directly with no branch push.

In both cases, clears the agent's tether and returns it to idle (unless the
session is configured to stay alive for further dispatch).

Typically called from within an agent session. Uses SOL_WORLD and SOL_AGENT
environment variables when --world and --agent are not provided.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--world` | string | "" | world name |

### `sol status`

Show sphere or world status

Show system status.

Without arguments, auto-detects world from cwd (or SOL_WORLD).
If a world is detected, shows sphere processes plus world detail combined.
Otherwise, shows a sphere-level overview of all worlds and processes.
With a world name, shows detailed status for that specific world.

Exit codes:
  Sphere-only (no world detected or specified): always exits 0
  World or combined (world detected from cwd or explicitly specified):
    0 = healthy
    1 = unhealthy
    2 = degraded

**Usage:** `sol status [world]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

---

## Writs:

### `sol caravan`

Manage caravans (grouped writ batches)

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol caravan add` | Add items to an existing caravan |
| `sol caravan check` | Check readiness of caravan items |
| `sol caravan close` | Close a completed caravan |
| `sol caravan commission` | Commission a caravan (drydock → open) |
| `sol caravan create` | Create a caravan with optional initial items |
| `sol caravan delete` | Delete a drydocked or closed caravan entirely |
| `sol caravan dep` | Manage caravan-level dependencies |
| `sol caravan drydock` | Return a caravan to drydock (open → drydock) |
| `sol caravan launch` | Dispatch ready items in a caravan |
| `sol caravan list` | List caravans with optional status filtering |
| `sol caravan remove` | Remove an item from a caravan |
| `sol caravan reopen` | Reopen a closed caravan (closed → drydock) |
| `sol caravan set-phase` | Update the phase of items in a caravan |
| `sol caravan status` | Show caravan status |

#### `sol caravan add`

**Usage:** `sol caravan add <caravan-id> <item-id> [<item-id> ...]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--phase` | int | 0 | phase for items (default 0) |
| `--world` | string | "" | world name |

#### `sol caravan check`

**Usage:** `sol caravan check <caravan-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol caravan close`

Close a caravan by ID, or use --auto to close all caravans where every item is merged.

Requires --confirm to proceed; without it, prints a preview of the caravan and exits.
Use --force to close even if not all items are merged (requires --confirm).

**Usage:** `sol caravan close [<caravan-id>]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--auto` | bool | false | scan all open caravans and close any where all items are merged |
| `--confirm` | bool | false | confirm closure |
| `--force` | bool | false | close even if not all items are merged |

#### `sol caravan commission`

**Usage:** `sol caravan commission <caravan-id>`

#### `sol caravan create`

**Usage:** `sol caravan create <name> [<item-id> ...]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--owner` | string | "" | caravan owner (default: autarch) |
| `--phase` | int | 0 | phase for items (default 0) |
| `--world` | string | "" | world name |

#### `sol caravan delete`

Delete a drydocked or closed caravan entirely.

Requires --confirm to proceed; without it, prints what would be deleted and exits.

**Usage:** `sol caravan delete <caravan-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | confirm deletion (without this flag, prints what would be deleted) |

#### `sol caravan dep`

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol caravan dep add` | Declare that a caravan depends on another caravan being closed |
| `sol caravan dep list` | Show caravan-level dependencies |
| `sol caravan dep remove` | Remove a caravan dependency |

##### `sol caravan dep add`

**Usage:** `sol caravan dep add <caravan-id> <depends-on-caravan-id>`

##### `sol caravan dep list`

**Usage:** `sol caravan dep list <caravan-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

##### `sol caravan dep remove`

**Usage:** `sol caravan dep remove <caravan-id> <depends-on-caravan-id>`

#### `sol caravan drydock`

**Usage:** `sol caravan drydock <caravan-id>`

#### `sol caravan launch`

Check readiness of all items in the caravan and dispatch those that are
ready (open, unblocked) in the specified world. Items blocked by dependencies
or in earlier phases are skipped.

Drydock caravans must be commissioned first. Auto-closes the caravan if all
items complete after dispatch. Use --guidelines to select a specific guidelines
template for dispatched writs.

**Usage:** `sol caravan launch <caravan-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--guidelines` | string | "" | guidelines template for dispatched items |
| `--var` | stringSlice | [] | variable assignment (key=val) |
| `--world` | string | "" | world name |

#### `sol caravan list`

List all caravans. Shows active (non-closed) caravans by default. Use --all for all caravans or --status to filter.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | include closed caravans |
| `--json` | bool | false | output as JSON |
| `--status` | string | "" | filter by status (open, ready, closed) |

#### `sol caravan remove`

**Usage:** `sol caravan remove <caravan-id> <item-id>`

#### `sol caravan reopen`

Move a closed caravan back to drydock status for modification. Only closed
caravans can be reopened. After reopening, commission the caravan to make it
dispatchable again.

**Usage:** `sol caravan reopen <caravan-id>`

#### `sol caravan set-phase`

Update the phase of a single item, or use --all to update all items in the caravan.

**Usage:** `sol caravan set-phase <caravan-id> [<item-id>] <phase>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | update all items in the caravan |

#### `sol caravan status`

**Usage:** `sol caravan status [<caravan-id>]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol workflow`

Manage workflows

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol workflow init` | Scaffold a new workflow |
| `sol workflow list` | List available workflows |
| `sol workflow manifest` | Manifest a workflow into writs and a caravan |
| `sol workflow show` | Display workflow details and resolution source |

#### `sol workflow init`

**Usage:** `sol workflow init <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | bool | false | create in project tier (.sol/workflows/) |
| `--type` | string | workflow | workflow type |
| `--world` | string | "" | world name |

#### `sol workflow list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | show all tiers including shadowed workflows |
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol workflow manifest`

**Usage:** `sol workflow manifest <workflow>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--target` | string | "" | existing writ ID to manifest against |
| `--var` | stringSlice | [] | variable assignment (key=val) |
| `--world` | string | "" | world name |

#### `sol workflow show`

**Usage:** `sol workflow show [workflow]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--path` | string | "" | load workflow from directory path instead of by name |
| `--world` | string | "" | world name |

### `sol writ`

Manage writs

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol writ activate` | Switch active writ for a persistent agent |
| `sol writ clean` | Clean writ output directories |
| `sol writ close` | Close a writ |
| `sol writ create` | Create a writ |
| `sol writ dep` | Manage writ dependencies |
| `sol writ list` | List writs |
| `sol writ query` | Run a read-only SQL query |
| `sol writ ready` | List writs ready for dispatch |
| `sol writ status` | Show writ status |
| `sol writ trace` | Show full trace of a writ |
| `sol writ update` | Update a writ |

#### `sol writ activate`

Switch the active writ with lightweight session handoff. The writ must be tethered to the agent. If the writ is already active, this is a no-op.

**Usage:** `sol writ activate <writ-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--world` | string | "" | world name |

#### `sol writ clean`

Delete output directories for closed writs past the retention threshold.

Requires --confirm to proceed; without it, lists candidates and exits.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | confirm the destructive operation |
| `--older-than` | string | "" | retention threshold (e.g., 7d, 15d, 30d) |
| `--world` | string | "" | world name |

#### `sol writ close`

Close a writ permanently. Supersedes any failed merge requests linked to
the writ and auto-resolves linked escalations.

Use --reason to record why the writ was closed (e.g. completed, superseded,
cancelled). This is a terminal state — closed writs cannot be reopened.

Requires --confirm to proceed; without it, prints what would be closed and exits.

**Usage:** `sol writ close <id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | confirm the destructive operation |
| `--reason` | string | "" | close reason (e.g. completed, superseded, cancelled) |
| `--world` | string | "" | world name |

#### `sol writ create`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--description` | string | "" | writ description |
| `--kind` | string | "" | writ kind (default: code) |
| `--label` | stringArray | [] | label (can be repeated) |
| `--metadata` | string | "" | metadata as JSON object |
| `--priority` | int | 2 | priority (1=high, 2=normal, 3=low) |
| `--title` | string | "" | writ title |
| `--world` | string | "" | world name |

#### `sol writ dep`

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol writ dep add` | Add a dependency (from depends on to) |
| `sol writ dep list` | List dependencies for a writ |
| `sol writ dep remove` | Remove a dependency |

##### `sol writ dep add`

**Usage:** `sol writ dep add <from-id> <to-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

##### `sol writ dep list`

**Usage:** `sol writ dep list <item-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

##### `sol writ dep remove`

**Usage:** `sol writ dep remove <from-id> <to-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol writ list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | show all writs including closed |
| `--assignee` | string | "" | filter by assignee |
| `--json` | bool | false | output as JSON |
| `--label` | string | "" | filter by label |
| `--status` | string | "" | filter by status |
| `--world` | string | "" | world name |

#### `sol writ query`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--sql` | string | "" | SQL SELECT query |
| `--world` | string | "" | world name |

#### `sol writ ready`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol writ status`

**Usage:** `sol writ status <id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol writ trace`

Shows unified timeline, cost, and escalation data for a writ, aggregating data from world DB, sphere DB, tether files, and event logs.

**Usage:** `sol writ trace <id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--cost` | bool | false | show cost only |
| `--json` | bool | false | machine-readable JSON output |
| `--no-events` | bool | false | skip event log scan (faster) |
| `--timeline` | bool | false | show timeline only |
| `--world` | string | "" | world name |

#### `sol writ update`

**Usage:** `sol writ update <id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--assignee` | string | "" | new assignee (- to clear) |
| `--description` | string | "" | new description |
| `--priority` | int | 0 | new priority |
| `--status` | string | "" | new status |
| `--title` | string | "" | new title |
| `--world` | string | "" | world name |

---

## Agents & Sessions:

### `sol agent`

Manage agents

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol agent create` | Create an agent |
| `sol agent handoffs` | Show recent handoff events |
| `sol agent history` | Show agent work trail |
| `sol agent list` | List agents |
| `sol agent postmortem` | Show diagnostic information for a dead or stuck agent |
| `sol agent reset` | Reset a stuck agent to idle state |
| `sol agent stats` | Show agent performance metrics |

#### `sol agent create`

**Usage:** `sol agent create <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--role` | string | outpost | agent role |
| `--world` | string | "" | world name |

#### `sol agent handoffs`

Show recent handoff events for agents in a world.

Without a name argument, lists handoffs for all agents in the world.
Passing a name filters handoffs to just that agent:

    sol agent handoffs Polaris --world=sol-dev
    sol agent handoffs --world=sol-dev

The --world flag is optional: if omitted, sol auto-detects the world from
the current directory (when inside a sol-managed worktree or world tree).

The --agent flag is deprecated; pass the agent name as a positional
argument instead.

**Usage:** `sol agent handoffs [name]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--last` | int | 20 | number of recent events to show |
| `--world` | string | "" | world name (auto-detected from current directory if omitted) |

#### `sol agent history`

Show the work trail for an agent — writs, cast/resolve times, cycle duration, and token usage.

Without a name argument, shows all agent activity in the world.

**Usage:** `sol agent history [name]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol agent list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | list agents across all worlds (overrides --world and cwd detection) |
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name (defaults to $SOL_WORLD or detected from current worktree) |

#### `sol agent postmortem`

Gathers session metadata, commit history, writ state, and last output for an agent — particularly useful for understanding what happened when an outpost dies mid-work.

**Usage:** `sol agent postmortem <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--commits` | int | 10 | number of recent commits to show |
| `--json` | bool | false | output as JSON |
| `--lines` | int | 50 | lines of session output to capture |
| `--world` | string | "" | world name |

#### `sol agent reset`

Force an agent back to idle when it's stuck in a bad state.

Clears the agent's tether file and sets the agent state to idle. If the
agent's active writ is in a non-terminal state (open/tethered/working/
resolve), it is returned to "open" with its assignee cleared. If the writ
is already in a terminal state (done/closed), it is left untouched — its
status and assignee are part of the historical record. Warns if the
agent's tmux session is still running — consider stopping it first to
avoid conflicting state.

Requires --confirm to proceed; without it, previews what would be reset and exits 1.

**Usage:** `sol agent reset <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | confirm the destructive operation |
| `--world` | string | "" | world name |

#### `sol agent stats`

Shows performance summary for a single agent, or a leaderboard across all
agents when no name is given.

The --world flag may be omitted: when unset, sol auto-detects the active
world from the current working directory (see ADR-0039). If no world can
be resolved, the command exits with an error.

When no agents have any recorded activity, the leaderboard still renders
an empty table (header row only) followed by a "0 agents" footer.

**Usage:** `sol agent stats [name]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name (auto-detected from current directory if omitted) |

### `sol envoy`

Manage persistent envoy agents

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol envoy attach` | Attach to an envoy's tmux session |
| `sol envoy create` | Create an envoy agent |
| `sol envoy delete` | Delete an envoy agent and all associated resources |
| `sol envoy list` | List envoy agents |
| `sol envoy restart` | Restart an envoy session (stop then start) |
| `sol envoy start` | Start an envoy session |
| `sol envoy status` | Show envoy status |
| `sol envoy stop` | Stop an envoy session |
| `sol envoy sync` | Sync managed repo and notify a running envoy session |

#### `sol envoy attach`

**Usage:** `sol envoy attach <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol envoy create`

**Usage:** `sol envoy create <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--persona` | string | "" | persona template name (e.g. planner, engineer) |
| `--world` | string | "" | world name |

#### `sol envoy delete`

Remove an envoy agent, its worktree, memory history, and agent record.

Requires --confirm to proceed; without it, prints what would be deleted and exits.

Refuses to delete if the envoy's session is active or tethered unless --force
is specified. With --force, stops the session and clears the tether before
deleting. Both flags may be needed together: sol envoy delete --confirm --force.

**Usage:** `sol envoy delete <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | confirm destructive action |
| `--force` | bool | false | force delete even if session is active or tethered |
| `--world` | string | "" | world name |

#### `sol envoy list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol envoy restart`

**Usage:** `sol envoy restart <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol envoy start`

**Usage:** `sol envoy start <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol envoy status`

Show envoy session and agent state.

Prints session status, agent state, and active writ.
Use --json for machine-readable output.

Exit codes:
  0 - Envoy session is running
  1 - Envoy session is not running

**Usage:** `sol envoy status <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol envoy stop`

**Usage:** `sol envoy stop <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol envoy sync`

**Usage:** `sol envoy sync <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

### `sol session`

Manage tmux sessions for agents

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol session attach` | Attach to a tmux session |
| `sol session capture` | Capture pane output |
| `sol session health` | Check session health |
| `sol session inject` | Inject text into a session |
| `sol session list` | List all sessions |
| `sol session start` | Start a tmux session |
| `sol session stop` | Stop a tmux session |

#### `sol session attach`

**Usage:** `sol session attach <name>`

#### `sol session capture`

**Usage:** `sol session capture <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--lines` | int | 50 | number of lines to capture |

#### `sol session health`

Check session health and report status via exit code.

Exit codes:
  0  healthy    — session alive with recent activity
  1  dead       — tmux session does not exist
  2  degraded   — session exists but agent process exited or no output change within --max-inactivity

**Usage:** `sol session health <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--max-inactivity` | duration | 30m0s | max inactivity before reporting hung |

#### `sol session inject`

**Usage:** `sol session inject <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--message` | string | "" | text to inject |
| `--no-submit` | bool | false | stage text without pressing Enter |

#### `sol session list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol session start`

**Usage:** `sol session start <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--cmd` | string | "" | command to run |
| `--env` | stringArray | [] | environment variable KEY=VAL (can be repeated) |
| `--role` | string | outpost | session role |
| `--workdir` | string | . | working directory |
| `--world` | string | "" | world name |

#### `sol session stop`

**Usage:** `sol session stop <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | false | force kill without graceful shutdown |

### `sol tether`

Bind a writ to a persistent agent (envoy, forge)

Bind a writ to a persistent agent without creating a worktree or launching a session.
Outpost agents must use sol cast instead.

**Usage:** `sol tether <writ-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (required) |
| `--world` | string | "" | world name |

### `sol untether`

Unbind a writ from a persistent agent

Unbind a specific writ from an agent without stopping the session.
If no tethers remain, the agent goes idle.

**Usage:** `sol untether <writ-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (required) |
| `--world` | string | "" | world name |

---

## Processes:

### `sol broker`

Manage AI provider health probing

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol broker restart` | Restart the broker (stop then start) |
| `sol broker run` | Run the broker loop (foreground) |
| `sol broker start` | Start the broker as a background process |
| `sol broker status` | Show broker status from heartbeat |
| `sol broker stop` | Stop the running broker |

#### `sol broker run`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--interval` | string | 5m | patrol interval |

#### `sol broker status`

Show whether the broker process is running via its heartbeat file.

Prints patrol count and provider health state.
Use --json for machine-readable output.

Exit codes:
  0 - Broker is running
  1 - Broker is not running

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol chronicle`

Manage the event feed chronicle

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol chronicle restart` | Restart the chronicle (stop then start) |
| `sol chronicle run` | Run the chronicle (foreground) |
| `sol chronicle start` | Start the chronicle as a background process |
| `sol chronicle status` | Show chronicle status |
| `sol chronicle stop` | Stop the chronicle background process |

#### `sol chronicle status`

Show whether the chronicle process is running.

Prints PID, heartbeat metrics, and checkpoint offset. Use --json for machine-readable output.

Exit codes:
  0 - Chronicle is running
  1 - Chronicle is not running

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol consul`

Manage the sphere-level consul patrol process

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol consul restart` | Restart the consul (stop then start) |
| `sol consul run` | Run the consul patrol loop (foreground) |
| `sol consul start` | Start the consul as a background process |
| `sol consul status` | Show consul status from heartbeat |
| `sol consul stop` | Stop the consul background process |

#### `sol consul run`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--interval` | string | 5m | patrol interval |
| `--stale-timeout` | string | 1h | stale tether timeout |
| `--webhook` | string | "" | escalation webhook URL |

#### `sol consul status`

Show consul status from its heartbeat file.

Prints patrol count, stale tethers, caravan feeds, and escalation counts.
Use --json for machine-readable output.

Exit codes:
  0 - Consul is running and heartbeat is fresh
  1 - Consul is not running (no heartbeat file) or an I/O error occurred
  2 - Consul is wedged: heartbeat is stale, or the recorded PID is gone
      while the state still claims running (degraded/stuck case)

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol down`

Stop sphere daemons and world services

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | also stop envoy sessions |
| `--world` | string | "" | stop only world services (optionally for a specific world) |

### `sol forge`

Manage the merge pipeline forge

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol forge attach` | Attach to the forge merge session (if active) |
| `sol forge await` | Block until a nudge arrives or timeout expires |
| `sol forge log` | Show the forge log file |
| `sol forge pause` | Pause the forge — stop claiming new MRs |
| `sol forge queue` | Show the merge request queue |
| `sol forge restart` | Restart the forge (stop then start) |
| `sol forge resume` | Resume the forge — start claiming MRs again |
| `sol forge start` | Start the forge as a background process |
| `sol forge status` | Show forge health summary |
| `sol forge stop` | Stop the forge |
| `sol forge sync` | Sync forge worktree: fetch origin, reset to target branch |

#### `sol forge attach`

Attach to the ephemeral forge merge session (sol-{world}-forge-merge).

The forge process itself runs as a direct background process (not in tmux).
Use 'sol forge log --follow' to watch forge output. This command attaches
to the merge session, which only exists while a merge is in progress.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge await`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--timeout` | int | 120 | max seconds to wait |
| `--world` | string | "" | world name |

#### `sol forge log`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--follow` | bool | false | follow the log file (like tail -f) |
| `--world` | string | "" | world name |

#### `sol forge pause`

Set the forge pause flag for the world. A paused forge will not claim new
merge requests from the queue, but the forge session stays running.

Nudges the forge session so it notices the pause promptly. Resume with
sol forge resume.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge queue`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol forge restart`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge resume`

Clear the forge pause flag and nudge the session to resume claiming merge
requests from the queue immediately.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge start`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge status`

Show whether the forge process is running and its merge queue health.

Exit codes:
  0 - Forge is running
  1 - Forge is not running

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol forge stop`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge sync`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

### `sol ledger`

Manage the token tracking ledger

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol ledger restart` | Restart the ledger (stop then start) |
| `sol ledger run` | Run the ledger OTLP receiver (foreground) |
| `sol ledger start` | Start the ledger as a background process |
| `sol ledger status` | Show ledger status |
| `sol ledger stop` | Stop the ledger background process |

#### `sol ledger status`

Show whether the ledger process is running.

Prints PID, OTLP port, and heartbeat info. Use --json for machine-readable output.

Exit codes:
  0 - Ledger is running
  1 - Ledger is not running

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol prefect`

Manage the sol prefect

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol prefect restart` | Restart the prefect (stop then start) |
| `sol prefect run` | Run the prefect (foreground) |
| `sol prefect start` | Start the prefect as a background process |
| `sol prefect status` | Show prefect status |
| `sol prefect stop` | Stop the running prefect |

#### `sol prefect run`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--consul` | bool | false | Enable consul monitoring and auto-start |
| `--source-repo` | string | "" | Source repository path (for consul dispatch) |
| `--worlds` | stringSlice | [] | Comma-separated list of worlds to supervise (default: all) |

#### `sol prefect status`

Show whether the prefect process is running.

Prints status, PID, and uptime. Use --json for machine-readable output.

Exit codes:
  0 - Prefect is running
  1 - Prefect is not running

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol sentinel`

Manage the per-world sentinel health monitor

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol sentinel log` | Show or tail the sentinel log |
| `sol sentinel restart` | Restart the sentinel (stop then start) |
| `sol sentinel run` | Run the sentinel patrol loop (foreground) |
| `sol sentinel start` | Start the sentinel as a background process |
| `sol sentinel status` | Show sentinel status |
| `sol sentinel stop` | Stop the sentinel |

#### `sol sentinel log`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--follow` | bool | false | follow (tail -f) the log |
| `--world` | string | "" | world name |

#### `sol sentinel restart`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol sentinel run`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol sentinel start`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol sentinel status`

Show whether the sentinel process is running and its health metrics.

Exit codes:
  0 - Sentinel is running
  1 - Sentinel is not running

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol sentinel stop`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

### `sol service`

Manage system service units for sol sphere daemons

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol service install` | Generate and install system service units (enable but don't start) |
| `sol service restart` | Restart all sol sphere daemon units |
| `sol service start` | Start all sol sphere daemon units |
| `sol service status` | Show status of sol sphere daemon units |
| `sol service stop` | Stop all sol sphere daemon units |
| `sol service uninstall` | Stop, disable, and remove system service units |

#### `sol service status`

Show status of sol sphere daemon units.

This command queries the platform service manager (systemd on Linux, launchd
on macOS) and prints per-component state. It is suitable for use in monitoring
and health-check scripts.

Exit codes:
  0   All sol sphere daemons are running.
  1   The status command itself failed (could not query the service manager,
      or another unexpected error).
  2   One or more daemons are degraded: stopped, failed, or unknown to the
      service manager. The command itself ran successfully.

### `sol up`

Start sphere daemons and world services

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | start only world services (optionally for a specific world) |
| `--worlds` | stringSlice | [] | comma-separated list of worlds to supervise and start services for |

---

## Communication:

### `sol escalate`

Create an escalation

Create an escalation record and route it for autarch attention.

Auto-detects source from SOL_WORLD/SOL_AGENT environment variables when
called from within an agent session. Also auto-detects the active writ
from the agent's tether to set --source-ref.

Severity defaults to "medium". Routing behavior (event log, webhook) depends
on the configured escalation router and SOL_ESCALATION_WEBHOOK.

Exit codes:
  0 - Escalation created (routing is best-effort and logged as a warning
      if it fails — the escalation still exists and last_notified_at is
      recorded so the aging loop does not spin)
  1 - Failed to create the escalation or to record last_notified_at

**Usage:** `sol escalate <description>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--severity` | string | medium | Severity level (low, medium, high, critical) |
| `--source` | string | autarch | Source of the escalation |
| `--source-ref` | string | "" | Structured reference (e.g., mr:mr-abc123, writ:sol-xyz) |

### `sol escalation`

Manage escalations

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol escalation ack` | Acknowledge an escalation |
| `sol escalation list` | List escalations |
| `sol escalation resolve` | Resolve an escalation |

#### `sol escalation ack`

**Usage:** `sol escalation ack <id>`

#### `sol escalation list`

List escalations in a table.

By default, only open and acknowledged escalations are shown (resolved ones
are hidden). Use --all to include resolved escalations, or --status to filter
by a specific status.

Flags:
  --all            Include resolved escalations (shows open, acknowledged, resolved).
  --status STATUS  Show only escalations with the given status
                   (open, acknowledged, resolved). Takes precedence over --all.
  --json           Emit a JSON array with flat, structured fields.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | Include resolved escalations |
| `--json` | bool | false | Output as JSON array |
| `--status` | string | "" | Filter by status (open, acknowledged, resolved) |

#### `sol escalation resolve`

**Usage:** `sol escalation resolve <id>`

### `sol feed`

View the event activity feed

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--follow` | bool | false | tail mode — stream events as they appear |
| `--json` | bool | false | output raw JSONL |
| `--limit` | int | 20 | show only the last N events |
| `--raw` | bool | false | read raw event log instead of curated feed |
| `--since` | string | "" | show events from the last duration (e.g., 1h, 30m) |
| `--type` | string | "" | filter by event type |

### `sol inbox`

Unified TUI for autarch escalations and mail

Launch a unified inbox TUI showing escalations and unread mail.

Presents a single priority-sorted view of everything needing the
autarch's attention. Navigate with arrow keys, expand with enter,
and take inline actions (ack, resolve, dismiss).

Use --json to dump the unified item list for scripting.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol mail`

Inter-agent messaging

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol mail ack` | Acknowledge a message |
| `sol mail check` | Count unread messages |
| `sol mail inbox` | List pending messages |
| `sol mail purge` | Delete acknowledged messages |
| `sol mail read` | Read a message (marks as read) |
| `sol mail send` | Send a message |

#### `sol mail ack`

**Usage:** `sol mail ack <message-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--identity` | string | "" | Caller identity for recipient verification (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch) |

#### `sol mail check`

Check for unread messages and print the count.

Useful in scripts to conditionally process mail.

Exit codes:
  0 - Unread messages exist
  1 - No unread messages

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--identity` | string | "" | Recipient identity (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch) |

#### `sol mail inbox`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--identity` | string | "" | Recipient identity (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch) |
| `--json` | bool | false | Output as JSON |

#### `sol mail purge`

Delete acknowledged messages from the sphere mailbox.

Requires --confirm to proceed; without it, previews what would be deleted and exits 1.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all-acked` | bool | false | Delete all acknowledged messages regardless of age |
| `--before` | string | "" | Delete acked messages older than duration (e.g., 7d, 24h) |
| `--confirm` | bool | false | confirm destructive action |

#### `sol mail read`

**Usage:** `sol mail read <message-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--identity` | string | "" | Caller identity for recipient verification (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch) |

#### `sol mail send`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--body` | string | "" | Message body |
| `--no-notify` | bool | false | Suppress nudge notification to recipient |
| `--priority` | int | 2 | Priority (1=urgent, 2=normal, 3=low) |
| `--subject` | string | "" | Message subject |
| `--to` | string | "" | Recipient agent ID or "autarch" |
| `--world` | string | "" | world name |

---

## Setup & Diagnostics:

### `sol account`

Manage Claude OAuth accounts

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol account add` | Register a new account |
| `sol account default` | Show or set the default account |
| `sol account delete` | Delete a registered account |
| `sol account list` | List registered accounts |
| `sol account set-api-key` | Store an API key for an account |
| `sol account set-token` | Store an OAuth token for an account |

#### `sol account add`

**Usage:** `sol account add <handle>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--description` | string | "" | account description |
| `--email` | string | "" | email associated with the account |

#### `sol account default`

**Usage:** `sol account default [<handle>]`

#### `sol account delete`

Delete a registered account and its stored credentials.

Requires --confirm to proceed; without it, prints what would be removed and
exits. Before deleting, sol scans for live bindings to the account:

  - quota state (.runtime/quota.json)
  - any world's default_account (world.toml)
  - any agent's claude-config metadata (.claude-config/<role>s/<agent>/.account)

If any live bindings are found and --force is not set, the command refuses to
delete the account and lists every binding it found. Pass --force to proceed
anyway; a warning is logged for each still-bound binding before the deletion.

Exit codes:
  0  account deleted (or dry-run preview when --confirm absent and no bindings)
  1  general failure (account not found, registry I/O error, or dry-run preview)
  2  refused: live bindings exist and --force was not supplied

**Usage:** `sol account delete <handle>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | confirm deletion |
| `--force` | bool | false | proceed even if the account has live bindings (logs a warning per binding) |

#### `sol account list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol account set-api-key`

**Usage:** `sol account set-api-key <handle> [key]`

#### `sol account set-token`

**Usage:** `sol account set-token <handle> [token]`

### `sol config`

Manage sol configuration

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol config claude` | Edit sphere-level Claude Code defaults |

#### `sol config claude`

Launch an interactive Claude Code session for configuring sphere-level defaults.

The defaults directory ($SOL_HOME/.claude-defaults/) is the template for all
agent config directories. Changes made here propagate to all agents on their
next session start.

File ownership:
  settings.json        Sol-owned. Always overwritten from template. Do not edit.
  settings.local.json  User-owned. Your customizations go here.
  plugins/             Managed by /install and /uninstall. Shared sphere-wide.

Plugins installed here are available to all agents across all worlds.
After installing a plugin, verify its enabledPlugins entry exists in
settings.local.json (not just settings.json) to ensure it persists
across sol restarts.

Uses the sphere-level default account for authentication.

### `sol doctor`

Check system prerequisites

Validate that all prerequisites for running sol are met.

Checks: tmux, git, claude CLI, SOL_HOME directory, SQLite WAL support.

Exit code 0 if all checks pass, 1 if any check fails.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol init`

Initialize sol for first-time use

Set up SOL_HOME directory structure and create your first world.

Three modes:
  Flag mode:        sol init --name=myworld [--source-repo=<url-or-path>]
  Interactive mode: sol init (prompts for input when stdin is a TTY)
  Guided mode:      sol init --guided (Claude-powered setup conversation)

Runs prerequisite checks (sol doctor) by default. Use --skip-checks to bypass.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--guided` | bool | false | Claude-powered guided setup |
| `--name` | string | "" | world name (required in flag mode) |
| `--skip-checks` | bool | false | skip prerequisite checks |
| `--source-repo` | string | "" | git URL or local path to source repository |

### `sol migrate`

Manage sol migrations

Manage sol's built-in migration framework.

Sol ships with a registry of migrations — upgrade steps that shift an
existing installation from one state to another. Pending migrations are
surfaced automatically via 'sol doctor' and the banner printed by 'sol up'
so operators see them the moment they matter.

Subcommands:
  list     — show all registered migrations with their status
  show     — print the full description of a single migration
  run      — execute a migration (requires --confirm)
  history  — show previously applied migrations (newest first)

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol migrate history` | Show previously applied migrations, newest first |
| `sol migrate list` | List registered migrations with status |
| `sol migrate run` | Execute a registered migration |
| `sol migrate show` | Print the full description of a migration |

#### `sol migrate history`

Show the migrations_applied table, newest first.

Columns: NAME, VERSION, APPLIED AT, SUMMARY.

Exit code 0 unless IO fails.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol migrate list`

List all registered migrations with their current status.

STATUS values:
  applied      — recorded in migrations_applied; nothing to do
  pending      — Detect reports the migration is applicable
  not-needed   — Detect reports the migration is not applicable
  error        — Detect returned an error; see the REASON column

Exit code 0 unless IO fails.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol migrate run`

Execute a registered migration.

By default, 'sol migrate run' is a dry run: it calls the migration's Detect
function and prints what it would do, then exits 1. Use --confirm to
actually execute the migration.

On success, the result is recorded in the sphere's migrations_applied
table. On failure, nothing is recorded — migrations must be idempotent, so
re-running after fixing the underlying issue is safe.

Flags:
  --confirm       actually execute (otherwise dry-run only)
  --force         bypass the "already applied" guard (does not bypass Detect)
  --world=<name>  scope to a single world (ignored by sphere-wide migrations)

Exit codes:
  0  success or dry-run with an applicable detection
  1  dry-run (printed what would run), migration not registered, or failure

**Usage:** `sol migrate run <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | actually execute (default: dry-run) |
| `--force` | bool | false | bypass already-applied guard |
| `--world` | string | "" | scope to a single world |

#### `sol migrate show`

Print the markdown description of a registered migration to stdout.

Exit code 1 if the named migration is not registered.

**Usage:** `sol migrate show <name>`

### `sol quota`

Manage account rate limit state

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol quota rotate` | Rotate rate-limited agents to available accounts |
| `sol quota scan` | Scan agent sessions for rate limit errors |
| `sol quota status` | Show per-account quota state |

#### `sol quota rotate`

Rotate rate-limited agents off their current account onto an available
account. By default this is a preview only — pass --confirm to actually
perform the rotation.

Exit codes:
  0 - Rotation executed successfully (--confirm), or no rotation needed
  1 - Preview mode (--confirm not provided), or an error occurred

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | execute rotations (default is preview-only) |
| `--world` | string | "" | world name |

#### `sol quota scan`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol quota status`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol schema`

Schema version and migration management

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol schema migrate` | Run schema migrations on all databases |
| `sol schema status` | Show schema version information for all databases |

#### `sol schema migrate`

Run schema migrations on the sphere database and every world
database in the store directory.

By default this is a preview only — pass --confirm to actually apply
migrations. Pass --backup to snapshot each database before migrating.

Exit codes:
  0 - Migrations applied successfully (--confirm), or all databases
      already at current schema version
  1 - Preview mode (--confirm not provided), or an error occurred

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--backup` | bool | false | Create a backup of each database before migrating |
| `--confirm` | bool | false | Execute migrations (default is preview-only) |

#### `sol schema status`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol world`

Manage worlds

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol world clone` | Clone a world |
| `sol world delete` | Delete a world |
| `sol world export` | Export a world to a tar.gz archive |
| `sol world import` | Import a world from an export archive |
| `sol world init` | Initialize a new world |
| `sol world list` | List all worlds |
| `sol world sleep` | Mark a world as sleeping and stop its services |
| `sol world status` | Show world status with config |
| `sol world sync` | Sync the managed repo with its remote |
| `sol world wake` | Mark a world as active and start its services |

#### `sol world clone`

Duplicate a world with copied configuration, database state (writs,
dependencies), and directory structure. Credentials and tethers are NOT copied.
The new world gets a fresh agent pool.

Agent state (history, token usage) is excluded by default. Use --include-history
to copy it.

**Usage:** `sol world clone <source> <target>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--include-history` | bool | false | include agent history and token usage in clone |

#### `sol world delete`

Permanently delete a world and all associated data:
  - World database (writs, merge requests, dependencies)
  - World directory (repo, outposts, worktrees, config)
  - Agent records for the world in sphere.db

Refuses to delete if any agent sessions are still running — stop them first.
Requires --confirm to proceed; without it, prints what would be deleted and exits.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | confirm deletion |
| `--world` | string | "" | world name |

#### `sol world export`

Export a world's state to a compressed archive for backup or migration.

The archive includes the world database (WAL-checkpointed), world.toml,
sphere-scoped data (agents, messages, escalations, caravans), and a manifest.
Ephemeral state (tmux sessions, PID files, worktrees) is excluded.

The managed repo (repo/) is excluded — it can be re-cloned from source_repo.

**Usage:** `sol world export <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--output` | string | "" | output file path (default: <name>-export.tar.gz) |

#### `sol world import`

Restore a world from a .tar.gz archive produced by sol world export.

Validates the archive manifest and schema compatibility before restoring.
Refuses to import if the world name already exists — delete it first or
use --name to import under a different name.

Agent states are reset to idle on import (no active sessions exist for
imported agents). Ephemeral state (repo, worktrees, sessions) is not
restored — run sol world sync after import to clone the managed repo.

**Usage:** `sol world import <archive>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name` | string | "" | import under a different name (rewrites agent IDs and references) |

#### `sol world init`

Create a new world with directory structure, database, and configuration.

Creates:
  - World directory at $SOL_HOME/<name>/ with outposts/ subdirectory
  - World database (<name>.db) with schema migrations
  - Default world.toml configuration
  - Managed repo clone (if --source-repo is provided)

Registers the world in sphere.db. If a pre-Arc1 database exists (DB without
world.toml), migrates legacy quality gates and name pool settings.

world.toml configuration reference:

  [world]
  source_repo = "/path/to/repo"   # persistent source repo binding
  branch = "main"                 # primary branch (used for merges and guard protection)
  protected_branches = []         # additional protected branches (glob patterns OK)

  [agents]
  max_active = 10                 # max concurrent agents (0 = unlimited)
  name_pool_path = ""             # custom name pool file (empty = built-in)
  model = "sonnet"                # default model for all roles (passthrough to runtime)

  [agents.models.claude]          # per-runtime, per-role model overrides
  outpost = "sonnet"              # overrides agents.model for outpost agents
  envoy = "opus"                  # overrides agents.model for envoy agents
  forge = "sonnet"                # overrides agents.model for forge

  [forge]
  quality_gates = ["make test"]   # commands that must pass before merge
  gate_timeout = "5m"             # per-gate timeout

Resolution order for model: agents.models.<runtime>.<role> → agents.model → adapter.DefaultModel().
Any non-empty string is valid (passed through to the runtime).

**Usage:** `sol world init <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--source-repo` | string | "" | git URL or local path to source repository |

#### `sol world list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol world sleep`

Mark a world as sleeping, which stops world services (sentinel, forge)
and activates dispatch gates that prevent new work from being cast.

With --force, also stops all outpost agent sessions immediately:
  - Waits up to 30 seconds for session stability before killing
  - Kills sessions that don't stabilize in time
  - Returns writs to "open" status, sets agents to "idle", clears tethers
  - Warns envoy sessions but does not stop them (human-directed)

**Usage:** `sol world sleep <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | false | stop all outpost agent sessions and return their writs to the open pool |

#### `sol world status`

**Usage:** `sol world status <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol world sync`

Fetch and pull latest changes from the source repo's origin.
If the managed repo doesn't exist yet but source_repo is configured
in world.toml, clones it first.

With --all, also syncs forge worktree and notifies running envoy sessions.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | also sync forge and envoys |
| `--world` | string | "" | world name |

#### `sol world wake`

Clear the sleeping flag in world.toml and restart world services
(sentinel, forge). This reverses sol world sleep — dispatch gates are
deactivated and new work can be cast again.

Does not restart outpost agent sessions that were stopped by sleep --force;
those must be re-dispatched manually.

**Usage:** `sol world wake <name>`

---

## Plumbing:

### `sol docs`

Documentation tools

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol docs generate` | Generate CLI reference documentation |
| `sol docs validate` | Validate docs/cli.md against the command tree |

#### `sol docs generate`

Generate docs/cli.md from the Cobra command tree. Use --check to validate without writing.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--check` | bool | false | Validate docs/cli.md without writing (same as sol docs validate) |
| `--stdout` | bool | false | Write to stdout instead of docs/cli.md |

#### `sol docs validate`

Compare docs/cli.md against what the command tree would generate. Exits non-zero if discrepancies are found.

---

## Other Commands

### `sol completion`

Generate the autocompletion script for the specified shell

Generate the autocompletion script for sol for the specified shell.
See each sub-command's help for details on how to use the generated script.


**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol completion bash` | Generate the autocompletion script for bash |
| `sol completion fish` | Generate the autocompletion script for fish |
| `sol completion powershell` | Generate the autocompletion script for powershell |
| `sol completion zsh` | Generate the autocompletion script for zsh |

#### `sol completion bash`

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source <(sol completion bash)

To load completions for every new session, execute once:

#### Linux:

	sol completion bash > /etc/bash_completion.d/sol

#### macOS:

	sol completion bash > $(brew --prefix)/etc/bash_completion.d/sol

You will need to start a new shell for this setup to take effect.


| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-descriptions` | bool | false | disable completion descriptions |

#### `sol completion fish`

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	sol completion fish | source

To load completions for every new session, execute once:

	sol completion fish > ~/.config/fish/completions/sol.fish

You will need to start a new shell for this setup to take effect.


| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-descriptions` | bool | false | disable completion descriptions |

#### `sol completion powershell`

Generate the autocompletion script for powershell.

To load completions in your current shell session:

	sol completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output of the above command
to your powershell profile.


| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-descriptions` | bool | false | disable completion descriptions |

#### `sol completion zsh`

Generate the autocompletion script for the zsh shell.

If shell completion is not already enabled in your environment you will need
to enable it.  You can execute the following once:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

To load completions in your current shell session:

	source <(sol completion zsh)

To load completions for every new session, execute once:

#### Linux:

	sol completion zsh > "${fpath[1]}/_sol"

#### macOS:

	sol completion zsh > $(brew --prefix)/share/zsh/site-functions/_sol

You will need to start a new shell for this setup to take effect.


| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-descriptions` | bool | false | disable completion descriptions |

---

## Plumbing Commands

These commands are hidden from `--help` output. They are internal commands used by Sol's orchestration layer and hooks. They remain fully functional when called directly.

- `sol account remove — Deprecated: use 'sol account delete'`
- `sol forge blocked — List blocked merge requests`
- `sol forge check-unblocked — Check for resolved blockers and unblock MRs`
- `sol forge claim — Claim the next ready unblocked merge request`
- `sol forge create-resolution — Create a conflict resolution task and block the MR`
- `sol forge mark-failed — Mark a merge request as failed`
- `sol forge mark-merged — Mark a merge request as merged`
- `sol forge ready — List ready (unblocked) merge requests`
- `sol forge release — Release a claimed merge request back to ready`
- `sol forge run — Run the forge patrol loop (internal — launched by forge start)`
- `sol guard dangerous-command — Block dangerous commands (rm -rf, force push, hard reset, etc.)`
- `sol guard workflow-bypass — Block commands that circumvent the forge merge pipeline`
- `sol log-event — Log a custom event to the event feed (plumbing)`
- `sol mr create — Create a merge request for an existing writ`
- `sol mr — Merge request plumbing commands`
- `sol nudge count — Print count of pending nudge messages`
- `sol nudge drain — Drain pending nudge messages for an agent session`
- `sol nudge list — View pending nudge queue messages`
- `sol prime — Assemble and print execution context for an agent`
- `sol workflow eject — Eject an embedded workflow for customization`
- `sol writ get — Show writ status`

