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
| Forge | `merge_requests` table, slot lock | In-progress merge | Prefect restarts Go process; patrol resumes from cycle start (idempotent) | <30s |
| Outpost | Tether file, worktree, identity | Session memory | `sol prime` re-injects context (GUPP) | <30s |
| Event Feed | JSONL files | Chronicle buffer | Chronicle restarts, tails from last position | <10s |
| Ledger | token_usage + agent_history in world DBs | In-memory session cache, in-flight requests | Restart; session cache rebuilds on first event | <1s |
| Brief | `.brief/memory.md` file | None (file-based) | Read on next injection; missing = clean start | <1s |
| Envoy | Worktree, tether dir, brief, resume state | Session memory | Brief re-injection + tether list + resume state | <30s |
| Governor | Governor dir, tether dir, brief, world summary | Session memory | Brief re-injection + tether list + world sync | <30s |
| Senate | Senate dir, tether dir, brief | Session memory | Brief re-injection + tether list | <30s |
| Doctor | None (stateless) | N/A | No recovery needed | N/A |
| Status | None (stateless) | N/A | No recovery needed | N/A |

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
| Ledger | Token tracking pauses. Agents continue — no work is gated on telemetry. |
| Senate | Cross-world planning pauses. Per-world governors and agents continue independently. |
| Governor | Per-world coordination pauses. Tethered agents continue executing. New writ dispatch waits. |

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

If the forge Go process crashes, the prefect detects heartbeat staleness and
restarts the process. The patrol loop resumes from the beginning of the cycle
— all steps are idempotent, so no state recovery is needed.

**Crash during merge** (after `git merge --squash`, before push): the worktree
is dirty. The next patrol cycle runs `git reset --hard` in the sync step,
restoring a clean slate.

**Crash after push, before mark-merged**: the writ is still open and the MR
is still claimed. On restart, the patrol detects the stale claim (TTL expiry)
or processes it normally. The existing crash-safety in `MarkMerged()` (close
writ first) is unchanged.

Claimed merge requests with expired TTL (30 min) are automatically released
for re-claim. No merges land while down; the queue accumulates.

### Outpost (Worker Agent)

Session crash: work remains tethered, worktree preserved. On restart,
`sol prime` reads tether, workflow state, and pending messages to reconstruct
context. The agent resumes from last durable state. It doesn't know it
crashed — it just sees its tether and gets to work (GUPP).

### Event Feed / Chronicle

Event logging is best-effort — failures are silently ignored. If the chronicle
crashes, the raw log continues growing and the curated feed is stale. The
prefect restarts the chronicle. No primary operations are affected.

### Ledger

If the ledger crashes, token tracking pauses but no agent work is affected —
ledger is a telemetry receiver, not a coordination component. The prefect
detects the dead `sol-ledger` tmux session and restarts it.

**State survives:** `agent_history` and `token_usage` rows in per-world SQLite
databases. All committed token data is durable (WAL journaling).

**State lost:** In-memory session cache (`sessionKey → history_id` map) and
cached store handles. In-flight OTLP requests being processed at crash time.

**Recovery:** On restart, the ledger starts with empty caches. The first OTLP
event for each agent session calls `ensureHistory()` to create a new
`agent_history` record and caches the ID. Subsequent events for that session
reuse the cached ID. Token events received before the crash are safe in the
database; events lost in-flight are gone (acceptable — telemetry is best-effort).

**Recovery time:** <1s. Cache rebuilds lazily on first event per session.

### Brief

Brief files (`.brief/memory.md`) are the context durability primitive for
persistent agents. They are plain files — no database backing, no in-memory
cache.

**State survives:** The markdown file itself. Brief files survive session
crashes, process restarts, and tmux server restarts.

**State lost:** Nothing — brief is file-based and written by the agent during
its session. On crash, the file reflects the last agent write.

**Recovery:** On next session start, startup hooks call `sol brief inject` to
read the file and inject its contents. Missing brief = clean start (not a
failure). Stale brief = reduced context (not an error). Three-layer size
management: CLAUDE.md guidance, agent self-pruning, injection truncation
(200-line hard cap).

**Recovery time:** <1s (file read and injection).

**Graceful shutdown:** `brief.GracefulStop()` injects an update prompt before
killing the session, polling for output stability (4 stable captures at 10s
intervals). Force-kills after 90s. If no `.brief/` directory exists, falls
back to immediate kill.

### Envoy

Envoys are persistent human-directed agents with dedicated worktrees, brief
files, and multi-writ tethers. Their failure profile mirrors outposts but with
additional durable state.

**State survives:** Git worktree (branch `envoy/{world}/{name}`), tether
directory with per-writ files, `.brief/memory.md`, agent record in sphere DB,
`.resume_state.json` (writ switch state). All survive crashes intact.

**State lost:** Session conversation history and in-flight tool executions.

