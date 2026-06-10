# ADR-0040: Operator-Managed Credentials and Machinery Removal

Status: Proposed
Date: 2026-06-06

## Context

Sol's value is the orchestration layer above the runtime: tether durability,
writ lifecycle, caravan phase sequencing, workflow execution, forge merge
pipeline, the three-tier supervision triad (prefect/sentinel/consul), persona
resolution, and envoy persistent memory. These primitives are sol's core
contribution — they are what makes concurrent AI coding agents tractable.

The runtime layer and credential machinery have grown heavier than the
operating context justifies:

- **RuntimeAdapter interface** (`internal/adapter/`) currently defines 14
  methods, with per-runtime adapter packages for Claude and Codex. The
  interface captures real seams (hook injection, persona writing, command
  building, telemetry wiring), but the abstraction is over-specified for a
  system that has two runtimes and no concrete plans for a third.

- **Credential machinery** (`internal/account/`, `internal/quota/`,
  `internal/budget/`) implements multi-account OAuth token storage, sentinel-
  managed credential rotation on rate-limit detection, per-account quota
  tracking, and daily budget gating on dispatch. This machinery exists to
  autonomously recover when a single Claude account hits its rate limit.

The key insight is that the credential rotation machinery solves a problem sol
does not need to own. Emdash's architecture illustrates the right shape: a
676-line metadata table plus thin bridge functions supports 30 providers
without a credential storage or rotation layer. The abstraction is
configuration + a small interface, not a management system.

Sol operates in an autarch-monitored context. The autarch is present and
available; fully autonomous recovery from credential exhaustion adds complexity
without proportional value. Tether durability already ensures that work is not
lost when an agent stalls.

### What This Simplification Enables

Removing the credential management layer eliminates:
- The account/quota/budget code paths that every dispatch and sentinel patrol
  must navigate
- `sol account` and `sol quota` CLI commands that operators rarely use
- The `broker.Provider` interface's rate-limit detection contract, reducing
  broker to a liveness probe
- Sentinel's rate-limit detection branch, which has been a source of fragile
  pattern matching against runtime error output

## Decision

### 1. Operator-Managed Credentials

Credentials are managed by the operator via the runtime's native flow:

- Claude: `claude login` (OAuth) or `ANTHROPIC_API_KEY` (API key)
- Codex: `pi /login` or equivalent native flow
- Any future runtime: whatever that runtime requires

Sol never stores tokens, never runs OAuth flows, and never rotates
credentials. The operator is responsible for ensuring credentials are valid
before starting agents.

### 2. Per-Agent Config Dir Isolation with Static Credential Symlink

ADR-0018 (per-agent config dir isolation via `CLAUDE_CONFIG_DIR`) stays in
force unchanged. The credential binding mechanism is simplified:

- At spawn time, the agent's config dir receives a single symlink pointing to
  the operator-managed global credential location (e.g.,
  `~/.claude/.credentials.json`).
- The symlink is created once at spawn and never swapped. There is no rotation,
  so there is no race condition.
- No `$SOL_HOME/.accounts/` directory tree. No account registry.

### 3. Rate-Limit Behavior

When an account hits a rate limit:

- Affected agents fail (Claude Code exits or stalls with a rate-limit error).
- Sol does not autonomously recover. Sentinel detects the stall via the
  existing health monitor (output hash unchanged, no progress) and escalates
  to the autarch via the normal escalation path.
- The operator switches credentials at the runtime level (e.g., `claude login`
  with a different account) and respawns affected agents.
- Tether durability ensures no work is lost — the writ remains tethered and
  the worktree is intact.

**Trade-off accepted**: agents sit idle after a rate-limit hit until the
operator intervenes. This is acceptable because: (a) the autarch is present,
(b) rate limits reset quickly in practice, and (c) eliminating the autonomous
rotation machinery removes significant complexity and failure surface.

### 4. `sol cost` Becomes Reporting-Only

`sol cost` is retained as a reporting command over ledger data. The budget
enforcement gate (blocking dispatch when an account exceeds its daily limit)
is removed. Cost data remains available for operator awareness; enforcement
becomes an operator decision, not an automated gate.

### 5. Three-Tier Supervision Stays Unchanged

Prefect (sphere orchestrator), sentinel (per-world health monitor), and consul
(sphere patrol) continue operating as specified in ADR-0001, ADR-0006, and
ADR-0007. The simplification removes sentinel's rate-limit detection branch
but does not alter its overall patrol architecture.

## Consequences

### Code Removed

- `internal/account/` — account storage, OAuth token management, account CLI
  plumbing
- `internal/quota/` — per-account rate-limit state tracking
- `internal/budget/` — daily spend tracking and dispatch gating
- `sol account` and `sol quota` CLI command trees
- Sentinel's rate-limit detection branch (output pattern matching for Claude
  error messages, account rotation trigger)
- Account selection and budget gating in `internal/dispatch/`
- `broker.Provider` rate-limit detection and credential expiry contract
  (`internal/broker/` collapses to liveness probing — can the runtime process
  be launched successfully?)

### Interface Simplified

- `broker.Provider` interface reduced to liveness probing only. ADR-0036 is
  amended accordingly.

### Behavior Changes

- Dispatch no longer selects accounts or checks budgets — it spawns agents
  with whatever credentials the operator has configured.
- `sol cost` no longer enforces; it only reports.
- Sentinel no longer rotates credentials on rate-limit detection; it escalates
  to the autarch via the standard escalation path.

### Supersession

This ADR supersedes ADR-0019 (Account & Quota Management). That system is
removed in its entirety. ADR-0018 (Agent Config Directory Isolation) remains
in force — the config dir isolation mechanism is preserved; only the
credential rotation machinery layered on top of it is removed.

## Alternatives Considered

### Keep credential rotation, remove only budget enforcement

Partial simplification retains the `internal/account/` and `internal/quota/`
machinery. This still eliminates the budget gate (the most friction-causing
piece) while keeping autonomous rate-limit recovery. Rejected because the
quota/account machinery adds substantial code surface and operational
complexity (account registration, symlink swapping, sentinel pattern matching)
for a benefit that autarch presence makes largely redundant.

### Replace current machinery with a lighter rotation design

Keep the concept of multiple accounts but simplify the rotation to a
configuration file (`accounts:` list in `world.toml`) with a simpler
sentinel-triggered swap. Rejected because the fundamental problem is that
autonomous credential rotation is a category of complexity sol should not
own — not just that the current implementation is over-engineered.

### Defer — remove nothing now, revisit at scale

Keep everything, accept the code weight, revisit if a third runtime is added.
Rejected because the complexity cost is paid continuously in every dispatch,
every sentinel patrol, and every code review touching these paths. The
simplification is clearly the right long-term direction; deferring it
accumulates interest.
