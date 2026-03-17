# ADR-0009: Envoy as Context-Persistent Claude Session

Status: accepted
Date: 2026-02-28
Arc: 3

## Context

Sol's agent model is built around outposts: ephemeral workers that follow the
cast/resolve cycle. Each writ gets a fresh session, a temporary worktree,
and goes through the forge merge pipeline. This model scales well for discrete,
well-defined tasks dispatched by the system.

However, several autarch use cases don't fit the ephemeral model:

- **Pair programming** — interactive collaboration requiring a persistent
  agent that maintains context as the conversation evolves
- **Long-running research** — tasks spanning multiple writs where
  accumulated context is essential
- **Design partnering** — persistent persona for feature design, spikes, and
  exploration where the agent's accumulated knowledge is the primary value

Outposts lose context on every cast/resolve cycle. There is no way to maintain
an ongoing relationship with an agent across writs.

The Gastown prototype implemented "crew" agents for this purpose — persistent
named Claude Code sessions with mail, self-directed work, direct push to main,
and context cycling (`gt crew refresh` sent handoff mail to self, then
restarted the session). Crew were fully functional but pushed directly to main,
bypassing the merge pipeline entirely.

## Options Considered

### 1. Gastown crew model (direct push, no forge)

Gastown crew were persistent named Claude Code sessions with mail, identity
continuity, and context cycling via handoff mail. They pushed directly to
main, bypassing the refinery (forge) merge pipeline entirely. This created a
trust asymmetry: crew code was unreviewed while polecat (outpost) code went
through quality gates. At scale, this is a reliability risk — a persistent
agent accumulating context drift could push increasingly divergent code with
no checkpoint.

Rejected: all code should go through forge. No trust asymmetry between
agent types. The envoy preserves crew's persistence model but routes all
code through forge.

### 2. Outpost with session persistence

Modify the existing outpost model to optionally preserve sessions across
writs. This avoids introducing a new role but conflates two different
interaction models: system-dispatched ephemeral work (outpost) and
human-directed persistent collaboration (envoy). Sentinel, prefect, and
dispatch logic would need role-aware branching throughout.

Rejected: cleaner to separate the roles entirely. Outposts are
system-managed workers; envoys are human-managed collaborators.

### 3. New role with brief-based context persistence (chosen)

Introduce `role=envoy` with its own lifecycle: persistent worktree,
persistent session, human-supervised, forge-gated. Context persists via
agent-maintained brief files injected through Claude Code hooks.

## Decision

Introduce **envoy** as a new agent role (`role=envoy` in the agents table).
Envoys are persistent, human-directed agents with durable context.

**Context persistence via brief:**

Envoys maintain a brief — an agent-maintained file at `.brief/memory.md`
inside their directory. The envoy's CLAUDE.md instructs it to update this file
with important decisions, research findings, and accumulated knowledge as it
works.

Claude Code hooks provide the persistence lifecycle:
- `SessionStart` (startup) — inject the brief into the conversation
- `SessionStart` (compact) — re-inject after context compaction
- `Stop` — prompt-based hook that nudges the agent to update its brief
  before clean exit (checks `stop_hook_active` to prevent loops)

This is agent-maintained context (no AI summarization overhead, no system
capture). The brief is a plain file — GLASS-inspectable with `cat`.

**Session and worktree model:**

Envoys have a persistent worktree at `$SOL_HOME/{world}/envoys/{name}/worktree/`
and a long-lived tmux session. Unlike outposts, the session and worktree
survive resolve — resolve creates an MR (through forge) but does not kill
the session or tear down the worktree.

**Writ flow:**

Envoys support three modes:
1. Tether to an existing writ (autarch or governor assigns it)
2. Create and tether to their own writ (self-service via `sol writ create`)
3. Freeform — no tether, no writ (exploration, research, design)

The envoy's default persona instructs voluntary tethering when starting focused
work. This uses the existing `tether_item` column in the agents table — no
schema changes required.

**Supervision:**

Envoys are human-supervised. Sentinel already filters to `role='outpost'`, so
envoys are invisible to health monitoring. Prefect also skips envoys — sessions
are not auto-respawned. The human is the supervisor. Lighter-touch automated
supervision was considered (e.g., sentinel awareness without respawn) but adds
complexity for no benefit — the human who started the envoy is already
supervising it interactively.

**Git workflow:**

Always through forge. Envoy code goes through the same merge pipeline as
outpost code. No forge bypass, no trust asymmetry.

## Consequences

**Benefits:**
- Persistent context across sessions — the primary value for pair programming
  and research
- Brief is GLASS-inspectable, agent-maintained, zero AI overhead
- Fits within existing infrastructure: agents table, tmux sessions, forge
  pipeline, resolve flow
- No schema changes required (role column accepts arbitrary strings,
  tether_item column already exists)

**Tradeoffs:**
- Persistent sessions have ongoing API cost (but human-controlled —
  autarch starts/stops at will)
- Brief quality depends on the agent following CLAUDE.md instructions
  (mitigated by Stop hook safety net)
- Crash recovery is lossy: if session is killed (not exited cleanly),
  brief may be stale. Next session picks up from last written state.

**Resolve behavior:**

Envoy resolve runs the standard resolve flow (commit, push, create MR,
update writ status, clear tether) but skips session stop and worktree
cleanup. After resolve, the worktree is on a stale branch. Worktree reset
is agent-managed — CLAUDE.md instructs `git checkout main && git pull`
before new work. System-managed reset was considered but adds forge-to-envoy
coupling and edge cases (what if the envoy has uncommitted work?). The agent
is already human-supervised; trusting it to reset its own worktree is
consistent with the supervision model.

**Code changes:**
- `dispatch.Resolve()`: skip session kill and worktree cleanup for `role=envoy`
- `prefect`: skip `role=envoy` in heartbeat/respawn loop
- `protocol`: new `EnvoyClaudeMD()` generator
- New `internal/envoy/` package for envoy lifecycle management
- New `cmd/envoy.go` for CLI commands