**Recovery:** Prefect detects the dead session and respawns it. On startup,
brief is re-injected (reduced context, not failure), tether directory is read
to recover writ bindings, and resume state file determines the correct active
writ. The envoy resumes from last durable state — it sees its tether and gets
to work (GUPP).

**Recovery time:** <30s (prefect respawn + brief injection).

**Multi-tether crash:** Tether directory survives. If `active_writ` in the DB
is stale (crash during writ switch), the startup sequence reads the resume
state file and tether directory to reconcile. See Multi-Tether Crash Recovery
section.

### Governor

Governors are per-world coordinators with persistent Claude sessions, brief
files, and multi-writ tethers. Similar failure profile to envoys but with
world-scoped coordination state.

**State survives:** Governor directory (`$SOL_HOME/{world}/governor/`), tether
directory, `.brief/memory.md`, `.brief/world-summary.md`, agent record in
sphere DB, `.resume_state.json`.

**State lost:** Session conversation history and in-flight coordination
decisions (which writs to dispatch, caravan phase transitions).

**Recovery:** Prefect detects the dead session and respawns it. On startup,
brief and world summary are re-injected, tether directory is read to recover
writ bindings, and `sol world sync` runs to refresh world state. The governor
resumes coordination from last durable state. Pending decisions are lost but
can be re-derived from writ and caravan state in the database.

**Recovery time:** <30s (prefect respawn + brief injection + world sync).

**While down:** Tethered agents continue executing. New writ dispatch waits
until the governor session is restored. Sentinel continues health monitoring
independently.

### Senate

The senate is a sphere-scoped planning agent with a fixed tmux session
(`sol-senate`), brief system, and multi-writ tethers. Similar failure profile
to governor but at sphere scope.

**State survives:** Senate directory (`$SOL_HOME/senate/`), tether directory,
`.brief/memory.md`, agent record in sphere DB.

**State lost:** Session conversation history and in-flight cross-world planning
decisions.

**Recovery:** Prefect detects the dead session and respawns it. On startup,
brief is re-injected and tether directory is read to recover writ bindings.
The senate resumes planning from last durable state. Cross-world coordination
pauses during downtime — per-world governors and agents continue independently.

**Recovery time:** <30s (prefect respawn + brief injection).

### Doctor

Doctor is a stateless prerequisite checker. It runs read-only checks (tmux,
git, claude, SOL_HOME, SQLite WAL support), produces an in-memory report, and
exits. No persistent state is created or modified.

**No recovery needed.** Doctor is idempotent and can be re-run at any time.
A crash during a check has no side effects — temporary files (used for
writability and WAL tests) are cleaned up via deferred removal.

### Status

Status is a stateless read-only renderer. It queries authoritative sources
(databases, tmux sessions, PID files, heartbeat files, brief file timestamps)
at point of use (ZFC principle) and produces a snapshot for display.

**No recovery needed.** Status creates no persistent state. If it crashes
mid-render, re-running produces a fresh snapshot. The accuracy of its output
depends on other components' state — if a PID file or heartbeat is stale,
status reports stale health, but that's a problem in the source component,
not in status itself.

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

### Multi-Tether Crash Recovery (Persistent Agents)

Persistent agents (envoys, governors) use tether directories with multiple
writ files. On crash:

- **Tether directory survives.** All bound writs are recoverable via
  `tether.List()`. No writ bindings are lost.
- **`active_writ` may be stale.** The DB column reflects the last known
  active writ. If the crash occurred during a writ switch, the column may
  point to the previous or new writ. Recovery: the startup sequence reads
  the tether directory and resume state file to determine the correct
  active writ.
- **Resume state file survives.** If `sol writ activate` wrote a
  `.resume_state.json` before crash, the next startup picks it up and
  resumes into the correct writ context.
- **Safe default:** If active_writ points to a writ no longer in the tether
  directory, the startup clears it. The operator or governor can re-activate
  the appropriate writ.

### Writ Switching Failure

When `sol writ activate` triggers a session restart:

- **DB update succeeds, session restart fails.** The active_writ is updated
  in the DB, and a `.resume_state.json` is written to disk. On next session
  start (manual or prefect respawn), `startup.Resume()` reads the resume
  state and launches with the correct writ context. No data loss.
- **Handoff marker written.** The resume state file acts as a handoff
  marker — it persists across process crashes and tells the next startup
  exactly where to resume.
- **Partial restart.** If the session stops but doesn't restart (e.g., tmux
  server crash), the prefect detects the dead session and respawns it. The
  respawn reads the resume state and activates the correct writ.

## Mass Failure

When 3+ agent sessions die within 30 seconds, the prefect enters degraded mode:
it logs the event, stops respawning agents, and notifies the operator. This
prevents cascade failures where a systemic issue (bad git state, full disk,
tmux bug) causes the prefect to continuously restart agents that immediately
crash again.

The operator investigates, fixes the root cause, and restarts the prefect to
resume normal operation.
