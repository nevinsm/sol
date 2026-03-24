# ADR-0013: Brief System for Context Persistence

Status: accepted
Date: 2026-03-01
Arc: 3

## Context

Envoys (ADR-0009), governors (ADR-0010), and the chancellor (ADR-0011) are all
persistent Claude sessions that accumulate valuable context over their
lifetime — decisions made, patterns discovered, autarch preferences learned.
When a session ends (clean exit, crash, or context compaction), that knowledge
is lost unless explicitly persisted.

Claude Code has a built-in memory system (`.claude/` auto-memory) but it is
scoped to the Claude Code instance, not to Sol's agent lifecycle. Sol needs
its own context persistence that:

- Survives session restarts and crashes
- Re-injects after context compaction (which discards earlier conversation)
- Is GLASS-inspectable (the autarch can `cat` the file anytime)
- Works across envoy, governor, and chancellor with shared infrastructure

## Options Considered

### 1. System-captured context (automatic summarization)

Sol captures session output at key points and uses AI to summarize it into
a context file. Consistent quality, no agent cooperation required.

Rejected: adds AI summarization cost on every capture, introduces a second
AI call that can fail or produce poor summaries, and the system can't know
what the agent considers important. The agent has the best judgment about
what matters in its accumulated context.

### 2. Structured database storage

Store context as rows in SQLite — key-value pairs, tagged entries, or
structured records. Queryable, schema-enforced.

Rejected: context is inherently unstructured and varies by role. An envoy's
context (design decisions, code patterns) looks nothing like a governor's
(agent capabilities, work history). Forcing structure adds friction without
benefit. A markdown file is more natural for AI agents to read and write.

### 3. Agent-maintained brief files (chosen)

The agent maintains its own context in a plain markdown file
(`.brief/memory.md`). CLAUDE.md instructs the agent what to capture and how
to organize it. Claude Code hooks handle injection and save-checking.

## Decision

Context persistence uses **agent-maintained brief files** with a three-layer
size management system.

**Files:**

| File | Owner | Purpose |
|------|-------|---------|
| `.brief/memory.md` | envoy, governor, chancellor | Internal accumulated knowledge |
| `.brief/world-summary.md` | governor only | External-facing world summary for chancellor and the autarch |

The `memory.md` file is freeform — the agent organizes it naturally, same
model as Claude Code's own MEMORY.md. The `world-summary.md` has prescribed
sections (Project, Architecture, Priorities, Constraints) for consistency,
since chancellor and the autarch depend on predictable structure.

**Injection via Claude Code hooks:**

- `SessionStart` (startup/resume) — `sol brief inject` reads the brief,
  truncates if needed, outputs framed content for injection.
- `SessionStart` (compact) — same injection, re-injects after compaction.

**No Stop hook — CLAUDE.md instructions instead:**

The Stop hook was removed because Claude Code's "Stop" event fires on every
turn completion, not just session exit. This caused noisy false positives
(nudge messages mid-conversation). Brief update responsibility is instead
conveyed through CLAUDE.md instructions which tell the agent to update its
brief before ending a session.

**Three-layer size management:**

1. **CLAUDE.md guidance (soft)** — instructs the agent to keep the brief
   under 200 lines, consolidate older entries, and focus on current state,
   key decisions, and next steps.
2. **AI self-management** — the agent is responsible for its own brief
   content and size. The Stop hook provides a natural consolidation point.
3. **Injection truncation (hard safety net)** — `sol brief inject
   --max-lines=200` truncates if the brief exceeds the limit and tells the
   agent to consolidate. This prevents a runaway brief from consuming the
   context window even if the agent neglects self-management.

The 200-line limit matches Claude Code's own MEMORY.md truncation. It keeps
injection from consuming excessive context while leaving room for meaningful
accumulated knowledge.

## Consequences

**Benefits:**
- Zero AI overhead — no summarization calls, agent writes its own context
- GLASS-inspectable — `cat .brief/memory.md` shows exactly what the agent
  knows from previous sessions
- Shared infrastructure — same hooks and CLI commands across envoy, governor,
  and chancellor
- Graceful degradation — missing brief = clean start, stale brief = reduced
  context (not failure), truncated brief = agent prompted to consolidate

**Tradeoffs:**
- Brief quality depends on agent cooperation (mitigated by CLAUDE.md
  instructions)
- Crash recovery is lossy — brief reflects whatever was last written, which
  may be mid-session stale. CLAUDE.md warns the agent to review on startup.
- No versioning or history — brief is overwritten in place. Previous versions
  are only recoverable from filesystem snapshots or backups.

**Code changes:**
- New `internal/brief/` package — injection (read, truncate, frame)
- New `cmd/brief.go` — `sol brief inject`
- `sol envoy start`, `sol governor start`, `sol chancellor start` — write
  `.claude/settings.json` with hook configuration
- `protocol` — `EnvoyClaudeMD()`, `GovernorClaudeMD()`, `ChancellorClaudeMD()`
  include brief maintenance instructions
