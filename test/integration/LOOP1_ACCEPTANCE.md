# Loop 1 Acceptance Criteria

## 1. Multi-agent dispatch
- [x] `sol cast <item1> myworld && sol cast <item2> myworld` dispatches to two different outposts
- [x] Each outpost has a unique name from the name pool
- [x] Each outpost runs in its own worktree and tmux session

## 2. Dispatch serialization
- [x] No two outposts get the same work item (flock prevents races)
- [x] Concurrent `sol cast` for the same item: one wins, one fails with contention error

## 3. Name pool
- [x] Auto-provisioned agents get names from the embedded pool (Toast, Jasper, ...)
- [x] Custom `$SOL_HOME/{world}/names.txt` overrides the default pool
- [x] Pool exhaustion returns a clear error

## 4. Prefect — crash detection and restart
- [x] `sol prefect run` starts the prefect (foreground, PID file written)
- [x] Prefect is sphere-level — monitors all worlds from one process
- [x] Kill a outpost's tmux session → prefect detects and restarts within heartbeat interval
- [x] Restarted outpost picks up tethered work (GUPP principle)

## 5. Prefect — backoff
- [x] Repeated crashes of the same agent increase restart delay
- [x] Backoff resets when agent completes work normally

## 6. Prefect — mass-death protection
- [x] Kill 3+ sessions in 30s → prefect enters degraded mode (no respawns)
- [x] Degraded mode auto-recovers after 5 minutes of quiet

## 7. Prefect — lifecycle
- [x] `sol prefect stop` sends SIGTERM, prefect stops all sessions gracefully
- [x] Only one prefect instance (PID file guard)
- [x] Stale PID files from crashed prefects are detected and overwritten

## 8. Status command
- [x] `sol status myworld` shows agents, sessions, tethered work, prefect state
- [x] `sol status myworld --json` outputs valid JSON with all fields
- [x] Exit code 0 = healthy, 1 = dead sessions, 2 = degraded/no prefect

## 9. All tests pass
- [x] `make test` exits 0
- [x] Loop 0 integration tests still pass
- [x] Loop 1 integration tests pass
- [x] CLI smoke tests pass (new commands)
