# ADR-0007: Deacon as Go Process (Not Full Claude Session)

Status: accepted
Date: 2026-02-27
Loop: 5

## Context

The target architecture (Section 3.7) describes the deacon as a
"town-level AI agent" — language that could imply a full Claude session
like the refinery (ADR-0005). The Gastown prototype ran its deacon as a
persistent Claude session executing a patrol formula with full AI
reasoning on every cycle.

ADR-0001 noted that "the Deacon (Loop 5) will provide AI-agent-level
judgment for situations the witness can't handle." This left the
implementation open.

We evaluated the Gastown deacon's actual behavior to determine what
required AI judgment versus deterministic logic:

**Mechanical (no AI needed):**
- Stale hook detection: session dead + age > threshold → unhook
- Re-dispatch attempt counting: counter + cooldown → escalate at limit
- Stranded convoy detection: open convoy + ready items + no assignee
- Rate limiting: cooldown timers per convoy/bead
- Heartbeat management: write timestamp to file
- Lifecycle request processing: read mail, match subject, act

**Judgment calls in Gastown (AI used):**
- Health assessment with context: "witness silent 4 cycles" means
  different things depending on whether the town has 20 in-progress
  items or is idle at 3 AM
- Escalation narratives: crafting informative mail explaining what
  failed and suggesting recovery actions
- Pattern recognition: detecting "poison work" that repeatedly fails
  across dispatches

**Key difference from the refinery:** The refinery is a Claude session
because merge conflict resolution truly requires reading code and
understanding semantics — there is no deterministic substitute. The
deacon's judgment calls either (a) are already covered by the witness's
per-rig `claude -p` callouts, (b) can be handled with structured
templates and counters, or (c) are future enhancements that fit the
targeted callout pattern.

The Gastown experience also showed that AI layers in the supervision
chain were a source of bugs at layer boundaries. The three-layer chain
(Daemon → Boot → Deacon) produced real failures where an intermediate
AI layer hung, preventing supervision of the layers below. The gt
rewrite eliminated the Boot layer for exactly this reason. Making the
deacon a full Claude session would reintroduce a fragile AI layer in
the supervision chain.

## Decision

Implement the deacon as a Go process, following the same pattern as the
witness (ADR-0001). The patrol loop, stale hook recovery, convoy
feeding, and lifecycle processing are all deterministic Go code.

If town-level AI assessment is needed in the future (e.g., cross-rig
pattern recognition, contextual health assessment), it will be added
as targeted `claude -p` callouts gated by heuristics — the same
pattern the witness uses successfully.

The deacon registers as agent `town/deacon` with role `deacon`. The
supervisor monitors it via heartbeat file freshness, not session
liveness.

## Consequences

**Benefits:**
- Patrol loop is fast, cheap, and deterministic — no API costs for
  routine coordination
- No risk of the deacon itself getting stuck or context-compacted
- Supervisor can restart instantly (Go binary, not a Claude session
  needing priming)
- Fully testable without mocking AI responses
- Supervision chain stays clean: Supervisor (Go) → Deacon (Go) →
  no fragile AI middle layer

**Tradeoffs:**
- Cannot reason about novel cross-rig failure patterns (mitigated:
  add targeted `claude -p` callouts when needed)
- Escalation messages are template-based, not AI-crafted (mitigated:
  structured templates with full context are adequate for operator
  triage)
- "Poison work" detection uses simple counters, not pattern recognition
  (mitigated: counter-based detection covers the common case; edge
  cases can escalate to operator)

**Comparison with other components:**
- Witness (ADR-0001): Go process + targeted `claude -p` → deacon
  follows the same pattern
- Refinery (ADR-0005): Claude session → justified because conflict
  resolution has no deterministic substitute
- Deacon: Go process → justified because coordination tasks are
  mechanical with judgment as a future enhancement
