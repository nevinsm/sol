# Sol Command Reference — External Collaborator

This document covers the commands available to external collaborators. Commands marked as "NOT for external use" must never be run from an external session.

## Status (Read-Only)

### `sol status`

Sphere overview. Shows running worlds, active agents, sphere processes, and health.

### `sol status <world>`

World detail. Shows writs (by state), agents, forge status, and sentinel health for a specific world.

### `sol writ list --world=<world>`

List writs in a world. Shows ID, title, state, kind, priority, and labels.

Useful flags:
- `--status=<status>` — filter by status (open, tethered, working, done, closed)
- `--label=<label>` — filter by label
- `--json` — output as JSON

### `sol writ status <id> --world=<world>`

Detailed view of a single writ. Shows description, state history, assigned agent, branch name, and dependencies.

### `sol caravan list`

List all caravans across the sphere. Shows ID, name, state, item count, and progress.

### `sol caravan status <id>`

Detailed view of a caravan. Shows items grouped by phase, each item's writ state, and overall progress.

### `sol forge queue --world=<world>`

View the forge merge queue. Shows branches waiting to be merged, currently processing, and recent results.

## Create and Dispatch

### `sol writ create --world=<world>`

Create a new writ.

Required flags:
- `--world=<world>` — target world
- `--title="..."` — short title

Optional flags:
- `--description="..."` — detailed description (supports multi-line)
- `--kind=code|analysis` — writ kind (default: code)
- `--priority=1|2|3` — priority level (default: 2)
- `--label=<label>` — add a label (can be repeated)

Example:

```bash
sol writ create --world=myproject \
  --title="Add rate limiting" \
  --description="Implement token bucket rate limiting on all API endpoints" \
  --kind=code \
  --priority=2 \
  --label=api --label=security
```

### `sol cast <writ-id> --world=<world>`

Dispatch a writ to an agent. Creates a worktree, tethers the writ, and starts an AI session.

Optional flags:
- `--agent=<name>` — target a specific agent (default: sol picks one)

The writ must be in the `open` state. After casting, it moves to `tethered`.

### `sol caravan create "name" <id> [<id>...] --world=<world>`

Create a caravan from existing writs. All writs start in phase 0 by default.

### `sol caravan set-phase <caravan-id> <item-id> <phase>`

Assign a caravan item to a phase. Phase is a non-negative integer. Items in the same phase run in parallel. Phase N waits for all prior phases to complete.

### `sol caravan commission <caravan-id>`

Commission a caravan — make it live and dispatchable. Once commissioned, sol begins dispatching phase-0 items.

## Communication

### `sol mail send`

Send an asynchronous message to an agent or the operator.

Required flags:
- `--to=<recipient>` — agent name or "autarch" for the operator
- `--subject="..."` — message subject

Optional flags:
- `--body="..."` — message body
- `--priority=1|2|3` — message priority (default: 2)

Example:

```bash
sol mail send --to=Toast --subject="API question" \
  --body="Should the rate limiter use a fixed or sliding window?"
```

### `sol escalate "description"`

Escalate a blocker to the operator. Use this when you encounter a problem that requires human intervention. The escalation appears in `sol inbox`.

## NOT for External Use

The following commands are internal to sol-managed agents. **Do not use them.**

| Command | Why it's off-limits |
|---------|-------------------|
| `sol resolve` | Clears agent tethers, pushes branches — only for sol-managed agents |
| `sol tether <writ-id>` | Internal agent-writ binding — managed by `sol cast` |
| `sol untether <writ-id>` | Internal tether removal — managed by `sol resolve` |
| `sol handoff` | Internal session cycling for context exhaustion — sol-managed only |
| `sol agent reset <name>` | Internal agent recovery — restarts a stuck agent |
| `sol prime` | Internal context injection on session start |

Using these commands from an external session will corrupt sol's internal state. If you need to stop or restart an agent's work, use `sol writ` commands to manage the writ state, or escalate to the operator.
