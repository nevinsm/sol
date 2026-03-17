# ADR-0028: Event-Driven Forge with Go Orchestration Shell

Status: accepted (supersedes ADR-0027, supersedes ADR-0005, supersedes ADR-0017)
Date: 2026-03-09

## Context

ADR-0027 moved the forge from a workflow-constrained Claude session to a
deterministic Go process. This eliminated drift, context rot, and API cost
during normal merges — but it also eliminated judgment capability. The Go
process cannot resolve merge conflicts; it dispatches resolution tasks to
outpost agents, adding minutes of wall time per conflict. Gate failure
analysis is limited to mechanical heuristics (the Scotty Test).

The event system and session injection infrastructure (ADR-0023) now enable
a different model: ephemeral Claude sessions that receive work only when
needed. This eliminates the polling loop that caused the original Claude
session (ADR-0005) to fail while preserving the judgment capability that
the deterministic Go process (ADR-0027) lacks.

The key insight is that the forge's two concerns have different
characteristics:

- **Orchestration** (poll queue, claim MRs, record outcomes, heartbeat) is
  mechanical and runs indefinitely — a perfect fit for Go.
- **Merge execution** (sync, merge, conflict resolution, gate analysis,
  push) benefits from judgment but is bounded to a single MR — a perfect
  fit for a short-lived Claude session.

The sentinel (ADR-0001, ADR-0003) and consul (ADR-0007) proved the
"deterministic Go + targeted AI" pattern. This ADR extends it: instead of
`claude -p` callouts at failure points, the forge starts a full Claude
session per merge task, giving the agent enough context and capability to
resolve conflicts inline rather than delegating them.

## Decision

Forge becomes a **Go orchestration shell** that starts **ephemeral Claude
sessions** for merge execution.

### Go Orchestration Shell (continuous process)

The Go process retains all existing orchestration logic:

- Polls merge queue, claims MRs (unchanged)
- Writes heartbeat, emits events (unchanged)
- Runs as a direct background process under prefect supervision (unchanged)

New responsibilities:

- **Session lifecycle.** Starts a fresh Claude session per claimed MR.
  Stops and cleans up the session after completion or timeout.
- **Context injection.** Builds a comprehensive injection message from MR
  metadata, writ description, gate commands, and attempt history. Injected
  at session start (see Injection Protocol below).
- **Activity monitoring.** Monitors session progress using the output hash
  pattern from sentinel (see Monitoring Specification below).
- **Result collection.** Reads `.forge-result.json` from the worktree
  after session completion (see Result Protocol below).
- **State transitions.** Go always owns state transitions —
  `MarkMerged`, `MarkFailed`, or `CreateResolutionTask` — based on the
  result file contents. If the session crashes (no result file), Go
  releases the MR.

### Per-MR Claude Session (ephemeral)

Each merge task gets a fresh Claude session with:

- **Tight persona.** Senior merge engineer — no feature work, no code
  exploration, no modifications beyond merge scope.
- **Full merge context.** MR details, writ description, gate commands,
  attempt history, and the target branch — everything needed to execute
  the merge without prior context.
- **Scoped operations.** Sync branch, merge to target, resolve conflicts,
  run gates, push. Reports result via `.forge-result.json`.
- **Session recycling.** Fresh session per MR. No state accumulation
  across merges, no drift, no context compaction needed.

### Key Design Decisions

**1. Session recycling — fresh session per MR.**

No state carries between merges. Each session starts clean with
comprehensive injection. This eliminates drift (the failure mode that
drove ADR-0017) and context rot (the failure mode that drove ADR-0027).
The cost is one session startup per merge — acceptable given that merge
execution dominates the time budget.

**2. Go owns all state transitions.**

Claude never calls `MarkMerged`, `MarkFailed`, or `CreateResolutionTask`
directly. The session reports its result via a file; Go reads the file and
makes the state transition. This provides crash safety: if Claude crashes
mid-merge, Go detects the missing result file and releases the MR for
retry. It also provides auditability: all state transitions flow through
Go code with structured logging.

**3. Activity-based monitoring (no hard timeout).**

The forge monitors each merge session using the same pattern as sentinel's
`checkProgress` (ADR-0003):

- Capture last ~80 lines of tmux output
- SHA256 hash comparison every 3 minutes
- If hash unchanged, AI assessment (`claude -p`) determines if the
  session is progressing or stuck
- No hard timeout — a session running gates for 20 minutes with changing
  output is not stuck

This avoids the false-positive problem of hard timeouts while catching
genuinely stuck sessions.

**4. Resolution task as last resort.**

The Claude session resolves conflicts inline when possible — this is the
judgment capability that ADR-0027 lacked. `CreateResolutionTask` is only
used when the session reports `"conflict"` (couldn't resolve after
reasonable effort). This reduces wall time for routine conflicts from
minutes (outpost dispatch + execution) to seconds (inline resolution).

**5. Persona constraints enforce scope.**

The session persona explicitly prohibits:

- Feature work or code modifications beyond merge scope
- Code exploration or refactoring
- Direct state transitions (MarkMerged, MarkFailed, etc.)
- Force push, branch creation, or destructive git operations

Tools are limited to git merge operations and gate command execution.
The persona is installed via `CLAUDE.local.md` in the worktree root,
following the existing agent startup pattern (ADR-0023).

### Injection Protocol

