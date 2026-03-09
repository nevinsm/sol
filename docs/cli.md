# CLI Reference

Auto-generated from the Cobra command tree. Do not edit manually.

Run `sol docs generate` to regenerate this file.

---

## Dispatch:

### `sol cast`

Assign a writ to an agent and start its session

**Usage:** `sol cast <writ-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--account` | string | "" | account to use for credentials (overrides world.toml default_account) |
| `--agent` | string | "" | agent name (auto-selects idle agent if omitted) |
| `--var` | stringSlice | [] | workflow variable (key=val, repeatable) |
| `--workflow` | string | "" | workflow to instantiate |
| `--world` | string | "" | world name |

### `sol cost`

Show token usage and cost across worlds

Show token usage and estimated cost.

Without flags, shows sphere-wide per-world cost totals.
With --world, shows per-agent breakdown within a world.
With --agent and --world, shows per-writ breakdown for an agent.
With --caravan, shows per-writ breakdown across worlds for a caravan.
With --since, filters by time window (relative duration or absolute date).

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | show per-writ breakdown for an agent (requires --world) |
| `--caravan` | string | "" | show per-writ breakdown for a caravan (ID or name) |
| `--json` | bool | false | output as JSON |
| `--since` | string | "" | time window: relative duration (24h) or absolute date (2006-01-02) |
| `--world` | string | "" | show per-agent breakdown for a world |

### `sol handoff`

Hand off to a fresh session with context preservation

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--reason` | string | "" | handoff reason (compact, manual, health-check) |
| `--summary` | string | "" | summary of current progress |
| `--world` | string | "" | world name (defaults to SOL_WORLD env) |

### `sol resolve`

Signal work completion — code writs push branch and create MR; non-code writs close directly

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--world` | string | "" | world name (defaults to SOL_WORLD env) |

### `sol status`

Show sphere or world status

Show system status.

Without arguments, auto-detects world from cwd (or SOL_WORLD).
If a world is detected, shows sphere processes plus world detail combined.
Otherwise, shows a sphere-level overview of all worlds and processes.
With a world name, shows detailed status for that specific world.

Exit codes (world --json only):
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
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

#### `sol caravan check`

**Usage:** `sol caravan check <caravan-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol caravan close`

Close a caravan by ID, or use --auto to close all caravans where every item is merged.

**Usage:** `sol caravan close [<caravan-id>]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--auto` | bool | false | scan all open caravans and close any where all items are merged |
| `--force` | bool | false | close even if not all items are merged |

#### `sol caravan create`

**Usage:** `sol caravan create <name> [<item-id> ...]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--owner` | string | "" | caravan owner (default: operator) |
| `--phase` | int | 0 | phase for items (default 0) |
| `--world` | string | "" | world name |

#### `sol caravan delete`

Delete a drydocked or closed caravan entirely, including all items and dependencies.

**Usage:** `sol caravan delete <caravan-id>`

#### `sol caravan dep`

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol caravan dep add` | Declare that a caravan depends on another caravan being closed |
| `sol caravan dep list` | Show caravan-level dependencies |
| `sol caravan dep remove` | Remove a caravan dependency |

##### `sol caravan dep list`

**Usage:** `sol caravan dep list <caravan-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol caravan launch`

**Usage:** `sol caravan launch <caravan-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--var` | stringSlice | [] | variable assignment (key=val) |
| `--workflow` | string | "" | workflow for dispatched items |
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

#### `sol caravan list`

List all caravans. Shows active (non-closed) caravans by default. Use --all for all caravans or --status to filter.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | include closed caravans |
| `--json` | bool | false | output as JSON |
| `--status` | string | "" | filter by status (open, ready, closed) |

#### `sol caravan remove`

Remove an item from a caravan. Cannot remove from closed caravans.

**Usage:** `sol caravan remove <caravan-id> <item-id>`

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

Manage workflow instances

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol workflow advance` | Advance to the next workflow step |
| `sol workflow current` | Print the current step's instructions |
| `sol workflow eject` | Eject an embedded workflow for customization |
| `sol workflow fail` | Mark the current workflow step and workflow as failed |
| `sol workflow init` | Scaffold a new workflow |
| `sol workflow instantiate` | Instantiate a workflow |
| `sol workflow list` | List available workflows |
| `sol workflow manifest` | Manifest a workflow into writs and a caravan |
| `sol workflow show` | Display workflow details and resolution source |
| `sol workflow skip` | Skip the current workflow step and advance to the next |
| `sol workflow status` | Show workflow status |

