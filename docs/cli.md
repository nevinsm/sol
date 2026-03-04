# CLI Reference

## Setup

| Command | Description |
|---------|-------------|
| `sol init` | First-time setup. Creates SOL_HOME, first world. `--name` and `--source-repo` for flag mode, `--guided` for Claude-powered setup. |
| `sol doctor` | Check system prerequisites (tmux, git, claude, SOL_HOME, SQLite WAL). `--json` for machine-readable output. |

## World Management

| Command | Description |
|---------|-------------|
| `sol world init <name>` | Create a world (database, directory tree, config). `--source-repo` associates a git repository. |
| `sol world list` | List all registered worlds. `--json` for machine-readable output. |
| `sol world status <name>` | Show world status including config, agents, work items, and health. `--json` supported. |
| `sol world delete <name>` | Delete a world and all associated data. Requires `--confirm`. Refuses if sessions are active. |
| `sol world sync <name>` | Fetch and pull latest from the managed repo's origin. Clones if repo doesn't exist yet. `--all` also syncs forge, envoys, and governor. |

## Dispatch

| Command | Description |
|---------|-------------|
| `sol cast <item-id> <world>` | Assign work to an agent, create worktree, start session |
| `sol tether <agent> <item-id> --world=W` | Bind a work item to an agent (any role). Lightweight alternative to `cast` for persistent agents that already have worktrees. |
| `sol untether <agent> --world=W` | Unbind a work item from an agent (any role). Resets agent to idle and work item to open. |
| `sol prime --world=W --agent=A` | Assemble and print execution context for an agent |
| `sol resolve --world=W --agent=A` | Signal completion: push branch, update state, clear tether |

`cast` accepts `--agent` (auto-selects idle if omitted), `--formula`, and `--var` flags.

## Agents

| Command | Description |
|---------|-------------|
| `sol agent create <name> --world=W` | Create an agent (default role: agent) |
| `sol agent list --world=W` | List agents in a world |
| `sol agent reset <name> --world=W` | Reset a stuck agent to idle (clears tether, untethers work item) |

## Store (Work Items)

| Command | Description |
|---------|-------------|
| `sol store create --world=W --title=T` | Create a work item |
| `sol store get <id> --world=W` | Get a work item by ID |
| `sol store list --world=W` | List work items (filter by `--status`, `--label`, `--assignee`) |
| `sol store update <id> --world=W` | Update status, assignee, priority, title, or description |
| `sol store close <id> --world=W` | Close a work item |
| `sol store query --world=W --sql=Q` | Run a read-only SQL query |

## Dependencies

| Command | Description |
|---------|-------------|
| `sol store dep add <from> <to> --world=W` | Add a dependency (from depends on to) |
| `sol store dep remove <from> <to> --world=W` | Remove a dependency |
| `sol store dep list <id> --world=W` | List dependencies for a work item |

## Sessions

| Command | Description |
|---------|-------------|
| `sol session start <name>` | Start a tmux session |
| `sol session stop <name>` | Stop a tmux session |
| `sol session list` | List all sessions |
| `sol session health <name>` | Check session health |
| `sol session capture <name>` | Capture pane output |
| `sol session attach <name>` | Attach to a session |
| `sol session inject <name> --message=M` | Inject text and press Enter. `--no-submit` to stage only. |

## Supervision

| Command | Description |
|---------|-------------|
| `sol prefect run` | Run the prefect (foreground). `--consul` enables sphere-level patrol. |
| `sol prefect stop` | Stop the running prefect |
| `sol status <world>` | Show world status (exit code reflects health) |

## Sentinel (Per-World Health Monitor)

| Command | Description |
|---------|-------------|
| `sol sentinel run <world>` | Run the sentinel patrol loop (foreground) |
| `sol sentinel start <world>` | Start sentinel as background tmux session |
| `sol sentinel stop <world>` | Stop the sentinel |
| `sol sentinel attach <world>` | Attach to the sentinel session |

## Merge Requests (Plumbing)

| Command | Description |
|---------|-------------|
| `sol mr create --world=W --branch=B --work-item=ID` | Create a merge request manually. `--priority` (1-3, default from work item). `--json` supported. |

## Forge (Merge Pipeline)

| Command | Description |
|---------|-------------|
| `sol forge start <world>` | Start the forge as a Claude session |
| `sol forge stop <world>` | Stop the forge |
| `sol forge sync <world>` | Sync forge worktree: fetch origin, reset to target branch. Also syncs managed repo. |
| `sol forge attach <world>` | Attach to the forge session |
| `sol forge queue <world>` | Show the merge request queue |

Toolbox subcommands (used by the forge Claude session):

| Command | Description |
|---------|-------------|
| `sol forge ready <world>` | List ready merge requests |
| `sol forge blocked <world>` | List blocked merge requests |
| `sol forge claim <world>` | Claim the next ready MR |
| `sol forge release <world> <mr-id>` | Release a claimed MR back to ready |
| `sol forge run-gates <world>` | Run quality gates |
| `sol forge push <world>` | Push to target branch |
| `sol forge mark-merged <world> <mr-id>` | Mark MR as merged |
| `sol forge mark-failed <world> <mr-id>` | Mark MR as failed |
| `sol forge create-resolution <world> <mr-id>` | Create conflict resolution task |
| `sol forge check-unblocked <world>` | Check for resolved blockers |

## Messaging

