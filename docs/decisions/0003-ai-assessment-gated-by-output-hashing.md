# ADR-0003: AI Assessment Gated by tmux Output Hashing

Status: accepted
Date: 2026-02-26
Loop: 3

## Context

The witness needs to detect when a polecat is stuck. A polecat's session
is alive, it has work on its hook, but it isn't making progress. The
architecture spec (Section 3.8) described this as "no progress for
threshold → nudge" without specifying the mechanism.

We considered three approaches:

1. **Heuristic only:** No output change for N minutes → send a generic
   nudge ("Are you stuck? Run `gt escalate`"). Cheap but dumb — can't
   distinguish "stuck" from "running a long compilation" or "waiting
   for a large file write."

2. **AI on every patrol:** Capture output, send to AI, get assessment
   for every working agent on every patrol cycle. Accurate but
   expensive — at 3-minute intervals with 30 agents, that's 600 AI
   calls per hour regardless of whether agents are stuck.

3. **Heuristic-gated AI:** Hash tmux output between patrols. Only call
   AI when the hash is unchanged (suggesting no visible progress). Most
   agents show output changes on each cycle, so AI calls only fire for
   the few that don't.

## Decision

Use approach 3: heuristic-gated AI assessment.

On each patrol, the witness captures the last ~80 lines of each working
polecat's tmux output and computes a SHA-256 hash. It compares this hash
with the previous patrol's hash for that agent. If the hash changed, the
agent is making visible progress — no AI call needed. If unchanged, the
witness sends the captured output to `claude -p` for assessment.

The AI returns a structured JSON response with status (progressing /
stuck / waiting / idle), confidence (high / medium / low), suggested
action (none / nudge / escalate), and an optional nudge message.

Low-confidence assessments are always treated as "no action" regardless
of the suggested action.

## Consequences

**Benefits:**
- AI costs scale with stuck agents, not total agents — in a healthy
  system with 30 agents, maybe 0-2 AI calls per patrol cycle
- The heuristic catches the obvious "nothing is happening" case cheaply
- The AI call handles the nuanced cases (long-running compilation vs
  genuinely stuck)
- First patrol for each agent establishes a baseline hash with no AI
  call

**Tradeoffs:**
- One patrol cycle delay: the first patrol with unchanged output
  triggers assessment, so detection latency is 2× patrol interval
  (6 minutes with default 3-minute patrol) rather than 1×
- Hash comparison is coarse — if the agent is producing output but not
  making meaningful progress (e.g., looping the same error), the hash
  changes and no AI call fires. These cases are caught by the AI on
  the next unchanged cycle, or by the operator noticing in the feed.
- ~80 lines of captured output may miss important context earlier in
  the session

**Mitigations:**
- Assessment failure (timeout, parse error) is non-blocking — patrol
  continues normally
- The assessment command is configurable — operators can point to a
  cheaper/faster model if costs are a concern
- Capture line count is configurable for operators who want more context
