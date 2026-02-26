# ADR-0002: Refinery as Go Process

Status: accepted
Date: 2026-02-26
Loop: 2

## Context

The target architecture (Section 3.9) originally specified the refinery
as an "AI Agent, Per-Rig." The refinery's job is to process the merge
queue: claim merge requests, rebase onto the target branch, run quality
gates, and push.

During Loop 2 implementation, we evaluated the merge pipeline steps:

1. Poll merge_requests table for ready MRs → SQL query
2. Claim MR atomically → SQL UPDATE with RETURNING
3. Fetch and checkout refinery branch → git commands
4. Merge agent's branch → git merge --no-ff
5. Run quality gates → shell commands (go test, etc.)
6. Push to target branch → git push
7. Update MR phase and work item status → SQL updates
8. Clean up remote branch → git push --delete (best-effort)

Every step is a deterministic shell command or SQL operation. None
require AI judgment. The refinery never needs to read code, understand
context, or make subjective decisions — it executes a fixed pipeline.

## Decision

Implement the refinery as a Go process, not an AI agent session.

The refinery runs as `gt refinery run <rig>`, a long-running Go process
with a poll-based state machine. It uses git commands for merge
operations and SQL for queue management. No AI calls are involved.

## Consequences

**Benefits:**
- Zero API cost for merge operations — the refinery is pure
  infrastructure
- Faster merge cycle — no AI session startup, no prompt processing
- Deterministic behavior — same inputs always produce same outputs
- Simpler failure modes — process crash recovery is straightforward
  (TTL-based stale claim release)
- Easier to debug — structured JSON logs, no AI reasoning to trace

**Tradeoffs:**
- Cannot handle novel merge situations (e.g., semantic conflicts that
  pass git merge but break logic) — these surface as quality gate
  failures
- Cannot auto-resolve merge conflicts — conflicts result in
  phase=failed, deferred to rework pipeline (Loop 4)

**Note:** The architecture doc Section 3.9 title has been updated from
"AI Agent" to "Go Process" to reflect this decision.
