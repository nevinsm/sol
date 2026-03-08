# ADR-0025: Tether Evolution — Multi-Writ Persistent Agents

Status: accepted
Date: 2026-03-08

## Context

The tether was designed as a single file binding one outpost agent to one
writ. This model served Arc 1–2 perfectly: each outpost gets cast a writ,
executes it, resolves, and goes idle.

Arc 3 introduced persistent agents — envoys (human-directed), governors
(per-world coordinators), and the senate (sphere-scoped planner). These
agents maintain context across sessions and operate on multiple writs
concurrently. An envoy might be actively working on a design review while
two other writs are queued for its attention. A governor coordinates
dispatch across many writs simultaneously.

The single-file tether cannot represent this. A persistent agent needs:

1. **Multiple concurrent tethers** — more than one writ bound at a time.
2. **Active writ selection** — the operator chooses which tethered writ
   the agent focuses on. Only one writ can be active because Claude Code
   caches the system prompt and cannot rewrite it mid-session.
3. **Lightweight binding** — tethering a writ to a persistent agent should
   not create a worktree or start a session (unlike `sol cast` for outposts).

### Why not just use the database?

The tether's original purpose was durability — a file that survives crashes
and tells the agent what to do on restart. Moving this to a database column
would violate the CRASH principle: an agent with a tether file and a worktree
needs nothing else. The database is for coordination; the tether is for
execution. Persistent agents need both: the directory for crash recovery
(which writs am I responsible for?) and the database for active writ
tracking (which writ am I currently working on?).

## Decision

### Tether becomes a directory

Replace the single `.tether` file with a `.tether/` directory. Each
tethered writ gets its own file within the directory, named by writ ID:

```
$SOL_HOME/{world}/envoys/{name}/.tether/
├── sol-abc123def456
├── sol-789012345678
└── sol-fedcba987654
```

The tether package (`internal/tether/`) provides directory-aware operations:

- **`Write(world, agent, writID, role)`** — creates the directory
  (MkdirAll) and writes a writ file. Uses fsync-before-rename for
  durability.
- **`List(world, agent, role)`** — returns all tethered writ IDs.
- **`ClearOne(world, agent, writID, role)`** — removes a single tether.
- **`Clear(world, agent, role)`** — removes all tethers.
- **`IsTethered(world, agent, role)`** — checks if any tethers exist.
- **`IsTetheredTo(world, agent, writID, role)`** — checks specific writ.
- **`Migrate(world, agent, role)`** — converts legacy single-file tether
  to directory format (one-time, idempotent).

All roles use the directory model — outposts, envoys, governors. For
outposts, the directory contains exactly one file (same behavior as before,
just structured differently).

### Active writ tracked in sphere DB

The `agents` table in the sphere database has an `active_writ` column
(TEXT, nullable). This tracks which tethered writ the agent is currently
focused on. Updated by:

- **`sol cast`** — always sets active_writ to the dispatched writ.
- **`sol tether`** — sets active_writ only if none is currently set
  (first tether activates; subsequent tethers are background).
- **`sol writ activate`** — explicitly switches the active writ.
- **`sol untether`** — clears active_writ if the untethered writ was active.
- **`sol resolve`** — clears active_writ when work completes.

### Three dispatch operations

**`sol cast <writ-id>`** — full dispatch for outpost agents. Creates
worktree, writes tether, sets active_writ, launches session. Unchanged
from before except tether.Write() now creates a directory.

**`sol tether <writ-id> --agent=<name>`** — lightweight binding for
persistent agents (envoy, governor, forge). Creates tether file only.
No worktree, no session. Sets active_writ only if the agent has no
current active writ.

**`sol untether <writ-id> --agent=<name>`** — unbinds a writ from a
persistent agent. Removes the tether file. If no tethers remain, agent
goes idle.

### Writ switching via session restart

**`sol writ activate <writ-id>`** switches the active writ for a
persistent agent. Because Claude Code caches the system prompt at session
start and cannot rewrite it mid-session, activation triggers a session
restart:

1. Update active_writ in DB.
2. Write a `.resume_state.json` with writ-switch context.
3. Call `startup.Resume()` with `--continue` for conversation continuity.
4. The fresh session gets a new persona and prime with the activated writ's
   context.

This reuses the same resume mechanism as PreCompact handoffs (ADR-0023).
The agent's brief survives (it's role-scoped, not writ-scoped), so
accumulated context is preserved.

### GUPP adaptation for persistent agents

The GUPP principle — "if you find something on your tether, YOU RUN IT" —
was designed for outpost agents that fire on session start. Persistent
agents adapt this principle:

- **Outposts**: fire on session start. Unchanged. The tether directory
  contains one file, and the agent executes it immediately.
- **Persistent agents**: fire on operator direction. When the operator
  calls `sol writ activate`, the agent receives the writ context and
  executes immediately. No confirmation loop, no polling.

Propulsion is preserved — agents execute immediately when directed. The
trigger changes (session start vs operator command) but the principle holds:
the tether IS the instruction, and the agent runs it without delay.

### Status display

`sol status --world` shows multi-tether state for persistent agents:

- **Active writ**: title of the currently active writ.
- **Background tethers**: count of other tethered writs, shown as
  `[+N tethered]` after the work title.
- **Outposts**: unchanged. Single tether, same display as before.

Example envoy table line:
```
Scout    working    alive    Design review [+2 tethered]    45m ago
```

## Consequences

- **All roles use tether directories.** Consistency — no conditional logic
  for "is this a file or directory?" The Migrate() function handles the
  one-time transition for existing deployments.

- **Three dispatch commands cover the full lifecycle.** `cast` for outposts
  (full dispatch), `tether`/`untether` for persistent agents (lightweight
  binding), `writ activate` for focus switching.

- **Lightweight tether path for persistent agents.** Tethering a writ to
  an envoy is a single file write — no worktree creation, no session
  management, sub-millisecond. This supports batch operations where
  multiple writs are tethered to an agent in rapid succession.

- **Brief stays role-level, output goes in writ directory.** An envoy's
  brief accumulates context across all writs. Per-writ output goes in the
  writ output directory (`$SOL_HOME/{world}/writ-outputs/{writ-id}/`).
  This separation avoids writ-scoped brief management complexity.

- **Writ switching requires session restart.** This is a Claude Code
  constraint (cached system prompt), not a design choice. The latency is
  acceptable (session restart takes ~5s) and the `--continue` flag
  preserves conversation history.

- **Crash recovery is straightforward.** The tether directory survives
  crashes. On restart, `tether.List()` shows all bound writs. The
  `active_writ` column may be stale after a crash, but the startup
  sequence can derive the correct state from the tether directory and
  resume state file.
