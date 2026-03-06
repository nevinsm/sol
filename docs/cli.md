# CLI Reference

## World Resolution

Most commands accept a `--world` flag. When omitted, the world is resolved automatically:

1. **`--world=W` flag** — explicit, always wins
2. **`SOL_WORLD` env var** — set automatically in agent sessions
3. **Current directory** — if cwd is under `$SOL_HOME/{world}/`, the world is inferred

This means `--world` is optional when running from inside a world directory (e.g., an agent worktree) or when `SOL_WORLD` is set.

## Setup

| Command | Description |
|---------|-------------|
| `sol init` | Initialize sol for first-time use |
| `sol doctor` | Check system prerequisites |

## Account Management

| Command | Description |
|---------|-------------|
| `sol account add <handle>` | Register a new account |
| `sol account list` | List registered accounts |
| `sol account remove <handle>` | Remove a registered account |
| `sol account default [<handle>]` | Show or set the default account |
| `sol account login <handle>` | Open a Claude session to complete OAuth login |

Accounts are stored under `$SOL_HOME/.accounts/`. Each account has its own config directory with OAuth credentials. Agents reference accounts via credential symlinks in their config dirs.

## Quota Management

| Command | Description |
|---------|-------------|
| `sol quota scan` | Scan agent sessions for rate limit errors |
| `sol quota status` | Show per-account quota state |

Quota state is stored at `$SOL_HOME/.accounts/runtime/quota.json`. The scan command reads the bottom 20 lines of each agent's tmux pane and matches against known Claude rate limit error patterns.

## World Management

| Command | Description |
|---------|-------------|
| `sol world init <name>` | Initialize a new world |
| `sol world list` | List all worlds |
| `sol world status <name>` | Show world status with config |
| `sol world delete` | Delete a world |
| `sol world clone <source> <target>` | Clone a world |
| `sol world sync` | Sync the managed repo with its remote |
| `sol world import <archive>` | Import a world from an export archive |
| `sol world sleep <name>` | Mark a world as sleeping and stop its services |
| `sol world wake <name>` | Mark a world as active and start its services |
| `sol world summary <name>` | Show a world's governor-maintained summary |
| `sol world query <name> <question>` | Query a world's governor for information |
| `sol world export <name>` | Export a world to a tar.gz archive |

## Dispatch

| Command | Description |
|---------|-------------|
| `sol cast <work-item-id>` | Assign a work item to an agent and start its session |
| `sol tether <agent-name> <work-item-id>` | Bind a work item to an agent (any role) |
| `sol untether <agent-name>` | Unbind a work item from an agent (any role) |
| `sol prime` | Assemble and print execution context for an agent |
| `sol resolve` | Signal work completion — push branch, update state, clear tether |

`cast` accepts `--world` (or `SOL_WORLD` env), `--agent` (auto-selects idle if omitted), `--formula`, `--var`, and `--account` flags.

### Account resolution for credentials

When an agent session starts, credentials are symlinked from the resolved account's directory. Resolution priority:

1. `--account` flag on `sol cast` (per-dispatch override)
2. `default_account` in `world.toml` (per-world default)
3. `sol account default` (sphere-level default from registry)
4. `~/.claude/.credentials.json` (fallback when no accounts are configured)

## Agents

| Command | Description |
|---------|-------------|
| `sol agent create <name>` | Create an agent |
| `sol agent list` | List agents |
| `sol agent reset <name>` | Reset a stuck agent to idle state |
| `sol agent postmortem <name>` | Show diagnostic information for a dead or stuck agent |
| `sol agent handoffs` | Show recent handoff events |

## Store (Work Items)

| Command | Description |
|---------|-------------|
| `sol store create` | Create a work item |
| `sol store status <id>` | Show work item status |
| `sol store list` | List work items |
| `sol store update <id>` | Update a work item |
| `sol store close <id>` | Close a work item |
| `sol store query` | Run a read-only SQL query |

## Dependencies

