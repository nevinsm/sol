# Loop 3 Acceptance Checklist

## Mail System
- [x] Messages table created in town.db (schema V2)
- [x] Escalations table created in town.db (schema V2)
- [x] Town schema V1→V2 migration preserves existing agents
- [x] SendMessage creates message with msg- prefix ID
- [x] Inbox returns pending messages ordered by priority then age
- [x] ReadMessage fetches content and marks as read
- [x] AckMessage sets delivery=acked with timestamp
- [x] CountUnread returns correct unread count
- [x] Protocol messages sent with type=protocol and JSON body
- [x] PendingProtocol filters by recipient and protocol type
- [x] CLI: gt mail send creates message
- [x] CLI: gt mail inbox lists pending messages
- [x] CLI: gt mail read displays message content
- [x] CLI: gt mail ack acknowledges message
- [x] CLI: gt mail check reports unread count

## Event Feed
- [x] Events logged to $GT_HOME/.events.jsonl as valid JSONL
- [x] Concurrent writes are flock-serialized (no interleaving)
- [x] Logger is best-effort (nil-safe, errors swallowed silently)
- [x] Reader filters by type, since, limit
- [x] Follow mode streams new events via channel
- [x] Sling emits EventSling when logger provided
- [x] Done emits EventDone when logger provided
- [x] Refinery emits merge events when logger provided
- [x] Supervisor emits respawn/degraded events when logger provided
- [x] CLI: gt feed displays events with human-readable format
- [x] CLI: gt feed --follow streams events
- [x] CLI: gt feed --type filters by event type
- [x] CLI: gt feed --json outputs raw JSONL
- [x] CLI: gt log-event emits custom events

## Curator
- [x] Curator reads raw events and writes curated feed
- [x] Audit-only events filtered from curated feed
- [x] Duplicate events deduplicated within 10s window
- [x] Sling bursts aggregated within 30s window
- [x] Non-aggregatable events (done, merged) preserved individually
- [x] Curated feed truncated when exceeding max size
- [x] Truncation preserves complete JSON lines (no partials)
- [x] Checkpoint file tracks read position across restarts
- [x] gt feed reads curated feed by default, raw with --raw
- [x] CLI: gt curator run/start/stop all work

## Witness
- [x] Witness registers as {rig}/witness with role=witness
- [x] Patrol cycle runs every PatrolInterval
- [x] Dead session + hooked work → stalled detection
- [x] Stalled agent → respawn attempted (max 2 per work item)
- [x] After max respawns → work returned to open, hook cleared, agent idle
- [x] Zombie session (idle + no hook + live session) → session stopped
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
- [x] Supervisor handles witness role (respawnCommand, worktreeForAgent)
- [x] Status shows witness running/stopped
- [x] CLI: gt witness run/start/stop/attach all work

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