#### `sol workflow advance`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

#### `sol workflow current`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

#### `sol workflow eject`

Copies an embedded workflow to the user or project tier so it can be customized. Use --force to refresh from embedded defaults (backs up existing).

**Usage:** `sol workflow eject <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | false | overwrite existing workflow (backs up to {name}.bak-{timestamp}) |
| `--project` | bool | false | eject to project tier instead of user tier (requires --world) |
| `--world` | string | "" | world name (for project tier path resolution) |

#### `sol workflow fail`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

#### `sol workflow init`

**Usage:** `sol workflow init <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | bool | false | create in project tier (.sol/workflows/) |
| `--type` | string | workflow | workflow type (workflow, expansion, or convoy) |
| `--world` | string | "" | world name (required with --project) |

#### `sol workflow instantiate`

**Usage:** `sol workflow instantiate <workflow>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--item` | string | "" | writ ID |
| `--var` | stringSlice | [] | variable assignment (key=val) |
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

#### `sol workflow list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | show all tiers including shadowed workflows |
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name (for project-tier discovery) |

#### `sol workflow manifest`

**Usage:** `sol workflow manifest <workflow>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--target` | string | "" | existing writ ID to manifest against (required for expansion workflows) |
| `--var` | stringSlice | [] | variable assignment (key=val) |
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

#### `sol workflow show`

**Usage:** `sol workflow show [workflow]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--path` | string | "" | load workflow from directory path instead of by name |
| `--world` | string | "" | world name (for project-tier resolution) |

#### `sol workflow skip`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

#### `sol workflow status`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

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
| `--world` | string | "" | world name (defaults to SOL_WORLD env) |

#### `sol writ clean`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | false | list eligible writs without modifying anything |
| `--older-than` | string | "" | retention threshold (e.g., 7d, 15d, 30d) |
| `--world` | string | "" | world name |

#### `sol writ close`

**Usage:** `sol writ close <id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
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
| `--world` | string | "" | world name (skip world scanning) |

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
| `--role` | string | agent | agent role |
| `--world` | string | "" | world name |

#### `sol agent handoffs`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | filter by agent name |
| `--last` | int | 20 | number of recent events to show |
| `--world` | string | "" | world name |

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
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

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

**Usage:** `sol agent reset <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol agent stats`

Shows performance summary for a single agent, or a leaderboard across all agents when no name is given.

**Usage:** `sol agent stats [name]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

### `sol envoy`

Manage persistent envoy agents

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol envoy attach` | Attach to an envoy's tmux session |
| `sol envoy brief` | Display an envoy's brief |
| `sol envoy create` | Create an envoy agent |
| `sol envoy debrief` | Archive the envoy's brief and reset for fresh engagement |
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

#### `sol envoy brief`

**Usage:** `sol envoy brief <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol envoy create`

**Usage:** `sol envoy create <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol envoy debrief`

**Usage:** `sol envoy debrief <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol envoy delete`

**Usage:** `sol envoy delete <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | false | force delete even if session is active or tethered |
| `--world` | string | "" | world name |

#### `sol envoy list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name (optional, lists all if omitted) |

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

### `sol forget`

Delete a memory for the current agent

Delete a memory by key, or all memories with --all.

  sol forget "key"     — delete a single memory
  sol forget --all     — delete all memories for this agent

**Usage:** `sol forget [key]`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--all` | bool | false | delete all memories for this agent |
| `--world` | string | "" | world name |

### `sol memories`

List all memories for the current agent

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--world` | string | "" | world name |

### `sol remember`

Persist a memory for the current agent

Persist a key-value memory that survives across sessions.

With two arguments: sol remember "key" "value"
With one argument:  sol remember "value"  (key auto-generated from hash)

**Usage:** `sol remember [key] <value>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
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

#### `sol session capture`

**Usage:** `sol session capture <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--lines` | int | 50 | number of lines to capture |

#### `sol session health`

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
| `--role` | string | agent | session role |
| `--workdir` | string | . | working directory |
| `--world` | string | "" | world name |

#### `sol session stop`

**Usage:** `sol session stop <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | false | force kill without graceful shutdown |

### `sol tether`

Bind a writ to a persistent agent (envoy, governor, forge)

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

### `sol chronicle`

Manage the event feed chronicle

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol chronicle restart` | Restart the chronicle (stop then start) |
| `sol chronicle run` | Run the chronicle (foreground) |
| `sol chronicle start` | Start the chronicle as a background tmux session |
| `sol chronicle status` | Show chronicle status |
| `sol chronicle stop` | Stop the chronicle background session |

#### `sol chronicle status`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol consul`