| Command | Description |
|---------|-------------|
| `sol store dep add <from-id> <to-id>` | Add a dependency (from depends on to) |
| `sol store dep remove <from-id> <to-id>` | Remove a dependency |
| `sol store dep list <item-id>` | List dependencies for a work item |

## Sessions

| Command | Description |
|---------|-------------|
| `sol session start <name>` | Start a tmux session |
| `sol session stop <name>` | Stop a tmux session |
| `sol session list` | List all sessions |
| `sol session health <name>` | Check session health |
| `sol session capture <name>` | Capture pane output |
| `sol session attach <name>` | Attach to a tmux session |
| `sol session inject <name>` | Inject text into a session |

## Daemon Management

| Command | Description |
|---------|-------------|
| `sol up` | Start sphere daemons and world services |
| `sol down` | Stop sphere daemons and world services |

Without flags, `sol up` starts sphere daemons (prefect, consul, chronicle, ledger) and world services (sentinel, forge) for all non-sleeping worlds. `sol down` stops everything.

`--world` — manage only world services, skip sphere daemons. `--world=W` targets a specific world.

## Supervision

| Command | Description |
|---------|-------------|
| `sol prefect run` | Run the prefect (foreground) |
| `sol prefect stop` | Stop the running prefect |
| `sol status [world]` | Show sphere or world status |

## Sentinel (Per-World Health Monitor)

| Command | Description |
|---------|-------------|
| `sol sentinel run` | Run the sentinel patrol loop (foreground) |
| `sol sentinel start` | Start the sentinel as a background tmux session |
| `sol sentinel stop` | Stop the sentinel |
| `sol sentinel attach` | Attach to the sentinel tmux session |

## Merge Requests (Plumbing)

| Command | Description |
|---------|-------------|
| `sol mr create --world=W --branch=B --work-item=ID` | Create a merge request for an existing work item |

## Forge (Merge Pipeline)

| Command | Description |
|---------|-------------|
| `sol forge start` | Start the forge as a Claude session |
| `sol forge stop` | Stop the forge |
| `sol forge sync` | Sync forge worktree: fetch origin, reset to target branch |
| `sol forge attach` | Attach to the forge tmux session |
| `sol forge status <world>` | Show forge health summary |
| `sol forge queue` | Show the merge request queue |
| `sol forge pause` | Pause the forge — stop claiming new MRs |
| `sol forge resume` | Resume the forge — start claiming MRs again |

Toolbox subcommands (used by the forge Claude session):

| Command | Description |
|---------|-------------|
| `sol forge ready` | List ready (unblocked) merge requests |
| `sol forge blocked` | List blocked merge requests |
| `sol forge claim` | Claim the next ready unblocked merge request |
| `sol forge release <mr-id>` | Release a claimed merge request back to ready |
| `sol forge mark-merged <mr-id>` | Mark a merge request as merged |
| `sol forge mark-failed <mr-id>` | Mark a merge request as failed |
| `sol forge create-resolution <mr-id>` | Create a conflict resolution task and block the MR |
| `sol forge check-unblocked` | Check for resolved blockers and unblock MRs |
| `sol forge await` | Block until a nudge arrives or timeout expires |

## Messaging

| Command | Description |
|---------|-------------|
| `sol mail send` | Send a message |
| `sol mail inbox` | List pending messages |
| `sol mail read <message-id>` | Read a message (marks as read) |
| `sol mail ack <message-id>` | Acknowledge a message |
| `sol mail check` | Count unread messages |

## Nudge Queue (Inbox)

| Command | Description |
|---------|-------------|
| `sol inbox` | View pending nudge queue messages |
| `sol inbox count` | Print count of pending messages |
| `sol inbox drain` | Drain and display all pending messages |

Nudge queue counts are also shown in the NUDGE column of `sol status --world=W` agent and envoy tables.

## Escalations

