# ADR-0010: Governor as Per-World Work Coordinator

Status: superseded by [ADR-0037](0037-remove-governor-role.md)
Date: 2026-02-28
Arc: 3

## Context

Work dispatch in Sol requires multiple CLI commands: create writs, form
caravans, add items, dispatch with cast. For the autarch managing 10-30+
agents across a world, this is friction. The Gastown prototype addressed this
with a "mayor" — a persistent sphere-scoped AI agent that handled work
coordination.

However, Sol's architecture consistently decomposes responsibilities rather
than bundling them into monolithic agents:
- Sentinel: per-world health monitoring (ADR-0001)
- Forge: per-world merge pipeline (ADR-0005)
- Consul: sphere-wide infrastructure patrol (ADR-0007)

The Gastown mayor bundled three capabilities: natural language dispatch,
proactive coordination, and autarch onboarding. These are independent concerns
that don't need to be a single persistent process.

## Options Considered

### 1. Unified mayor (Gastown model)

A single sphere-scoped persistent AI agent that handles dispatch, proactive
coordination, and autarch onboarding. The mayor would be always-on, expensive,
and monolithic — a single point of failure for three unrelated responsibilities.
Sol's architecture consistently decomposes responsibilities (sentinel, forge,
consul are all separate), and the mayor's capabilities have different scopes
(dispatch is per-world, coordination is sphere-wide, onboarding is one-time).

Rejected: bundling independent concerns into one agent violates Sol's
decomposition principle and creates unnecessary coupling.

### 2. Consul enhancement

Add natural language dispatch to consul via `claude -p` callouts. Consul
already has sphere visibility. But consul is a deterministic Go patrol process
(ADR-0007) — adding interactive, conversational dispatch changes its character
entirely. Dispatch requires accumulated world context; consul is stateless
by design.

Rejected: wrong abstraction. Patrol and dispatch are different activities.

### 3. Decomposed capabilities (chosen)

Split the mayor into three independent components matched to their natural
scope: governor (per-world dispatch), consul AI enhancement (sphere-wide
proactive coordination, future arc), and `sol init` Claude mode (one-time
onboarding, Arc 2).

## Decision

Decompose the Gastown mayor into three independent capabilities:

1. **Natural language dispatch → Governor** (this ADR)
2. **Proactive coordination → Consul AI enhancement** (future arc)
3. **Conversational onboarding → `sol init` Claude mode** (Arc 2)

The **governor** is a per-world singleton Claude session backed by sol CLI
subcommands. It follows the forge architecture pattern (ADR-0005): Claude runs
the coordination logic, Go subcommands handle mechanical operations.

**What the governor does:**
- Parses natural language work requests into writs
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
governor create better writs and make informed coordination decisions.

Three mirror approaches were considered:

- **Editable worktree** — governor could prototype or spike code. Rejected:
  muddies the role boundary. If the governor writes code, what goes through
  forge? The governor coordinates; envoys and outposts write code.
- **No codebase access** — governor works purely from its brief and autarch
  input. Rejected: governor can't create good writs without understanding
  the codebase structure. "Add auth middleware" is a worse writ than
  "extend `internal/auth/middleware.go` with OAuth2 support."
- **Read-only mirror** (chosen) — governor reads the codebase but can't modify
  it. Clean role boundary, better writ quality.

The mirror auto-refreshes on session start via a `SessionStart` hook that runs
`git pull`. The governor's CLAUDE.md also instructs it to pull before major
research tasks. Event-driven refresh (post-merge hook from forge) was
considered but adds coupling between forge and governor for marginal freshness
improvement.

**Brief system:**

The governor uses the same brief infrastructure as envoys (ADR-0009). Its
brief at `.brief/memory.md` accumulates knowledge about the world — project
patterns, agent capabilities, work history, and autarch preferences.

**Per-world scope:**

The governor is scoped to a single world (not sphere-wide). This matches the
existing agent identity model (`{world}/{name}`) and the per-world singleton
pattern used by forge and sentinel. Cross-world coordination remains a consul
responsibility.

## Consequences

**Benefits:**
- Natural language dispatch reduces autarch friction from 10+ CLI commands
  to a single conversational request
- Per-world scope keeps the governor focused and its brief relevant
- Follows established patterns: Claude session + Go toolbox (ADR-0005),
  per-world singleton (forge, sentinel)
- Brief accumulates world knowledge — governor improves over time
- No new schema changes (uses existing agents table with `role=governor`)

**Tradeoffs:**
- API cost proportional to interaction frequency (but only active when
  autarch is dispatching work, not always-on)
- No sphere-wide coordination (consul handles cross-world concerns)
- Governor depends on sol CLI commands being correct and well-documented
  in its CLAUDE.md

**Code changes:**
- `protocol`: new `GovernorClaudeMD()` generator with sol CLI reference
- New `internal/governor/` package for governor lifecycle (mirror setup,
  session management)
- New `cmd/governor.go` for CLI commands
- `SessionStart` hook for mirror auto-refresh
