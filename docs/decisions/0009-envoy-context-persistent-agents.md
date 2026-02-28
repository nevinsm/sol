# ADR-0009: Envoy as Context-Persistent Claude Session

Status: accepted
Date: 2026-02-28
Arc: 3

## Context

Sol's agent model is built around outposts: ephemeral workers that follow the
cast/resolve cycle. Each work item gets a fresh session, a temporary worktree,
and goes through the forge merge pipeline. This model scales well for discrete,
well-defined tasks dispatched by the system.

However, several operator use cases don't fit the ephemeral model:

- **Pair programming** — interactive collaboration requiring a persistent
  agent that maintains context as the conversation evolves
- **Long-running research** — tasks spanning multiple work items where
  accumulated context is essential
- **Design partnering** — persistent persona for feature design, spikes, and
  exploration where the agent's accumulated knowledge is the primary value

Outposts lose context on every cast/resolve cycle. There is no way to maintain
an ongoing relationship with an agent across work items.

The Gastown prototype defined "crew" agents for this purpose but never
implemented them.

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

**Work item flow:**

Envoys support three modes:
1. Tether to an existing work item (operator or governor assigns it)
2. Create and tether to their own work item (self-service via `sol store create-item`)
3. Freeform — no tether, no work item (exploration, research, design)

The envoy's default persona instructs voluntary tethering when starting focused
work. This uses the existing `tether_item` column in the agents table — no
schema changes required.

**Supervision:**

Envoys are human-supervised. Sentinel already filters to `role='agent'`, so
envoys are invisible to health monitoring. Prefect also skips envoys — sessions
are not auto-respawned. The human is the supervisor.

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
  operator starts/stops at will)
- Brief quality depends on the agent following CLAUDE.md instructions
  (mitigated by Stop hook safety net)
- Crash recovery is lossy: if session is killed (not exited cleanly),
  brief may be stale. Next session picks up from last written state.

**Code changes:**
- `dispatch.Resolve()`: skip session kill and worktree cleanup for `role=envoy`
- `prefect`: skip `role=envoy` in heartbeat/respawn loop
- `protocol`: new `EnvoyClaudeMD()` generator
- New `internal/envoy/` package for envoy lifecycle management
- New `cmd/envoy.go` for CLI commands
