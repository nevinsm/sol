# ADR-0006: Prefect Defers Outpost Management to Sentinel

Status: accepted
Date: 2026-02-26
Loop: 3

## Context

The prefect (Loop 1) runs sphere-wide on a 3-minute heartbeat. For each
agent with `state=working` whose tmux session is dead, it respawns the
session with exponential backoff. It has no concept of "max respawns" or
"return work to open" — it retries indefinitely with increasing delays.

The sentinel (Loop 3) adds per-world outpost monitoring with richer
recovery: AI-assisted assessment, max-2-respawns-per-work-item, and
return-to-open when respawns are exhausted. It also runs on a 3-minute
patrol interval.

Both systems detect the same condition (dead session for a working
outpost) and take the same initial action (restart the tmux session).
Without coordination, this creates three problems:

1. **Race conditions.** Both detect the same dead agent in the same
   heartbeat window. Both call `session.Start()` — the second fails
   (session already exists), but their counters diverge.
2. **Counter drift.** The prefect might restart an agent that the
   sentinel is tracking. The sentinel's respawn counter becomes inaccurate
   because it didn't initiate the restart.
3. **Policy conflict.** The sentinel exhausts max respawns and returns
   work to open. But the prefect, unaware of this policy, may have
   already restarted the agent one more time before the sentinel clears
   the tether.

## Decision

When a sentinel is active for a world, the prefect defers all outpost
management in that world to the sentinel.

**A world is "sentineled" when both:**
- The `{world}/sentinel` agent has `state=working` in the sphere store
- The sentinel's tmux session (`sol-{world}-sentinel`) is alive

Both conditions must hold. If the sentinel agent is `working` but the
session is dead, the world is not sentineled — the prefect will restart
the sentinel (via normal heartbeat) and resume outpost supervision until
the sentinel is back.

**What the prefect skips in sentineled worlds:**
- Respawning outposts (`role=outpost`)
- Backoff tracking for outposts

**What the prefect still handles in sentineled worlds:**
- The sentinel itself (`role=sentinel`) — restarts it if it dies
- The forge (`role=forge`) — restarts it if it dies
- Mass death counting — all dead sessions still count toward the
  mass-death threshold, regardless of which component manages them

**What the prefect handles in non-sentineled worlds:**
- Everything, as before — full outpost supervision with backoff

### DEGRADE interaction

The prefect's mass-death detection remains sphere-wide. All dead
sessions (including outposts in sentineled worlds) count toward the
threshold. When degraded mode triggers:

- The prefect marks agents it manages as `stalled` and stops
  restarts (existing behavior)
- Outposts in sentineled worlds are unaffected by degraded mode — the
  sentinel continues its own patrol independently
- The sentinel's max-2-then-return-to-open policy is its own form of
  degraded behavior, preventing runaway restarts at the world level

This is acceptable. The Consul (Loop 5) will provide sphere-wide
judgment-based coordination across all supervisory components.

## Consequences

**Benefits:**
- Clean ownership boundary: sentinel owns outposts in its world,
  prefect owns infrastructure agents and non-sentineled worlds
- Automatic fallback: if the sentinel dies, the prefect detects the
  dead session, restarts the sentinel, and covers outposts in the
  interim (DEGRADE principle)
- No shared counters or coordination protocol between the two systems
- The sentinel's richer recovery policy (max respawns, return-to-open,
  AI assessment) is fully in effect without interference

**Tradeoffs:**
- Prefect heartbeat gains a per-world lookup (sentinel alive?) — minor
  cost, cached per heartbeat cycle
- Brief supervision gap: between the sentinel dying and the prefect's
  next heartbeat (up to 3 minutes), outposts in that world have no
  supervision. Acceptable — the work is tethered and durable (GUPP)
- Mass-death threshold may include outposts the prefect can't act
  on. This is intentional — infrastructure failures are worth detecting
  even if another component handles the response

**Prefect code changes:**
- `heartbeat()`: before processing working agents, query active
  sentinels (store state + session liveness) to build a set of
  sentineled worlds. Skip outposts in those worlds.
- `respawn()`: no changes needed — it simply won't be called for
  outposts in sentineled worlds
- Mass death counting: no changes — still counts all dead sessions
