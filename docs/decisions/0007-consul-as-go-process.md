# ADR-0007: Consul as Go Process (Not Full Claude Session)

Status: accepted
Date: 2026-02-27
Loop: 5

## Context

The target architecture (Section 3.7) describes the consul as a
"sphere-level AI agent" — language that could imply a full Claude session
like the forge (ADR-0005). The Gastown prototype ran its consul as a
persistent Claude session executing a patrol workflow with full AI
reasoning on every cycle.

ADR-0001 noted that "the Consul (Loop 5) will provide AI-agent-level
judgment for situations the sentinel can't handle." This left the
implementation open.

We evaluated the Gastown consul's actual behavior to determine what
required AI judgment versus deterministic logic:

**Mechanical (no AI needed):**
- Stale tether detection: session dead + age > threshold → unhook
- Re-dispatch attempt counting: counter + cooldown → escalate at limit
- Stranded caravan detection: open caravan + ready items + no assignee
- Rate limiting: cooldown timers per caravan/bead
- Heartbeat management: write timestamp to file
- Lifecycle request processing: read mail, match subject, act

**Judgment calls in Gastown (AI used):**
- Health assessment with context: "sentinel silent 4 cycles" means
  different things depending on whether the sphere has 20 in-progress
  items or is idle at 3 AM
- Escalation narratives: crafting informative mail explaining what
  failed and suggesting recovery actions
- Pattern recognition: detecting "poison work" that repeatedly fails
  across dispatches

**Key difference from the forge:** The forge is a Claude session
because merge conflict resolution truly requires reading code and
understanding semantics — there is no deterministic substitute. The
consul's judgment calls either (a) are already covered by the sentinel's
per-world `claude -p` callouts, (b) can be handled with structured
templates and counters, or (c) are future enhancements that fit the
targeted callout pattern.

The Gastown experience also showed that AI layers in the supervision
chain were a source of bugs at layer boundaries. The three-layer chain
(Daemon → Boot → Consul) produced real failures where an intermediate
AI layer hung, preventing supervision of the layers below. The sol
rewrite eliminated the Boot layer for exactly this reason. Making the
consul a full Claude session would reintroduce a fragile AI layer in
the supervision chain.

## Decision

Implement the consul as a Go process, following the same pattern as the
sentinel (ADR-0001). The patrol loop, stale tether recovery, caravan
feeding, and lifecycle processing are all deterministic Go code.

If sphere-level AI assessment is needed in the future (e.g., cross-world
pattern recognition, contextual health assessment), it will be added
as targeted `claude -p` callouts gated by heuristics — the same
pattern the sentinel uses successfully.

The consul registers as agent `sphere/consul` with role `consul`. The
prefect monitors it via heartbeat file freshness, not session
liveness.

## Consequences

**Benefits:**
- Patrol loop is fast, cheap, and deterministic — no API costs for
  routine coordination
- No risk of the consul itself getting stuck or context-compacted
- Prefect can restart instantly (Go binary, not a Claude session
  needing priming)
- Fully testable without mocking AI responses
- Supervision chain stays clean: Prefect (Go) → Consul (Go) →
  no fragile AI middle layer

**Tradeoffs:**
- Cannot reason about novel cross-world failure patterns (mitigated:
  add targeted `claude -p` callouts when needed)
- Escalation messages are template-based, not AI-crafted (mitigated:
  structured templates with full context are adequate for autarch
  triage)
- "Poison work" detection uses simple counters, not pattern recognition
  (mitigated: counter-based detection covers the common case; edge
  cases can escalate to the autarch)

**Comparison with other components:**
- Sentinel (ADR-0001): Go process + targeted `claude -p` → consul
  follows the same pattern
- Forge (ADR-0005): Claude session → justified because conflict
  resolution has no deterministic substitute
- Consul: Go process → justified because coordination tasks are
  mechanical with judgment as a future enhancement
