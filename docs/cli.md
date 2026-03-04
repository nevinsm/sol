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

## World Management

| Command | Description |
|---------|-------------|
| `sol world init <name>` | Initialize a new world |
| `sol world list` | List all worlds |
| `sol world status <name>` | Show world status with config |
| `sol world delete` | Delete a world |
| `sol world sync` | Sync the managed repo with its remote |

## Dispatch

| Command | Description |
|---------|-------------|
| `sol cast <work-item-id>` | Assign a work item to an agent and start its session |
| `sol tether <agent-name> <work-item-id>` | Bind a work item to an agent (any role) |
| `sol untether <agent-name>` | Unbind a work item from an agent (any role) |
| `sol prime` | Assemble and print execution context for an agent |
| `sol resolve` | Signal work completion — push branch, update state, clear tether |

`cast` accepts `--world` (or `SOL_WORLD` env), `--agent` (auto-selects idle if omitted), `--formula`, and `--var` flags.

## Agents

| Command | Description |
|---------|-------------|
| `sol agent create <name>` | Create an agent |
| `sol agent list` | List agents |
| `sol agent reset <name>` | Reset a stuck agent to idle state |

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
| `sol up` | Start sphere-level daemons (prefect, consul, chronicle) |
| `sol down` | Stop sphere-level daemons (prefect, consul, chronicle) |

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

Toolbox subcommands (used by the forge Claude session):

| Command | Description |
|---------|-------------|
| `sol forge ready` | List ready (unblocked) merge requests |
| `sol forge blocked` | List blocked merge requests |
| `sol forge claim` | Claim the next ready unblocked merge request |
| `sol forge release <mr-id>` | Release a claimed merge request back to ready |
| `sol forge run-gates` | Run quality gates in the forge worktree |
| `sol forge push` | Push HEAD to target branch (acquires merge slot) |
| `sol forge mark-merged <mr-id>` | Mark a merge request as merged |
| `sol forge mark-failed <mr-id>` | Mark a merge request as failed |
| `sol forge create-resolution <mr-id>` | Create a conflict resolution task and block the MR |
| `sol forge check-unblocked` | Check for resolved blockers and unblock MRs |

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

## Workflows

| Command | Description |
|---------|-------------|
| `sol workflow instantiate <formula>` | Instantiate a workflow from a formula |
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
| `sol caravan set-phase <caravan-id> [<item-id>] <phase>` | Update the phase of items in a caravan |
| `sol caravan close [<caravan-id>]` | Close a completed caravan |

## Handoff (Session Continuity)

| Command | Description |
|---------|-------------|
| `sol handoff` | Hand off to a fresh session with context preservation |

`--summary` provides a progress summary. Captures tmux output, git state, and workflow progress into `.handoff.json`, then cycles the session atomically using `tmux respawn-pane`. Safe for self-handoff (agent calling handoff on itself) and PreCompact auto-handoff — the old process is replaced without destroying the session.

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

## Documentation

| Command | Description |
|---------|-------------|
| `sol docs generate` | Generate CLI reference documentation |
