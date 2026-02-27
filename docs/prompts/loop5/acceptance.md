# Loop 5 Acceptance Checklist

## Escalation System
- [x] `gt escalate` creates escalation record in town.db
- [x] `gt escalate --severity=critical` routes to log + mail + webhook
- [x] `gt escalate --severity=low` routes to log only
- [x] `gt escalation list` shows open escalations (human and JSON output)
- [x] `gt escalation ack` marks escalation as acknowledged
- [x] `gt escalation resolve` marks escalation as resolved
- [x] WebhookNotifier POSTs JSON with correct headers and body
- [x] WebhookNotifier respects timeout and context cancellation
- [x] Router uses best-effort delivery (one failure doesn't block others)
- [x] Severity validation rejects invalid values

## Handoff
- [x] `gt handoff` captures tmux output, git log, workflow state
- [x] Handoff file written to `.handoff.json` in polecat dir
- [x] Hook file preserved (not cleared) during handoff
- [x] Handoff mail sent to self (audit trail)
- [x] Session stopped and new session started with same worktree
- [x] `gt prime` detects handoff file and injects handoff context
- [x] Handoff context includes summary, recent commits, workflow progress
- [x] Handoff file deleted after successful prime injection
- [x] Handoff takes priority over workflow context in prime
- [x] CLAUDE.md includes handoff command for all agents
- [x] `gt handoff --summary="..."` passes agent-provided summary

## Deacon
- [x] `gt deacon run` starts continuous patrol loop
- [x] Heartbeat file written atomically each patrol
- [x] `gt deacon status` reads and displays heartbeat
- [x] Stale hooks recovered (hooked work with dead session >1 hour)
- [x] Stale hook recovery: work item → open, agent → idle, hook cleared
- [x] Recent hooks not recovered (respects StaleHookTimeout)
- [x] Hooks with live sessions not recovered
- [x] Stranded convoys detected (open convoy with ready undispatched items)
- [x] CONVOY_NEEDS_FEEDING protocol message sent for stranded convoys
- [x] No duplicate CONVOY_NEEDS_FEEDING messages for same convoy
- [x] Lifecycle requests processed (SHUTDOWN, CYCLE)
- [x] Deacon registers as agent "town/deacon" with role "deacon"
- [x] Patrol errors logged but don't halt the loop (DEGRADE)
- [x] Graceful shutdown on SIGINT/SIGTERM

## Supervisor Integration
- [x] `gt supervisor run --deacon` enables deacon monitoring
- [x] Supervisor auto-starts deacon on first heartbeat if not running
- [x] Supervisor restarts deacon when heartbeat is stale (>15 minutes)
- [x] Deacon exempt from degraded mode (runs even when degraded)
- [x] `gt supervisor run` without `--deacon` works as before
- [x] Full startup: supervisor → deacon → monitoring loop
- [x] Full shutdown: supervisor stops deacon, then other agents

## Events
- [x] `escalation_created` emitted on escalation creation
- [x] `escalation_acked` emitted on acknowledgment
- [x] `escalation_resolved` emitted on resolution
- [x] `handoff` emitted on agent handoff
- [x] `deacon_patrol` emitted each patrol cycle
- [x] `deacon_stale_hook` emitted on stale hook recovery
- [x] `deacon_convoy_feed` emitted on convoy feeding
- [x] `gt feed` formats all new event types correctly

## Backwards Compatibility
- [x] `gt sling` without formula works (Loop 0 behavior)
- [x] `gt prime` without handoff or workflow works (Loop 0 behavior)
- [x] `gt done` works for all scenarios (normal, conflict-resolution)
- [x] `gt supervisor run` without `--deacon` works (Loop 1 behavior)
- [x] All Loop 0–4 tests pass unchanged

## Build
- [x] `make build` succeeds
- [x] `make test` passes (all packages)
- [x] `go vet ./...` clean
