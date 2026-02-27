# Loop 5 Acceptance Checklist

## Escalation System
- [x] `sol escalate` creates escalation record in sphere.db
- [x] `sol escalate --severity=critical` routes to log + mail + webhook
- [x] `sol escalate --severity=low` routes to log only
- [x] `sol escalation list` shows open escalations (human and JSON output)
- [x] `sol escalation ack` marks escalation as acknowledged
- [x] `sol escalation resolve` marks escalation as resolved
- [x] WebhookNotifier POSTs JSON with correct headers and body
- [x] WebhookNotifier respects timeout and context cancellation
- [x] Router uses best-effort delivery (one failure doesn't block others)
- [x] Severity validation rejects invalid values

## Handoff
- [x] `sol handoff` captures tmux output, git log, workflow state
- [x] Handoff file written to `.handoff.json` in outpost dir
- [x] Tether file preserved (not cleared) during handoff
- [x] Handoff mail sent to self (audit trail)
- [x] Session stopped and new session started with same worktree
- [x] `sol prime` detects handoff file and injects handoff context
- [x] Handoff context includes summary, recent commits, workflow progress
- [x] Handoff file deleted after successful prime injection
- [x] Handoff takes priority over workflow context in prime
- [x] CLAUDE.md includes handoff command for all agents
- [x] `sol handoff --summary="..."` passes agent-provided summary

## Consul
- [x] `sol consul run` starts continuous patrol loop
- [x] Heartbeat file written atomically each patrol
- [x] `sol consul status` reads and displays heartbeat
- [x] Stale tethers recovered (tethered work with dead session >1 hour)
- [x] Stale tether recovery: work item → open, agent → idle, tether cleared
- [x] Recent tethers not recovered (respects StaleHookTimeout)
- [x] Tethers with live sessions not recovered
- [x] Stranded caravans detected (open caravan with ready undispatched items)
- [x] CARAVAN_NEEDS_FEEDING protocol message sent for stranded caravans
- [x] No duplicate CARAVAN_NEEDS_FEEDING messages for same caravan
- [x] Lifecycle requests processed (SHUTDOWN, CYCLE)
- [x] Consul registers as agent "sphere/consul" with role "consul"
- [x] Patrol errors logged but don't halt the loop (DEGRADE)
- [x] Graceful shutdown on SIGINT/SIGTERM

## Prefect Integration
- [x] `sol prefect run --consul` enables consul monitoring
- [x] Prefect auto-starts consul on first heartbeat if not running
- [x] Prefect restarts consul when heartbeat is stale (>15 minutes)
- [x] Consul exempt from degraded mode (runs even when degraded)
- [x] `sol prefect run` without `--consul` works as before
- [x] Full startup: prefect → consul → monitoring loop
- [x] Full shutdown: prefect stops consul, then other agents

## Events
- [x] `escalation_created` emitted on escalation creation
- [x] `escalation_acked` emitted on acknowledgment
- [x] `escalation_resolved` emitted on resolution
- [x] `handoff` emitted on agent handoff
- [x] `deacon_patrol` emitted each patrol cycle
- [x] `deacon_stale_hook` emitted on stale tether recovery
- [x] `deacon_convoy_feed` emitted on caravan feeding
- [x] `sol feed` formats all new event types correctly

## Backwards Compatibility
- [x] `sol cast` without formula works (Loop 0 behavior)
- [x] `sol prime` without handoff or workflow works (Loop 0 behavior)
- [x] `sol resolve` works for all scenarios (normal, conflict-resolution)
- [x] `sol prefect run` without `--consul` works (Loop 1 behavior)
- [x] All Loop 0–4 tests pass unchanged

## Build
- [x] `make build` succeeds
- [x] `make test` passes (all packages)
- [x] `go vet ./...` clean
