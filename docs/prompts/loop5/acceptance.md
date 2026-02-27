# Loop 5 Acceptance Checklist

## Escalation System
- [ ] `gt escalate` creates escalation record in town.db
- [ ] `gt escalate --severity=critical` routes to log + mail + webhook
- [ ] `gt escalate --severity=low` routes to log only
- [ ] `gt escalation list` shows open escalations (human and JSON output)
- [ ] `gt escalation ack` marks escalation as acknowledged
- [ ] `gt escalation resolve` marks escalation as resolved
- [ ] WebhookNotifier POSTs JSON with correct headers and body
- [ ] WebhookNotifier respects timeout and context cancellation
- [ ] Router uses best-effort delivery (one failure doesn't block others)
- [ ] Severity validation rejects invalid values

## Handoff
- [ ] `gt handoff` captures tmux output, git log, workflow state
- [ ] Handoff file written to `.handoff.json` in polecat dir
- [ ] Hook file preserved (not cleared) during handoff
- [ ] Handoff mail sent to self (audit trail)
- [ ] Session stopped and new session started with same worktree
- [ ] `gt prime` detects handoff file and injects handoff context
- [ ] Handoff context includes summary, recent commits, workflow progress
- [ ] Handoff file deleted after successful prime injection
- [ ] Handoff takes priority over workflow context in prime
- [ ] CLAUDE.md includes handoff command for all agents
- [ ] `gt handoff --summary="..."` passes agent-provided summary

## Deacon
- [ ] `gt deacon run` starts continuous patrol loop
- [ ] Heartbeat file written atomically each patrol
- [ ] `gt deacon status` reads and displays heartbeat
- [ ] Stale hooks recovered (hooked work with dead session >1 hour)
- [ ] Stale hook recovery: work item → open, agent → idle, hook cleared
- [ ] Recent hooks not recovered (respects StaleHookTimeout)
- [ ] Hooks with live sessions not recovered
- [ ] Stranded convoys detected (open convoy with ready undispatched items)
- [ ] CONVOY_NEEDS_FEEDING protocol message sent for stranded convoys
- [ ] No duplicate CONVOY_NEEDS_FEEDING messages for same convoy
- [ ] Lifecycle requests processed (SHUTDOWN, CYCLE)
- [ ] Deacon registers as agent "town/deacon" with role "deacon"
- [ ] Patrol errors logged but don't halt the loop (DEGRADE)
- [ ] Graceful shutdown on SIGINT/SIGTERM

## Supervisor Integration
- [ ] `gt supervisor run --deacon` enables deacon monitoring
- [ ] Supervisor auto-starts deacon on first heartbeat if not running
- [ ] Supervisor restarts deacon when heartbeat is stale (>15 minutes)
- [ ] Deacon exempt from degraded mode (runs even when degraded)
- [ ] `gt supervisor run` without `--deacon` works as before
- [ ] Full startup: supervisor → deacon → monitoring loop
- [ ] Full shutdown: supervisor stops deacon, then other agents

## Events
- [ ] `escalation_created` emitted on escalation creation
- [ ] `escalation_acked` emitted on acknowledgment
- [ ] `escalation_resolved` emitted on resolution
- [ ] `handoff` emitted on agent handoff
- [ ] `deacon_patrol` emitted each patrol cycle
- [ ] `deacon_stale_hook` emitted on stale hook recovery
- [ ] `deacon_convoy_feed` emitted on convoy feeding
- [ ] `gt feed` formats all new event types correctly

## Backwards Compatibility
- [ ] `gt sling` without formula works (Loop 0 behavior)
- [ ] `gt prime` without handoff or workflow works (Loop 0 behavior)
- [ ] `gt done` works for all scenarios (normal, conflict-resolution)
- [ ] `gt supervisor run` without `--deacon` works (Loop 1 behavior)
- [ ] All Loop 0–4 tests pass unchanged

## Build
- [ ] `make build` succeeds
- [ ] `make test` passes (all packages)
- [ ] `go vet ./...` clean
