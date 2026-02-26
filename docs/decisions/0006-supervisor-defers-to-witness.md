# ADR-0006: Supervisor Defers Polecat Management to Witness

Status: accepted
Date: 2026-02-26
Loop: 3

## Context

The supervisor (Loop 1) runs town-wide on a 3-minute heartbeat. For each
agent with `state=working` whose tmux session is dead, it respawns the
session with exponential backoff. It has no concept of "max respawns" or
"return work to open" — it retries indefinitely with increasing delays.

The witness (Loop 3) adds per-rig polecat monitoring with richer
recovery: AI-assisted assessment, max-2-respawns-per-work-item, and
return-to-open when respawns are exhausted. It also runs on a 3-minute
patrol interval.

Both systems detect the same condition (dead session for a working
polecat) and take the same initial action (restart the tmux session).
Without coordination, this creates three problems:

1. **Race conditions.** Both detect the same dead agent in the same
   heartbeat window. Both call `session.Start()` — the second fails
   (session already exists), but their counters diverge.
2. **Counter drift.** The supervisor might restart an agent that the
   witness is tracking. The witness's respawn counter becomes inaccurate
   because it didn't initiate the restart.
3. **Policy conflict.** The witness exhausts max respawns and returns
   work to open. But the supervisor, unaware of this policy, may have
   already restarted the agent one more time before the witness clears
   the hook.

## Decision

When a witness is active for a rig, the supervisor defers all polecat
management in that rig to the witness.

**A rig is "witnessed" when both:**
- The `{rig}/witness` agent has `state=working` in the town store
- The witness's tmux session (`gt-{rig}-witness`) is alive

Both conditions must hold. If the witness agent is `working` but the
session is dead, the rig is not witnessed — the supervisor will restart
the witness (via normal heartbeat) and resume polecat supervision until
the witness is back.

**What the supervisor skips in witnessed rigs:**
- Respawning polecats (`role=polecat`)
- Backoff tracking for polecats

**What the supervisor still handles in witnessed rigs:**
- The witness itself (`role=witness`) — restarts it if it dies
- The refinery (`role=refinery`) — restarts it if it dies
- Mass death counting — all dead sessions still count toward the
  mass-death threshold, regardless of which component manages them

**What the supervisor handles in unwitnessed rigs:**
- Everything, as before — full polecat supervision with backoff

### DEGRADE interaction

The supervisor's mass-death detection remains town-wide. All dead
sessions (including polecats in witnessed rigs) count toward the
threshold. When degraded mode triggers:

- The supervisor marks agents it manages as `stalled` and stops
  restarts (existing behavior)
- Polecats in witnessed rigs are unaffected by degraded mode — the
  witness continues its own patrol independently
- The witness's max-2-then-return-to-open policy is its own form of
  degraded behavior, preventing runaway restarts at the rig level

This is acceptable. The Deacon (Loop 5) will provide town-wide
judgment-based coordination across all supervisory components.

## Consequences

**Benefits:**
- Clean ownership boundary: witness owns polecats in its rig,
  supervisor owns infrastructure agents and unwitnessed rigs
- Automatic fallback: if the witness dies, the supervisor detects the
  dead session, restarts the witness, and covers polecats in the
  interim (DEGRADE principle)
- No shared counters or coordination protocol between the two systems
- The witness's richer recovery policy (max respawns, return-to-open,
  AI assessment) is fully in effect without interference

**Tradeoffs:**
- Supervisor heartbeat gains a per-rig lookup (witness alive?) — minor
  cost, cached per heartbeat cycle
- Brief supervision gap: between the witness dying and the supervisor's
  next heartbeat (up to 3 minutes), polecats in that rig have no
  supervision. Acceptable — the work is hooked and durable (GUPP)
- Mass-death threshold may include polecats the supervisor can't act
  on. This is intentional — infrastructure failures are worth detecting
  even if another component handles the response

**Supervisor code changes:**
- `heartbeat()`: before processing working agents, query active
  witnesses (store state + session liveness) to build a set of
  witnessed rigs. Skip polecats in those rigs.
- `respawn()`: no changes needed — it simply won't be called for
  polecats in witnessed rigs
- Mass death counting: no changes — still counts all dead sessions