Manage the sphere-level consul patrol process

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol consul attach` | Attach to the consul tmux session |
| `sol consul restart` | Restart the consul (stop then start) |
| `sol consul run` | Run the consul patrol loop (foreground) |
| `sol consul start` | Start the consul as a background tmux session |
| `sol consul status` | Show consul status from heartbeat |
| `sol consul stop` | Stop the consul background session |

#### `sol consul run`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--interval` | string | 5m | patrol interval |
| `--stale-timeout` | string | 1h | stale tether timeout |
| `--webhook` | string | "" | escalation webhook URL |

#### `sol consul status`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol down`

Stop sphere daemons and world services

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | also stop envoy, governor, and senate sessions |
| `--world` | string | "" | stop only world services (optionally for a specific world) |

### `sol forge`

Manage the merge pipeline forge

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol forge attach` | Attach to the forge tmux session |
| `sol forge await` | Block until a nudge arrives or timeout expires |
| `sol forge blocked` | List blocked merge requests |
| `sol forge check-unblocked` | Check for resolved blockers and unblock MRs |
| `sol forge claim` | Claim the next ready unblocked merge request |
| `sol forge create-resolution` | Create a conflict resolution task and block the MR |
| `sol forge mark-failed` | Mark a merge request as failed |
| `sol forge mark-merged` | Mark a merge request as merged |
| `sol forge pause` | Pause the forge — stop claiming new MRs |
| `sol forge queue` | Show the merge request queue |
| `sol forge ready` | List ready (unblocked) merge requests |
| `sol forge release` | Release a claimed merge request back to ready |
| `sol forge restart` | Restart the forge (stop then start) |
| `sol forge resume` | Resume the forge — start claiming MRs again |
| `sol forge start` | Start the forge as a Claude session |
| `sol forge status` | Show forge health summary |
| `sol forge stop` | Stop the forge |
| `sol forge sync` | Sync forge worktree: fetch origin, reset to target branch |

#### `sol forge attach`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge await`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--timeout` | int | 120 | max seconds to wait |
| `--world` | string | "" | world name |

#### `sol forge blocked`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol forge check-unblocked`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol forge claim`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol forge create-resolution`

**Usage:** `sol forge create-resolution <mr-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol forge mark-failed`

**Usage:** `sol forge mark-failed <mr-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge mark-merged`

**Usage:** `sol forge mark-merged <mr-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge pause`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge queue`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol forge ready`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol forge release`

**Usage:** `sol forge release <mr-id>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge restart`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge resume`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge start`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge status`

**Usage:** `sol forge status <world>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol forge stop`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol forge sync`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

### `sol governor`

Manage the per-world governor coordinator

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol governor attach` | Attach to the governor's tmux session |
| `sol governor brief` | Display the governor's brief |
| `sol governor debrief` | Archive the governor's brief and reset |
| `sol governor restart` | Restart the governor (stop then start) |
| `sol governor start` | Start the governor for a world |
| `sol governor status` | Show governor status |
| `sol governor stop` | Stop the governor for a world |
| `sol governor summary` | Display the governor's world summary |
| `sol governor sync` | Sync managed repo the governor reads from |

#### `sol governor attach`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol governor brief`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol governor debrief`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol governor restart`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol governor start`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol governor status`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol governor stop`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol governor summary`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

#### `sol governor sync`

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
| `sol ledger start` | Start the ledger as a background tmux session |
| `sol ledger status` | Show ledger status |
| `sol ledger stop` | Stop the ledger background session |

#### `sol ledger status`

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

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol senate`

Manage the sphere-scoped planning session

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol senate attach` | Attach to the senate tmux session |
| `sol senate brief` | Display the senate's brief |
| `sol senate debrief` | Archive the senate's brief and reset |
| `sol senate restart` | Restart the senate (stop then start) |
| `sol senate start` | Start the senate planning session |
| `sol senate status` | Show senate status |
| `sol senate stop` | Stop the senate session |

#### `sol senate status`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

### `sol sentinel`

