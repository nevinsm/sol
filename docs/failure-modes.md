# Failure Modes and Recovery

Every component in sol has a defined crash recovery path. This is a first-class
design requirement (the CRASH principle), not optional hardening.

The core invariant: **an agent with work on its tether and a local worktree
needs nothing else to execute.** The entire coordination layer can be down and
in-flight work continues. Recovery happens when services return.

## Recovery Matrix

| Component | State Survives | State Lost | Recovery Action | Recovery Time |
|-----------|---------------|------------|-----------------|---------------|
| Store (SQLite) | DB file (WAL journal) | Open transactions | Reopen DB (WAL recovery) | <1s |
| Session Manager | Session metadata files | tmux server memory | Prefect restarts sessions | <3 min |
| Mail | `messages` table | In-flight INSERT | Re-derive from DB | <1s |
| Workflow Engine | `state.json`, step files | In-memory step state | Re-read state.json on restart | <1s |
| Prefect | PID file, session registry | Heartbeat loop state | Restart prefect (systemd/launchd) | <10s |
| Consul | Heartbeat file | Patrol cycle state | Prefect restarts, re-patrols | <3 min |
| Sentinel | Patrol state file | Current patrol cycle | Prefect restarts, re-patrols | <3 min |
| Forge | `merge_requests` table, slot lock | In-progress merge | Prefect restarts Claude session; TTL expiry releases claimed MR | <30 min |
| Outpost | Tether file, worktree, identity | Session memory | `sol prime` re-injects context (GUPP) | <30s |
| Event Feed | JSONL files | Chronicle buffer | Chronicle restarts, tails from last position | <10s |

## Graceful Degradation

When a subsystem is down, the system continues in reduced capacity rather than
halting.

| Subsystem Down | System Behavior |
|----------------|-----------------|
| SQLite store | Agents with tethered work continue executing (tether is a local file). New dispatch fails. Pending messages unavailable. |
| Prefect | Running agents continue. No crash recovery or new spawns. |
| Sentinel | Outposts work normally. Stalled agents aren't detected until restart. |
| Forge | Work accumulates in merge queue. No merges land. |
| Consul | Stale tethers accumulate. Caravans with ready work wait. Resolved on restart. |
| Network/git remote | Agents work locally. `sol resolve` push phase retries. |

## Per-Component Details

### Store (SQLite)

If the database file is corrupted or locked, operations that require
coordination state fail. Agents with tethered work continue executing. The
store is a file, so corruption recovery is `cp backup.db store.db`.

### Session Manager (tmux)

If the tmux server crashes, all sessions die. The prefect detects this (all PID
checks fail simultaneously) and enters degraded mode. Recovery: prefect restarts
the tmux server, then restarts agents. Tethered work is durable — agents
recover via GUPP on restart.

### Mail

Same as store — if SQLite is unavailable, message writes fail. Agents with
tethered work continue executing. Pending messages are also unavailable when
the store is down.

### Workflow Engine

If a workflow state file is corrupted, the agent loses its place. Recovery: the
operator or prefect re-reads the step directory to reconstruct state. Each
step's completion is idempotent — re-running a completed step is safe.

### Prefect

If the prefect crashes, running agents continue unaffected. No new agents are
spawned, and crashed agents are not recovered. The operator must restart the
prefect. A system-level service manager (systemd or launchd) can restart the
prefect process itself.

### Consul

If the consul crashes, the prefect restarts it. While down: stale tethers
accumulate (resolved on restart), caravans with ready work wait (dispatched
on restart). No data loss.

### Sentinel

If the sentinel crashes, the prefect restarts it. While down: crashed outposts
are not respawned at the work level (prefect handles session restarts, but
sentinel handles work-level recovery like returning work to the open pool after
max respawns). In-memory state (respawn counts, output hashes) is lost on crash
and re-derived on restart. No data loss.

### Forge

If the forge session crashes, the prefect restarts it. Claimed merge requests
with expired TTL (30 min) are automatically released for re-claim. No merges
land while down; the queue accumulates.

### Outpost (Worker Agent)

Session crash: work remains tethered, worktree preserved. On restart,
`sol prime` reads tether, workflow state, and pending messages to reconstruct
context. The agent resumes from last durable state. It doesn't know it
crashed — it just sees its tether and gets to work (GUPP).

### Event Feed / Chronicle

Event logging is best-effort — failures are silently ignored. If the chronicle
crashes, the raw log continues growing and the curated feed is stale. The
prefect restarts the chronicle. No primary operations are affected.

### Non-Code Writ Resolve

Non-code writs (kind != "code") follow a simpler resolve path with fewer
failure points. There is no branch push, no MR creation, and no forge
involvement. The writ transitions directly to closed status.

**What can fail:**
- Database write to close the writ (same as any store operation — retry on restart)
- Tether clear (file deletion — best-effort, consul catches stale tethers)

**What cannot fail:** No git push (no network dependency), no MR creation
(no forge dependency), no squash-merge (the failure that motivated this design).

Recovery is simpler than code writs: if the agent crashes mid-resolve, the writ
remains tethered. On respawn, the agent re-reads its tether (GUPP) and resolves
again. Since resolve is idempotent for non-code writs (close is a status update),
no duplicate MRs are created.

### Writ Closed While Agent Is Working

When a governor closes a writ (cancelled, superseded, etc.) while an agent is
actively working on it, the sentinel detects the closed writ on its next patrol
cycle (≤60s). The agent's session is stopped, its tether cleared, and the
outpost reaped. Agent work in progress is lost (acceptable — the writ was
cancelled).

This applies to both code and non-code writs. When a non-code writ resolves
(closing it directly), any other agent that happens to be tethered to it is
reaped by sentinel on the next patrol — same mechanism, same timing.

## Mass Failure

When 3+ agent sessions die within 30 seconds, the prefect enters degraded mode:
it logs the event, stops respawning agents, and notifies the operator. This
prevents cascade failures where a systemic issue (bad git state, full disk,
tmux bug) causes the prefect to continuously restart agents that immediately
crash again.

The operator investigates, fixes the root cause, and restarts the prefect to
resume normal operation.
