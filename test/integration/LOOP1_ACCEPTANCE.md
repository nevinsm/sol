# Loop 1 Acceptance Criteria

## 1. Multi-agent dispatch
- [ ] `gt sling <item1> myrig && gt sling <item2> myrig` dispatches to two different polecats
- [ ] Each polecat has a unique name from the name pool
- [ ] Each polecat runs in its own worktree and tmux session

## 2. Dispatch serialization
- [ ] No two polecats get the same work item (flock prevents races)
- [ ] Concurrent `gt sling` for the same item: one wins, one fails with contention error

## 3. Name pool
- [ ] Auto-provisioned agents get names from the embedded pool (Toast, Jasper, ...)
- [ ] Custom `$GT_HOME/{rig}/names.txt` overrides the default pool
- [ ] Pool exhaustion returns a clear error

## 4. Supervisor — crash detection and restart
- [ ] `gt supervisor run` starts the supervisor (foreground, PID file written)
- [ ] Supervisor is town-level — monitors all rigs from one process
- [ ] Kill a polecat's tmux session → supervisor detects and restarts within heartbeat interval
- [ ] Restarted polecat picks up hooked work (GUPP principle)

## 5. Supervisor — backoff
- [ ] Repeated crashes of the same agent increase restart delay
- [ ] Backoff resets when agent completes work normally

## 6. Supervisor — mass-death protection
- [ ] Kill 3+ sessions in 30s → supervisor enters degraded mode (no respawns)
- [ ] Degraded mode auto-recovers after 5 minutes of quiet

## 7. Supervisor — lifecycle
- [ ] `gt supervisor stop` sends SIGTERM, supervisor stops all sessions gracefully
- [ ] Only one supervisor instance (PID file guard)
- [ ] Stale PID files from crashed supervisors are detected and overwritten

## 8. Status command
- [ ] `gt status myrig` shows agents, sessions, hooked work, supervisor state
- [ ] `gt status myrig --json` outputs valid JSON with all fields
- [ ] Exit code 0 = healthy, 1 = dead sessions, 2 = degraded/no supervisor

## 9. All tests pass
- [ ] `make test` exits 0
- [ ] Loop 0 integration tests still pass
- [ ] Loop 1 integration tests pass
- [ ] CLI smoke tests pass (new commands)