| Command | Description |
|---------|-------------|
| `sol mail send --to=R --subject=S` | Send a message |
| `sol mail inbox` | List pending messages |
| `sol mail read <msg-id>` | Read a message (marks as read) |
| `sol mail ack <msg-id>` | Acknowledge a message |
| `sol mail check` | Count unread messages (exit 1 if unread) |

## Nudge Queue (Inbox)

| Command | Description |
|---------|-------------|
| `sol inbox` | List pending nudge messages for an agent. `--world`, `--agent` (defaults to `SOL_WORLD`/`SOL_AGENT` env). `--json` supported. |
| `sol inbox count` | Print count of pending messages (for scripting/status display) |
| `sol inbox drain` | Drain and display all pending messages, marking them claimed. `--json` supported. |

Nudge queue counts are also shown in the NUDGE column of `sol status <world>` agent and envoy tables.

## Escalations

| Command | Description |
|---------|-------------|
| `sol escalate <description>` | Create an escalation (`--severity`: low/medium/high/critical) |
| `sol escalation list` | List escalations (`--status`: open/acknowledged/resolved) |
| `sol escalation ack <id>` | Acknowledge an escalation |
| `sol escalation resolve <id>` | Resolve an escalation |

## Observability

| Command | Description |
|---------|-------------|
| `sol feed` | View event feed (`-f` follow, `-n` limit, `--since`, `--type`) |
| `sol log-event --type=T --actor=A` | Log a custom event (plumbing) |
| `sol chronicle run` | Run the event chronicle (foreground) |
| `sol chronicle start` | Start chronicle as background session |
| `sol chronicle stop` | Stop the chronicle |

## Workflows

| Command | Description |
|---------|-------------|
| `sol workflow instantiate <formula>` | Instantiate a workflow from a formula |
| `sol workflow current --world=W --agent=A` | Print current step instructions |
| `sol workflow advance --world=W --agent=A` | Advance to next step |
| `sol workflow status --world=W --agent=A` | Show workflow progress |

## Caravans

| Command | Description |
|---------|-------------|
| `sol caravan create <name> [items...]` | Create a caravan with optional items |
| `sol caravan add <caravan-id> <items...>` | Add items to a caravan |
| `sol caravan check <caravan-id>` | Check readiness of caravan items |
| `sol caravan status [caravan-id]` | Show caravan status |
| `sol caravan launch <caravan-id> --world=W` | Dispatch ready items in a caravan |
| `sol caravan close <caravan-id>` | Close a completed caravan. `--force` skips merged check. `--auto` (no ID) closes all fully-merged caravans. |

## Handoff (Session Continuity)

| Command | Description |
|---------|-------------|
| `sol handoff --world=W --agent=A` | Hand off to a fresh session with context preservation |

`--summary` provides a progress summary. Captures tmux output, git state, and workflow progress into `.handoff.json`, then cycles the session atomically using `tmux respawn-pane`. Safe for self-handoff (agent calling handoff on itself) and PreCompact auto-handoff — the old process is replaced without destroying the session.

## Envoy (Persistent Human-Directed Agents)

| Command | Description |
|---------|-------------|
| `sol envoy create <name> --world=W` | Create an envoy agent with persistent worktree |
| `sol envoy start <name> --world=W` | Start an envoy session |
| `sol envoy stop <name> --world=W` | Stop an envoy session |
| `sol envoy attach <name> --world=W` | Attach to an envoy's tmux session |
| `sol envoy list` | List envoy agents. `--world` filters by world, `--json` for machine-readable output. |
| `sol envoy brief <name> --world=W` | Display an envoy's brief |
| `sol envoy debrief <name> --world=W` | Archive the envoy's brief and reset for fresh engagement |
| `sol envoy sync <name> --world=W` | Sync managed repo and notify running envoy session. Does not rebase envoy branch. |
| `sol envoy delete <name> --world=W` | Delete an envoy agent, worktree, branch, and store record. Refuses if session active or tethered unless `--force`. |

## Governor (Per-World Coordinator)

| Command | Description |
|---------|-------------|
| `sol governor start --world=W` | Start the governor for a world |
| `sol governor stop --world=W` | Stop the governor for a world |
| `sol governor attach --world=W` | Attach to the governor's tmux session |
| `sol governor brief --world=W` | Display the governor's brief |
| `sol governor debrief --world=W` | Archive the governor's brief and reset |
| `sol governor summary --world=W` | Display the governor's world summary |
| `sol governor sync --world=W` | Sync managed repo the governor reads from. Notifies running governor session. |

## Nudge (Inter-Agent Notifications)

| Command | Description |
|---------|-------------|
| `sol nudge drain --world=W --agent=A` | Drain pending nudge messages for an agent session. Prints formatted notifications to stdout, runs cleanup. Silent no-op if queue is empty. Used by UserPromptSubmit hooks. |

## Brief (Agent Context)

| Command | Description |
|---------|-------------|
| `sol brief inject --path=P` | Inject brief into session context. Used by Claude Code hooks. `--max-lines` (default 200). |

## Consul (Sphere-Level Patrol)

| Command | Description |
|---------|-------------|
| `sol consul run` | Run the consul patrol loop (foreground) |
| `sol consul status` | Show consul status from heartbeat |

`consul run` accepts `--interval` (default 5m), `--stale-timeout` (default 1h), and `--webhook` for escalation notifications.
