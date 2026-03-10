# ADR-0001: Sentinel as Go Process with Targeted AI Call-outs

Status: accepted
Date: 2026-02-26
Loop: 3

## Context

The target architecture (Section 3.8) originally specified the sentinel as
an "AI Agent, Per-World" — implying a full Claude Code session running in
tmux, similar to a outpost. The sentinel's job is to patrol outposts,
detect stalled/zombie sessions, trigger recovery, and nudge stuck agents.

During Loop 3 prompt design, we evaluated what actually requires AI
judgment versus what is deterministic:

- Session liveness check → deterministic (tmux session exists or not)
- Tether state check → deterministic (file exists or not)
- Stalled detection → deterministic (session dead + tether set)
- Zombie detection → deterministic (session alive + idle + no tether)
- Respawn logic → deterministic (count attempts, start tmux session)
- "Is this agent stuck or just thinking?" → requires judgment

Only the last item needs AI. Running a full Claude Code session 24/7 per
world for a patrol loop that is 95% deterministic wastes API credits and
adds an unnecessary failure mode (the AI session itself can get stuck,
context-compacted, or confused).

## Decision

Implement the sentinel as a Go process that uses targeted `claude -p`
call-outs for the judgment calls only.

The patrol loop, state detection, respawn logic, and zombie cleanup are
all deterministic Go code. When the heuristic detects potential trouble
(tmux output hash unchanged between consecutive patrols), the sentinel
shells out to `claude -p` with the captured session output and a
structured prompt requesting a JSON assessment. The assessment determines
whether to nudge, escalate, or do nothing.

The assessment command is configurable (`AssessCommand` in sentinel
config) so the autarch can substitute a different model or tool.

## Consequences

**Benefits:**
- Patrol loop is fast, cheap, and deterministic — no API costs for
  routine health checks
- AI calls only fire when the heuristic triggers (~few per hour in
  practice), keeping costs proportional to actual stuck agents
- No risk of the sentinel itself getting stuck, context-compacted, or
  confused — it's a Go binary
- Easier to test — mock the assessment function, test patrol logic in
  isolation
- Prefect can restart the sentinel instantly (Go binary, not a
  Claude session that needs priming)

**Tradeoffs:**
- Less flexible than a full AI agent — the sentinel can't reason about
  novel situations outside its coded patrol logic
- AI assessment quality depends on the prompt and the captured output
  window (~80 lines may miss important context)
- Low-confidence assessments are discarded (safety-first), which means
  some stuck agents may take an extra patrol cycle to detect

**Mitigations:**
- Low confidence → no action (wait for next patrol, reassess)
- Assessment failure → non-blocking (patrol continues)
- The Consul (Loop 5) will provide AI-agent-level judgment for
  situations the sentinel can't handle