| Command | Description |
|---------|-------------|
| `sol escalate <description>` | Create an escalation |
| `sol escalation list` | List escalations |
| `sol escalation ack <id>` | Acknowledge an escalation |
| `sol escalation resolve <id>` | Resolve an escalation |

## Observability

| Command | Description |
|---------|-------------|
| `sol feed` | View the event activity feed |
| `sol log-event` | Log a custom event to the event feed (plumbing) |
| `sol chronicle run` | Run the chronicle (foreground) |
| `sol chronicle start` | Start the chronicle as a background tmux session |
| `sol chronicle stop` | Stop the chronicle background session |

## Ledger (Token Tracking)

| Command | Description |
|---------|-------------|
| `sol ledger run` | Run the ledger OTLP receiver (foreground) |
| `sol ledger start` | Start the ledger as a background tmux session |
| `sol ledger stop` | Stop the ledger background session |

Sphere-scoped OTLP HTTP receiver on port 4318. Accepts `claude_code.api_request` log events from Claude Code agent sessions, extracts token counts (input, output, cache_read, cache_creation) and model, and writes `token_usage` records to the appropriate world database. Source agent identification via `OTEL_RESOURCE_ATTRIBUTES` (agent.name, world, work_item_id) injected at cast time.

## Workflows

| Command | Description |
|---------|-------------|
| `sol workflow instantiate <formula>` | Instantiate a workflow from a formula |
| `sol workflow manifest <formula>` | Manifest a formula into work items and a caravan |
| `sol workflow current` | Print the current step's instructions |
| `sol workflow advance` | Advance to the next workflow step |
| `sol workflow status` | Show workflow status |

## Caravans

| Command | Description |
|---------|-------------|
| `sol caravan create <name> [<item-id> ...]` | Create a caravan with optional initial items |
| `sol caravan add <caravan-id> <item-id> [<item-id> ...]` | Add items to an existing caravan |
| `sol caravan list` | List caravans with optional status filtering |
| `sol caravan check <caravan-id>` | Check readiness of caravan items |
| `sol caravan status [<caravan-id>]` | Show caravan status |
| `sol caravan launch <caravan-id>` | Dispatch ready items in a caravan |
| `sol caravan commission <caravan-id>` | Commission a caravan (drydock → open) |
| `sol caravan drydock <caravan-id>` | Return a caravan to drydock (open → drydock) |
| `sol caravan set-phase <caravan-id> [<item-id>] <phase>` | Update the phase of items in a caravan |
| `sol caravan close [<caravan-id>]` | Close a completed caravan |
| `sol caravan dep add <caravan-id> <depends-on-caravan-id>` | Declare that a caravan depends on another caravan being closed |
| `sol caravan dep remove <caravan-id> <depends-on-caravan-id>` | Remove a caravan dependency |
| `sol caravan dep list <caravan-id>` | Show caravan-level dependencies |

## Agent Memories

| Command | Description |
|---------|-------------|
| `sol remember [key] <value>` | Persist a memory for the current agent |
| `sol memories` | List all memories for the current agent |
| `sol forget [key]` | Delete a memory for the current agent |

Memories are key-value pairs stored in the world database, scoped to each agent name. They survive across sessions and handoffs. With a single argument, `sol remember` auto-generates a key from a hash of the value. Memories are injected during prime so successor sessions see them automatically.

## Handoff (Session Continuity)

| Command | Description |
|---------|-------------|
| `sol handoff` | Hand off to a fresh session with context preservation |

`--summary` provides a progress summary. `--reason` tags the handoff with a reason (`compact`, `manual`, `health-check`; defaults to `unknown`). Captures tmux output, git state, and workflow progress into `.handoff.json`, then cycles the session atomically using `tmux respawn-pane`. Safe for self-handoff (agent calling handoff on itself) and PreCompact auto-handoff — the old process is replaced without destroying the session. Each handoff emits a chronicle event with reason, session age, and role for observability. When reason is `compact`, the new session uses `--continue` and gets a lightweight prime that omits the full work item description.

## Envoy (Persistent Human-Directed Agents)