Manage the per-world sentinel health monitor

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol sentinel attach` | Attach to the sentinel tmux session |
| `sol sentinel restart` | Restart the sentinel (stop then start) |
| `sol sentinel run` | Run the sentinel patrol loop (foreground) |
| `sol sentinel start` | Start the sentinel as a background tmux session |
| `sol sentinel status` | Show sentinel status |
| `sol sentinel stop` | Stop the sentinel |

#### `sol sentinel attach`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
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

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name |

#### `sol sentinel stop`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | "" | world name |

### `sol token-broker`

Manage the token broker for centralized OAuth refresh

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol token-broker restart` | Restart the token broker (stop then start) |
| `sol token-broker run` | Run the token broker loop (foreground) |
| `sol token-broker start` | Start the token broker as a background process |
| `sol token-broker status` | Show token broker status from heartbeat |
| `sol token-broker stop` | Stop the running token broker |

#### `sol token-broker run`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--interval` | string | 5m | patrol interval |
| `--refresh-margin` | string | 30m | refresh tokens this long before expiry |

#### `sol token-broker status`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

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

**Usage:** `sol escalate <description>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--severity` | string | medium | Severity level (low, medium, high, critical) |
| `--source` | string | operator | Source of the escalation |
| `--source-ref` | string | "" | Structured reference (e.g., mr:mr-abc123, writ:sol-xyz) |

### `sol escalation`

Manage escalations

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol escalation ack` | Acknowledge an escalation |
| `sol escalation list` | List escalations |
| `sol escalation resolve` | Resolve an escalation |

#### `sol escalation list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | Include resolved escalations |
| `--json` | bool | false | Output as JSON array |
| `--status` | string | "" | Filter by status (open, acknowledged, resolved) |

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

View pending nudge queue messages

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--json` | bool | false | output as JSON |
| `--world` | string | "" | world name (defaults to SOL_WORLD env) |

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol inbox count` | Print count of pending messages |
| `sol inbox drain` | Drain and display all pending messages |

#### `sol inbox drain`

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

#### `sol mail check`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--identity` | string | operator | Recipient to check |

#### `sol mail inbox`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--identity` | string | operator | Recipient to check |
| `--json` | bool | false | Output as JSON |

#### `sol mail purge`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all-acked` | bool | false | Delete all acknowledged messages regardless of age |
| `--before` | string | "" | Delete acked messages older than duration (e.g., 7d, 24h) |
| `--force` | bool | false | Skip confirmation prompt |

#### `sol mail send`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--body` | string | "" | Message body |
| `--no-notify` | bool | false | Suppress nudge notification to recipient |
| `--priority` | int | 2 | Priority (1=urgent, 2=normal, 3=low) |
| `--subject` | string | "" | Message subject |
| `--to` | string | "" | Recipient agent ID or "operator" |
| `--world` | string | "" | World for recipient resolution (default: from env or cwd) |

### `sol nudge`

Nudge queue operations

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol nudge drain` | Drain pending nudge messages for an agent session |

#### `sol nudge drain`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name (defaults to SOL_AGENT env) |
| `--world` | string | "" | world name (optional with SOL_WORLD or inside a world directory) |

---

## Setup & Diagnostics:

### `sol account`

Manage Claude OAuth accounts

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol account add` | Register a new account |
| `sol account default` | Show or set the default account |
| `sol account list` | List registered accounts |
| `sol account login` | Open a Claude session to complete OAuth login |
| `sol account remove` | Remove a registered account |

#### `sol account add`

**Usage:** `sol account add <handle>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--description` | string | "" | account description |
| `--email` | string | "" | email associated with the account |

### `sol config`

Manage sol configuration

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol config claude` | Edit sphere-level Claude Code defaults |

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

### `sol quota`

Manage account rate limit state

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol quota rotate` | Rotate rate-limited agents to available accounts |
| `sol quota scan` | Scan agent sessions for rate limit errors |
| `sol quota status` | Show per-account quota state |

#### `sol quota rotate`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | false | show planned rotations without executing |
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

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--backup` | bool | false | Create a backup of each database before migrating |
| `--dry-run` | bool | false | Preview migrations without applying them |

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
| `sol world query` | Query a world's governor for information |
| `sol world sleep` | Mark a world as sleeping and stop its services |
| `sol world status` | Show world status with config |
| `sol world summary` | Show a world's governor-maintained summary |
| `sol world sync` | Sync the managed repo with its remote |
| `sol world wake` | Mark a world as active and start its services |

#### `sol world clone`

Duplicate a world with copied configuration, database state (writs,
dependencies), and directory structure. Credentials and tethers are NOT copied.
The new world gets a fresh agent pool.

Agent state (history, memories) is excluded by default. Use --include-history
to copy it.

**Usage:** `sol world clone <source> <target>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--include-history` | bool | false | include agent history and memories in clone |

#### `sol world delete`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--confirm` | bool | false | confirm deletion |
| `--world` | string | "" | world name |

