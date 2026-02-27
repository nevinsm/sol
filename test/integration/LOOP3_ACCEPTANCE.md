# Loop 3 Acceptance Checklist

## Mail System
- [x] Messages table created in sphere.db (schema V2)
- [x] Escalations table created in sphere.db (schema V2)
- [x] Sphere schema V1→V2 migration preserves existing agents
- [x] SendMessage creates message with msg- prefix ID
- [x] Inbox returns pending messages ordered by priority then age
- [x] ReadMessage fetches content and marks as read
- [x] AckMessage sets delivery=acked with timestamp
- [x] CountUnread returns correct unread count
- [x] Protocol messages sent with type=protocol and JSON body
- [x] PendingProtocol filters by recipient and protocol type
- [x] CLI: sol mail send creates message
- [x] CLI: sol mail inbox lists pending messages
- [x] CLI: sol mail read displays message content
- [x] CLI: sol mail ack acknowledges message
- [x] CLI: sol mail check reports unread count

## Event Feed
- [x] Events logged to $SOL_HOME/.events.jsonl as valid JSONL
- [x] Concurrent writes are flock-serialized (no interleaving)
- [x] Logger is best-effort (nil-safe, errors swallowed silently)
- [x] Reader filters by type, since, limit
- [x] Follow mode streams new events via channel
- [x] Cast emits EventCast when logger provided
- [x] Done emits EventResolve when logger provided
- [x] Forge emits merge events when logger provided
- [x] Prefect emits respawn/degraded events when logger provided
- [x] CLI: sol feed displays events with human-readable format
- [x] CLI: sol feed --follow streams events
- [x] CLI: sol feed --type filters by event type
- [x] CLI: sol feed --json outputs raw JSONL
- [x] CLI: sol log-event emits custom events

## Chronicle
- [x] Chronicle reads raw events and writes curated feed
- [x] Audit-only events filtered from curated feed
- [x] Duplicate events deduplicated within 10s window
- [x] Cast bursts aggregated within 30s window
- [x] Non-aggregatable events (done, merged) preserved individually
- [x] Curated feed truncated when exceeding max size
- [x] Truncation preserves complete JSON lines (no partials)
- [x] Checkpoint file tracks read position across restarts
- [x] sol feed reads curated feed by default, raw with --raw
- [x] CLI: sol chronicle run/start/stop all work

## Sentinel
- [x] Sentinel registers as {world}/sentinel with role=sentinel
- [x] Patrol cycle runs every PatrolInterval
- [x] Dead session + tethered work → stalled detection
- [x] Stalled agent → respawn attempted (max 2 per work item)
- [x] After max respawns → work returned to open, tether cleared, agent idle
- [x] Zombie session (idle + no tether + live session) → session stopped
- [x] Healthy agents → no action taken
- [x] Progress heuristic: captures tmux output and hashes between patrols
- [x] No output change → AI assessment triggered
- [x] AI assessment nudge → message injected into agent session
- [x] AI assessment escalate → RECOVERY_NEEDED message to operator
- [x] Low confidence assessment → no action taken
- [x] AI assessment failure → patrol continues (non-blocking)
- [x] Patrol emits EventPatrol with summary counts
- [x] Assessment emits EventAssess
- [x] Nudge emits EventNudge
- [x] Prefect handles sentinel role (respawnCommand, worktreeForAgent)
- [x] Status shows sentinel running/stopped
- [x] CLI: sol sentinel run/start/stop/attach all work

## Backward Compatibility
- [x] All Loop 0 tests pass
- [x] All Loop 1 tests pass
- [x] All Loop 2 tests pass
- [x] Existing dispatch operations work with nil logger
- [x] Existing mocks updated for new interface methods

## Overall
- [x] make test passes (all loops)
- [x] make build succeeds
- [x] No TODOs or incomplete features