| Command | Description |
|---------|-------------|
| `sol envoy create <name>` | Create an envoy agent |
| `sol envoy start <name>` | Start an envoy session |
| `sol envoy stop <name>` | Stop an envoy session |
| `sol envoy attach <name>` | Attach to an envoy's tmux session |
| `sol envoy list` | List envoy agents |
| `sol envoy brief <name>` | Display an envoy's brief |
| `sol envoy debrief <name>` | Archive the envoy's brief and reset for fresh engagement |
| `sol envoy sync <name>` | Sync managed repo and notify a running envoy session |
| `sol envoy delete <name>` | Delete an envoy agent and all associated resources |

## Governor (Per-World Coordinator)

| Command | Description |
|---------|-------------|
| `sol governor start` | Start the governor for a world |
| `sol governor stop` | Stop the governor for a world |
| `sol governor attach` | Attach to the governor's tmux session |
| `sol governor brief` | Display the governor's brief |
| `sol governor debrief` | Archive the governor's brief and reset |
| `sol governor summary` | Display the governor's world summary |
| `sol governor sync` | Sync managed repo the governor reads from |

## Nudge (Inter-Agent Notifications)

| Command | Description |
|---------|-------------|
| `sol nudge drain` | Drain pending nudge messages for an agent session |

## Brief (Agent Context)

| Command | Description |
|---------|-------------|
| `sol brief inject` | Inject brief into session context |

## Consul (Sphere-Level Patrol)

| Command | Description |
|---------|-------------|
| `sol consul run` | Run the consul patrol loop (foreground) |
| `sol consul status` | Show consul status from heartbeat |

`consul run` accepts `--interval` (default 5m), `--stale-timeout` (default 1h), and `--webhook` for escalation notifications.

## Senate (Sphere-Scoped Planner)

| Command | Description |
|---------|-------------|
| `sol senate start` | Start the senate planning session |
| `sol senate stop` | Stop the senate session |
| `sol senate attach` | Attach to the senate tmux session |
| `sol senate brief` | Display the senate's brief |
| `sol senate debrief` | Archive the senate's brief and reset |

Senate is an operator-managed sphere-scoped planning session. It reads governor world summaries via `sol world summary` and queries governors via `sol world query`. Not supervised by prefect — start and stop manually.

## Service (Systemd Units)

| Command | Description |
|---------|-------------|
| `sol service install` | Generate and install systemd user units (enable but don't start) |
| `sol service uninstall` | Stop, disable, and remove systemd user units |
| `sol service start` | Start all sol sphere daemon units |
| `sol service stop` | Stop all sol sphere daemon units |
| `sol service restart` | Restart all sol sphere daemon units |
| `sol service status` | Show status of sol sphere daemon units |

Linux-only. Manages systemd user units for sol sphere daemons (prefect, consul, chronicle, ledger).

## Quota (Rate Limit Rotation)

| Command | Description |
|---------|-------------|
| `sol quota rotate` | Rotate rate-limited agents to available accounts |

Reads quota state from `$SOL_HOME/.runtime/quota.json` to find rate-limited accounts, selects available accounts via LRU, swaps credential symlinks, and respawns agent sessions with `--continue` for context preservation. When no accounts are available, agents are paused and automatically restarted by the sentinel when accounts become available.

## Guard (PreToolUse Hooks)

| Command | Description |
|---------|-------------|
| `sol guard dangerous-command` | Block dangerous commands (rm -rf, force push, hard reset, etc.) |
| `sol guard workflow-bypass` | Block commands that circumvent the forge merge pipeline |

Guards are called by PreToolUse hooks in `.claude/settings.local.json`. They read tool input from stdin (Claude Code hook protocol) and exit 2 to block, 0 to allow. `workflow-bypass` respects `SOL_ROLE` — forge is exempt since it pushes to the target branch for merges.

## Documentation

| Command | Description |
|---------|-------------|
| `sol docs generate` | Generate CLI reference documentation |
