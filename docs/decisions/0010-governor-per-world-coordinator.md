# ADR-0010: Governor as Per-World Work Coordinator

Status: accepted
Date: 2026-02-28
Arc: 3

## Context

Work dispatch in Sol requires multiple CLI commands: create work items, form
caravans, add items, dispatch with cast. For an operator managing 10-30+
agents across a world, this is friction. The Gastown prototype addressed this
with a "mayor" — a persistent sphere-scoped AI agent that handled work
coordination.

However, Sol's architecture consistently decomposes responsibilities rather
than bundling them into monolithic agents:
- Sentinel: per-world health monitoring (ADR-0001)
- Forge: per-world merge pipeline (ADR-0005)
- Consul: sphere-wide infrastructure patrol (ADR-0007)

The Gastown mayor bundled three capabilities: natural language dispatch,
proactive coordination, and operator onboarding. These are independent concerns
that don't need to be a single persistent process.

## Decision

Decompose the Gastown mayor into three independent capabilities:

1. **Natural language dispatch → Governor** (this ADR)
2. **Proactive coordination → Consul AI enhancement** (future arc)
3. **Conversational onboarding → `sol init` Claude mode** (Arc 2)

The **governor** is a per-world singleton Claude session backed by sol CLI
subcommands. It follows the forge architecture pattern (ADR-0005): Claude runs
the coordination logic, Go subcommands handle mechanical operations.

**What the governor does:**
- Parses natural language work requests into work items
- Groups related items into caravans
- Dispatches work to available outposts via `sol cast`
- Tracks completion and suggests next priorities
- Accumulates knowledge about the world via the brief system (ADR-0009)

**What the governor does NOT do:**
- Health monitoring (sentinel)
- Merge processing (forge)
- Infrastructure patrol (consul)
- Exploratory/research work (envoy)

**Read-only codebase mirror:**

The governor does not edit code. Instead it has a read-only checkout of main
at `$SOL_HOME/{world}/governor/mirror/` for codebase research — reading files,
searching for patterns, understanding project structure. This helps the
governor create better work items and make informed coordination decisions.

The mirror auto-refreshes on session start via a `SessionStart` hook that runs
`git pull`. The governor's CLAUDE.md also instructs it to pull before major
research tasks.

**Brief system:**

The governor uses the same brief infrastructure as envoys (ADR-0009). Its
brief at `.brief/memory.md` accumulates knowledge about the world — project
patterns, agent capabilities, work history, and operator preferences.

**Per-world scope:**

The governor is scoped to a single world (not sphere-wide). This matches the
existing agent identity model (`{world}/{name}`) and the per-world singleton
pattern used by forge and sentinel. Cross-world coordination remains a consul
responsibility.

## Consequences

**Benefits:**
- Natural language dispatch reduces operator friction from 10+ CLI commands
  to a single conversational request
- Per-world scope keeps the governor focused and its brief relevant
- Follows established patterns: Claude session + Go toolbox (ADR-0005),
  per-world singleton (forge, sentinel)
- Brief accumulates world knowledge — governor improves over time
- No new schema changes (uses existing agents table with `role=governor`)

**Tradeoffs:**
- API cost proportional to interaction frequency (but only active when
  operator is dispatching work, not always-on)
- No sphere-wide coordination (consul handles cross-world concerns)
- Governor depends on sol CLI commands being correct and well-documented
  in its CLAUDE.md

**Code changes:**
- `protocol`: new `GovernorClaudeMD()` generator with sol CLI reference
- New `internal/governor/` package for governor lifecycle (mirror setup,
  session management)
- New `cmd/governor.go` for CLI commands
- `SessionStart` hook for mirror auto-refresh
