# Loop 2 Acceptance Criteria

## 1. Done creates merge request
- [x] `sol resolve` creates a merge request with `phase=ready` in the world store
- [x] MR has correct `writ_id` and `branch` fields
- [x] MR ID starts with `mr-` prefix
- [x] CLI output shows the merge request ID
- [x] Writ status is "done", agent is "idle" (existing behavior preserved)

## 2. Forge polls and claims
- [x] Forge polls `merge_requests` table for `phase=ready` items
- [x] Claims are atomic (UPDATE ... WHERE prevents races)
- [x] Higher priority MRs are processed first (lower number = higher priority)
- [x] Oldest MRs processed first within same priority (FIFO)

## 3. Forge rebases and tests
- [x] Forge rebases outpost's branch onto latest target branch
- [x] Quality gates run in the forge worktree
- [x] Quality gates are configurable via `quality-gates.txt`
- [x] Default quality gate is `go test ./...`

## 4. Merge on success
- [x] Tests pass → forge merges to target branch
- [x] MR phase updated to "merged", `merged_at` timestamp set
- [x] Writ status updated to "closed"
- [x] Outpost's remote branch cleaned up (best-effort)

## 5. Retry on failure
- [x] Quality gate failure → MR returned to "ready" for retry
- [x] Max 3 attempts before MR marked "failed"
- [x] Rebase conflict → MR immediately marked "failed"

## 6. Merge slot serialization
- [x] Only one merge in progress at a time per world (advisory file lock)
- [x] Different worlds can merge concurrently

## 7. TTL recovery
- [x] Stale claims (>30 min) automatically released to "ready"
- [x] Released MRs are picked up by the next poll cycle

## 8. Forge lifecycle
- [x] `sol forge run <world>` runs the merge loop in foreground
- [x] `sol forge start <world>` starts forge in tmux session
- [x] `sol forge stop <world>` stops the forge session
- [x] `sol forge attach <world>` attaches to the forge session

## 9. Operator visibility
- [x] `sol forge queue <world>` shows pending, claimed, merged, and failed MRs
- [x] `sol forge queue <world> --json` outputs valid JSON
- [x] `sol status <world>` shows forge running/stopped state
- [x] `sol status <world>` shows merge queue depth

## 10. Prefect integration
- [x] Prefect restarts crashed forge sessions
- [x] Forge respawned with `sol forge run <world>` (not claude)
- [x] Outpost respawn behavior unchanged

## 11. All tests pass
- [x] `make test` exits 0
- [x] Loop 0 integration tests still pass
- [x] Loop 1 integration tests still pass
- [x] Loop 2 integration tests pass
- [x] CLI smoke tests pass (new commands)