#### `sol world export`

Export a world's state to a compressed archive for backup or migration.

The archive includes the world database (WAL-checkpointed), world.toml,
agent configuration directories, and a metadata manifest. Ephemeral state
(tmux sessions, PID files, worktrees) is excluded.

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

**Usage:** `sol world init <name>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--source-repo` | string | "" | git URL or local path to source repository |

#### `sol world list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | false | output as JSON |

#### `sol world query`

Inject a question into the governor's tmux session and wait for a response.

The governor reads the question from .query/pending.md, writes its answer to
.query/response.md, and the CLI returns the response. If the governor is not
running, returns an error (callers should fall back to the static world summary).

**Usage:** `sol world query <name> <question>`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--timeout` | int | 120 | seconds to wait for governor response |

#### `sol world sleep`

Mark a world as sleeping, which stops world services (sentinel, forge,
governor) and activates dispatch gates that prevent new work from being cast.

With --force, also stops all outpost agent sessions immediately:
  - Injects a brief-save prompt and waits up to 30 seconds for stability
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

With --all, also syncs forge worktree and notifies running envoy/governor sessions.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | also sync forge, envoys, and governor |
| `--world` | string | "" | world name |

---

## Plumbing:

### `sol brief`

Manage agent brief files

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol brief inject` | Inject brief into session context |

#### `sol brief inject`

Read a brief file and output framed content for session injection.

Used by Claude Code hooks to inject agent context on session start
and after context compaction.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--max-lines` | int | 200 | maximum lines before truncation |
| `--path` | string | "" | path to brief file |

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

### `sol guard`

Block forbidden operations (PreToolUse hook)

Block forbidden operations via Claude Code PreToolUse hooks.

Guard commands exit with code 2 to BLOCK tool execution when a policy
is violated. They're called before the tool runs, preventing the
forbidden operation entirely.

Available guards:
  dangerous-command  - Block rm -rf /, force push, hard reset, git clean, checkout --
  workflow-bypass    - Block PR creation, direct push to main, manual branching

Example hook configuration:
  {
    "PreToolUse": [{
      "matcher": "Bash(git push --force*)",
      "hooks": [{"command": "sol guard dangerous-command"}]
    }]
  }

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol guard dangerous-command` | Block dangerous commands (rm -rf, force push, hard reset, etc.) |
| `sol guard workflow-bypass` | Block commands that circumvent the forge merge pipeline |

### `sol log-event`

Log a custom event to the event feed (plumbing)

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--actor` | string | "" | who triggered the event (required) |
| `--payload` | string | {} | JSON payload |
| `--source` | string | sol | event source |
| `--type` | string | "" | event type (required) |
| `--visibility` | string | both | event visibility (feed, audit, or both) |

### `sol mr`

Merge request plumbing commands

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol mr create` | Create a merge request for an existing writ |

#### `sol mr create`

Plumbing command to manually queue a branch for forge review without going through sol resolve.

**Usage:** `sol mr create --world=W --branch=B --writ=ID`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--branch` | string | "" | branch to merge (required) |
| `--json` | bool | false | output as JSON |
| `--priority` | int | 2 | priority (1=high, 2=normal, 3=low) |
| `--world` | string | "" | world name (required) |
| `--writ` | string | "" | writ ID (required) |

### `sol prime`

Assemble and print execution context for an agent

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--agent` | string | "" | agent name |
| `--world` | string | "" | world name |

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

### `sol dash`

Live TUI dashboard

Launch a live terminal dashboard.

Without arguments, auto-detects the current world from SOL_WORLD or the
working directory. Falls back to the sphere-level overview if no world
is detected. With a world name, shows detailed status for that world.

The dashboard refreshes every 3 seconds. Press r to force refresh.

**Usage:** `sol dash [world]`

### `sol service`

Manage systemd user units for sol sphere daemons

**Subcommands:**

| Command | Description |
|---------|-------------|
| `sol service install` | Generate and install systemd user units (enable but don't start) |
| `sol service restart` | Restart all sol sphere daemon units |
| `sol service start` | Start all sol sphere daemon units |
| `sol service status` | Show status of sol sphere daemon units |
| `sol service stop` | Stop all sol sphere daemon units |
| `sol service uninstall` | Stop, disable, and remove systemd user units |

