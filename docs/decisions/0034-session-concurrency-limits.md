# ADR-0034: Session Concurrency Limits

Status: Accepted

## Context

Sol has no mechanism to limit the total number of concurrent agent sessions across a sphere. The existing `agents.capacity` field (per-world, in world.toml/sol.toml) limits the number of agent *records* in the database, not the number of active sessions consuming compute resources. This creates two problems:

1. **No sphere-wide backpressure.** A sphere with many worlds can spawn an unbounded number of concurrent Claude sessions, overwhelming CPU, memory, and API quota on the host machine.

2. **Capacity counts the wrong thing.** Persistent agents (envoys, governors) that are sleeping — no active tmux session, no resource consumption — still count against the per-world limit. A world with 2 envoys, 1 governor, and `capacity=6` has only 3 outpost slots, even when all persistent agents are idle. The limit protects against roster bloat in the database, which is not a real problem.

What actually matters is the number of live sessions: tmux processes running a Claude Code (or other runtime) session, consuming CPU, memory, and API quota.

## Decision

Replace `agents.capacity` (agent record count) with two session-based concurrency limits:

### Configuration

```toml
# sol.toml — sphere-scoped ceiling
[sphere]
max_sessions = 0    # 0 = unlimited (default)

# world.toml — per-world limit (also settable in sol.toml as default)
[agents]
max_active = 0      # 0 = unlimited (default)
```

- `sphere.max_sessions`: Hard cap on total active tmux sessions across all worlds. Lives in sol.toml only (sphere-scoped). New config section `[sphere]`.
- `agents.max_active`: Maximum concurrent active sessions within a single world. Layered like other agent config (sol.toml default, world.toml override).
- Both default to 0 (unlimited), preserving current behavior for existing installations.
- `agents.capacity` is removed. Agent records in the database are not a scarce resource.

### Counting

Active sessions are counted via tmux, not the database. A session is "active" if `tmux has-session -t <name>` succeeds. This is ground truth — no stale-state issues from crashed agents that didn't update the DB.

- **Per-world count**: tmux sessions matching the `sol-{world}-*` naming convention.
- **Sphere-wide count**: all tmux sessions matching `sol-*`.

The session manager gains a `CountSessions` method (or the equivalent) that shells out to `tmux list-sessions` with a format filter.

### Enforcement points

1. **`dispatch.Cast`** (launching new work): Check both `max_active` and `max_sessions` before starting a session. Return `ErrCapacityExhausted` if either limit is reached. The existing provision lock serializes this check per-world; sphere-wide checking needs a sphere-level lock.
2. **`prefect` respawn**: Check both limits before restarting a crashed/stalled session. If at capacity, back off and retry on the next heartbeat cycle rather than dropping the agent.
3. **`consul` dispatch loop**: Already handles `ErrCapacityExhausted` — breaks the dispatch loop for that world. No change needed beyond what dispatch returns.

### Lock strategy

- **Per-world** (`max_active`): Reuse the existing provision lock (`{world}-provision.lock`).
- **Sphere-wide** (`max_sessions`): New sphere-level lock (`sphere-session.lock`) acquired after the per-world lock, held briefly for the count+start sequence.

### What counts against limits

Any live tmux session with a `sol-*` name counts, regardless of agent role. A running envoy session counts. A sleeping envoy (no tmux session) does not. This aligns the limit with actual resource consumption.

## Consequences

**Positive**:
- Sol can run on resource-constrained machines without overwhelming them.
- Operators can tune concurrency to match their API quota and compute budget.
- Persistent agents don't waste capacity slots when idle.
- Both limits default to unlimited — zero migration burden for existing users.

**Negative / Trade-offs**:
- tmux-based counting adds a shell-out per Cast/respawn. This is fast (~5ms) and already on the session-start critical path, so the overhead is negligible.
- Sphere-level lock introduces brief contention across worlds during session start. Acceptable given session starts are already heavyweight (tmux + claude startup).
- Removing `capacity` is a breaking change for anyone using it. Mitigated by the fact that `max_active` serves the same purpose (limiting per-world concurrency) but counts the right thing.
