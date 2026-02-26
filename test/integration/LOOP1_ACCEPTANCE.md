# Loop 1 Acceptance Criteria

## 1. Multi-agent dispatch
- [x] `gt sling <item1> myrig && gt sling <item2> myrig` dispatches to two different polecats
- [x] Each polecat has a unique name from the name pool
- [x] Each polecat runs in its own worktree and tmux session

## 2. Dispatch serialization
- [x] No two polecats get the same work item (flock prevents races)
- [x] Concurrent `gt sling` for the same item: one wins, one fails with contention error

## 3. Name pool
- [x] Auto-provisioned agents get names from the embedded pool (Toast, Jasper, ...)
- [x] Custom `$GT_HOME/{rig}/names.txt` overrides the default pool
- [x] Pool exhaustion returns a clear error

## 4. Supervisor — crash detection and restart
- [x] `gt supervisor run` starts the supervisor (foreground, PID file written)
- [x] Supervisor is town-level — monitors all rigs from one process
- [x] Kill a polecat's tmux session → supervisor detects and restarts within heartbeat interval
- [x] Restarted polecat picks up hooked work (GUPP principle)

## 5. Supervisor — backoff
- [x] Repeated crashes of the same agent increase restart delay
- [x] Backoff resets when agent completes work normally

## 6. Supervisor — mass-death protection
- [x] Kill 3+ sessions in 30s → supervisor enters degraded mode (no respawns)
- [x] Degraded mode auto-recovers after 5 minutes of quiet

## 7. Supervisor — lifecycle
- [x] `gt supervisor stop` sends SIGTERM, supervisor stops all sessions gracefully
- [x] Only one supervisor instance (PID file guard)
- [x] Stale PID files from crashed supervisors are detected and overwritten

## 8. Status command
- [x] `gt status myrig` shows agents, sessions, hooked work, supervisor state
- [x] `gt status myrig --json` outputs valid JSON with all fields
- [x] Exit code 0 = healthy, 1 = dead sessions, 2 = degraded/no supervisor

## 9. All tests pass
- [x] `make test` exits 0
- [x] Loop 0 integration tests still pass
- [x] Loop 1 integration tests pass
- [x] CLI smoke tests pass (new commands)
