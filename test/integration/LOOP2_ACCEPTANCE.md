# Loop 2 Acceptance Criteria

## 1. Done creates merge request
- [x] `gt done` creates a merge request with `phase=ready` in the rig store
- [x] MR has correct `work_item_id` and `branch` fields
- [x] MR ID starts with `mr-` prefix
- [x] CLI output shows the merge request ID
- [x] Work item status is "done", agent is "idle" (existing behavior preserved)

## 2. Refinery polls and claims
- [x] Refinery polls `merge_requests` table for `phase=ready` items
- [x] Claims are atomic (UPDATE ... WHERE prevents races)
- [x] Higher priority MRs are processed first (lower number = higher priority)
- [x] Oldest MRs processed first within same priority (FIFO)

## 3. Refinery rebases and tests
- [x] Refinery rebases polecat's branch onto latest target branch
- [x] Quality gates run in the refinery worktree
- [x] Quality gates are configurable via `quality-gates.txt`
- [x] Default quality gate is `go test ./...`

## 4. Merge on success
- [x] Tests pass → refinery merges to target branch
- [x] MR phase updated to "merged", `merged_at` timestamp set
- [x] Work item status updated to "closed"
- [x] Polecat's remote branch cleaned up (best-effort)

## 5. Retry on failure
- [x] Quality gate failure → MR returned to "ready" for retry
- [x] Max 3 attempts before MR marked "failed"
- [x] Rebase conflict → MR immediately marked "failed"

## 6. Merge slot serialization
- [x] Only one merge in progress at a time per rig (advisory file lock)
- [x] Different rigs can merge concurrently

## 7. TTL recovery
- [x] Stale claims (>30 min) automatically released to "ready"
- [x] Released MRs are picked up by the next poll cycle

## 8. Refinery lifecycle
- [x] `gt refinery run <rig>` runs the merge loop in foreground
- [x] `gt refinery start <rig>` starts refinery in tmux session
- [x] `gt refinery stop <rig>` stops the refinery session
- [x] `gt refinery attach <rig>` attaches to the refinery session

## 9. Operator visibility
- [x] `gt refinery queue <rig>` shows pending, claimed, merged, and failed MRs
- [x] `gt refinery queue <rig> --json` outputs valid JSON
- [x] `gt status <rig>` shows refinery running/stopped state
- [x] `gt status <rig>` shows merge queue depth

## 10. Supervisor integration
- [x] Supervisor restarts crashed refinery sessions
- [x] Refinery respawned with `gt refinery run <rig>` (not claude)
- [x] Polecat respawn behavior unchanged

## 11. All tests pass
- [x] `make test` exits 0
- [x] Loop 0 integration tests still pass
- [x] Loop 1 integration tests still pass
- [x] Loop 2 integration tests pass
- [x] CLI smoke tests pass (new commands)