Go builds the injection message from structured data and injects it at
session start. The injection template:

```
=== FORGE MERGE TASK ===

## Merge Request
- MR ID: {mr_id}
- Branch: {source_branch} → {target_branch}
- Writ: {writ_id}
- Title: {writ_title}
- Attempt: {attempt_number} of {max_attempts}

## Writ Description
{writ_description}

## Gate Commands
{gate_commands}

## Attempt History
{attempt_history}
(empty on first attempt; on retries, includes prior result summaries)

## Instructions
1. Sync the source branch with the target branch
2. Merge (squash) the source branch into the target branch
3. If conflicts arise, resolve them — prefer the source branch's intent
4. Run all gate commands; if gates fail, analyze the failure
5. Push to the target branch
6. Write your result to .forge-result.json in the worktree root

## Result Protocol
Write a JSON file at .forge-result.json with this schema:
{
  "result": "merged" | "failed" | "conflict",
  "summary": "human-readable description of what happened",
  "files_changed": ["list", "of", "files", "modified"],
  "gate_output": "stdout/stderr from gate commands (if relevant)"
}

- "merged": merge and push succeeded
- "failed": gates failed or push rejected (not a conflict issue)
- "conflict": conflicts could not be resolved after reasonable effort
=== END TASK ===
```

The injection is delivered via the session's initial prompt argument
(the `--prompt` flag on `claude`), ensuring the session starts with full
context before any tool use.

### Result Protocol

File-based result reporting via `.forge-result.json` in the worktree root.

**Schema:**

```json
{
  "result": "merged | failed | conflict",
  "summary": "string — human-readable description of the outcome",
  "files_changed": ["string — list of files modified during merge"],
  "gate_output": "string — gate command output (optional, included on failure)"
}
```

**Result values:**

| Result       | Go Action                | Description                                    |
|--------------|--------------------------|------------------------------------------------|
| `"merged"`   | `MarkMerged()`           | Merge and push succeeded                       |
| `"failed"`   | `MarkFailed()`           | Gates failed or push rejected                  |
| `"conflict"` | `CreateResolutionTask()` | Conflicts unresolvable by the merge session    |

**Missing result file:** If the session terminates without writing a
result file (crash, timeout, stuck detection), Go releases the MR
(decrements attempt counter, makes it available for re-claim).

### Monitoring Specification

The Go orchestrator monitors each active merge session:

1. **Baseline capture.** On session start, capture last 80 lines of tmux
   output and compute SHA256 hash. Store as baseline.

2. **Periodic check.** Every 3 minutes (configurable via
   `world.toml` forge section):
   - Capture last 80 lines of tmux output
   - Compute SHA256 hash
   - Compare with previous hash

3. **Hash changed.** Session is making progress. Update stored hash,
   continue monitoring.

4. **Hash unchanged.** Session may be stuck. Invoke AI assessment:
   - Send captured output to `claude -p` with assessment prompt
   - AI returns structured JSON: `{ status, confidence, reason }`
   - If status is `"stuck"` with high confidence: stop session, treat as
     missing result file (release MR)
   - If status is `"progressing"` or confidence is low: continue
     monitoring

5. **Session exit.** When the session process exits (detected via tmux):
   - Read `.forge-result.json` from worktree
   - Execute corresponding state transition
   - Clean up session and worktree

This is the same pattern proven by sentinel (ADR-0003), adapted for
merge session monitoring. No hard timeouts — activity determines liveness.

## Consequences

- **Inline conflict resolution.** Routine merge conflicts are resolved
  in seconds by the merge session, not minutes by a dispatched outpost.
  `CreateResolutionTask` becomes a last resort for genuinely complex
  conflicts.

- **Judgment where it matters.** The Claude session provides judgment for
  conflict resolution and gate failure analysis — the two areas where
  ADR-0027's deterministic approach fell short. Orchestration remains
  deterministic Go.

- **No drift, no context rot.** Session recycling (fresh per MR) prevents
  the failure modes that plagued ADR-0005 (persona drift) and ADR-0017
  (context compaction artifacts). Each session starts clean.

- **Crash safety via file protocol.** Go owns all state transitions and
  reads results from a file. If Claude crashes, Go detects the missing
  result and releases the MR. No orphaned state transitions.

- **API cost proportional to merge activity.** One Claude session per
  merge task. No idle polling sessions. During quiet periods, only the
  Go orchestrator runs (zero API cost, same as ADR-0027).

- **Monitoring reuses proven pattern.** The output hash + AI assessment
  pattern from sentinel (ADR-0003) is applied to merge sessions. No new
  monitoring infrastructure needed.

- **Existing Go toolbox preserved.** All `MarkMerged`, `MarkFailed`,
  `CreateResolutionTask`, queue management, and gate execution code
  remains unchanged. The change is in how merge execution is performed
  (Claude session vs Go code), not in how results are recorded.

- **Session startup overhead.** Each merge incurs Claude session startup
  cost (~5-10 seconds). Acceptable given that merge execution (sync,
  merge, gates, push) typically takes 30-120 seconds.

- **Forge architecture history.** The forge has now evolved through four
  architectures: pure Go (ADR-0002) → Claude session (ADR-0005) →
  workflow-constrained session (ADR-0017) → deterministic Go (ADR-0027)
  → Go shell + ephemeral sessions (this ADR). Each iteration addressed
  failure modes discovered in the previous approach.
