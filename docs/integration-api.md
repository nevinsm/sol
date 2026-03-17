# Integration API — Design Sketch

Status: **future work** — not scheduled, not committed. Captured here so the
thinking is preserved for when sol's feature set stabilizes and integration
needs become concrete.

---

## Problem

Sol manages agents, writs, merges, and caravans. External systems — CI
pipelines, issue trackers, monitoring dashboards, chat bots — may want to
interact with sol: creating writs from external triggers, reacting to sol
events, or reading sol state for display.

Sol should not become a platform that integrates with everything. It should
expose clean primitives that let other systems integrate with *it*.

## Design Principles

- **Sol doesn't know about external systems.** No GitHub adapter, no Slack
  plugin, no Jira connector. Sol exposes events and accepts commands.
  External systems decide what to do with them.
- **CLI is the inbound API.** The CLI already handles all mutations.
  Stabilizing its output is cheaper than building a second interface.
- **Webhooks are the outbound API.** Push events to configured endpoints.
  External systems react. No polling, no coupling.
- **No HTTP server in sol.** If remote access or a web dashboard is needed,
  that's a separate project that wraps sol's CLI and subscribes to its
  webhooks. Sol stays simple.
- **No SDK.** SDKs couple external systems to sol's internals and create
  maintenance burden. The integration surface is structured CLI output
  and webhook payloads — both language-agnostic.

---

## Part 1: Stable CLI Output (`--json`)

### What

Every user-facing command that produces structured information gets a
`--json` flag that emits machine-parseable JSON to stdout. Error output
stays on stderr. Exit codes remain meaningful.

### Stability Contract

Once a command's JSON output shape is documented, it becomes part of sol's
public API. Fields may be added but never removed or renamed within a major
version. This is semantic versioning applied to CLI output.

### Priority Commands

These commands are the most likely integration points. Stabilize these first:

**State queries:**

| Command | Description | Key fields |
|---------|-------------|------------|
| `sol status --json` | Sphere overview | agents, processes, caravans, merge queue |
| `sol status <world> --json` | Per-world detail | outposts, services, writs, forge |
| `sol writ status <id> --json` | Single writ state | id, status, assignee, MR phase |
| `sol writ list --json` | Writ listing | array of writ summaries |
| `sol agent list --json` | Agent state | name, state, active_writ, session |
| `sol caravan status <id> --json` | Caravan detail | items, phases, merge progress |
| `sol cost --json` | Token usage | per-agent, per-model, totals |

**Mutations (return the created/modified object):**

| Command | Description | Returns |
|---------|-------------|---------|
| `sol writ create --json` | Create writ | `{"id": "sol-...", ...}` |
| `sol cast <writ-id> --world=<world> --json` | Dispatch work | cast result with agent, worktree |
| `sol caravan create --json` | Create caravan | `{"id": "car-...", ...}` |

### Output Conventions

```jsonc
// Success: structured data on stdout, exit 0
{"id": "sol-a1b2c3d4e5f6a7b8", "status": "open", "title": "..."}

// Error: message on stderr, non-zero exit
// stderr: "failed to find writ \"sol-bad\": not found"
```

- Timestamps: RFC 3339, UTC (consistent with sol's internal convention)
- IDs: full-length, not truncated
- Enums: lowercase strings matching internal representation
- Nulls: omit the field rather than emit `null` (Go `omitempty`)

### What `--json` Does NOT Change

- Exit code semantics remain identical
- Auth, permissions, and validation are unchanged
- Commands that are interactive (inbox TUI, init wizard) do not get `--json`
- `--json` suppresses all non-JSON stdout (progress bars, spinners, lipgloss)

---

## Part 2: Event Webhooks

### What

Sol emits HTTP POST requests to configured endpoints when lifecycle events
occur. Configuration lives in `sol.toml` (sphere-wide) or `world.toml`
(per-world). Delivery is best-effort with logged failures.

### Existing Infrastructure

Consul already routes escalations to webhook endpoints. This proposal
generalizes that infrastructure to cover all lifecycle events.

### Event Types

**Writ lifecycle:**

| Event | Fires when |
|-------|------------|
| `writ.created` | A writ is created |
| `writ.cast` | A writ is dispatched to an agent |
| `writ.resolved` | An agent calls `sol resolve` |
| `writ.merged` | Forge successfully merges the writ's MR |
| `writ.failed` | Forge marks the MR as failed |
| `writ.closed` | A writ is closed (any path) |

**Caravan lifecycle:**

| Event | Fires when |
|-------|------------|
| `caravan.commissioned` | Caravan moves from drydock to open |
| `caravan.phase_complete` | All items in a phase are merged |
| `caravan.closed` | All items merged, caravan closed |

**Agent lifecycle:**

| Event | Fires when |
|-------|------------|
| `agent.started` | Agent session starts |
| `agent.stopped` | Agent session stops (clean or crash) |
| `agent.idle` | Agent transitions to idle after completing work |

**System events:**

| Event | Fires when |
|-------|------------|
| `escalation.created` | An escalation is raised |
| `merge.landed` | Any MR merges to the target branch |
| `forge.stalled` | Forge has not merged in configured threshold |

### Payload Shape

```json
{
  "event": "writ.merged",
  "timestamp": "2026-03-10T14:30:00Z",
  "world": "sol-dev",
  "data": {
    "writ_id": "sol-a1b2c3d4e5f6a7b8",
    "title": "feat: add widget support",
    "merge_request_id": "mr-00000042",
    "branch": "outpost/Toast/sol-a1b2c3d4e5f6a7b8",
    "agent": "Toast"
  }
}
```

- `event`: dot-namespaced event type
- `timestamp`: when the event occurred (RFC 3339 UTC)
- `world`: which world the event belongs to (omitted for sphere-level events)
- `data`: event-specific payload (varies by event type, documented per-event)

### Configuration

```toml
# sol.toml — sphere-wide webhook configuration

[[webhooks]]
url = "http://localhost:8080/sol-events"
events = ["writ.merged", "writ.failed", "caravan.closed"]

[[webhooks]]
url = "http://localhost:9090/all-events"
events = ["*"]  # subscribe to everything
```

```toml
# world.toml — per-world overrides (additive to sphere config)

[[webhooks]]
url = "http://localhost:8080/sol-dev-events"
events = ["writ.*", "merge.landed"]
```

- Glob patterns for event filtering (`writ.*` matches all writ events)
- Per-world config is additive — sphere-level webhooks always fire
- No auth headers in v1 — the consumer is localhost or a trusted network.
  If remote endpoints are needed, the consumer should run a local relay
  that adds authentication.

### Delivery Semantics

- **Best-effort, at-most-once.** Fire and forget. If the endpoint is down,
  the event is logged and dropped. No retry queue, no persistence.
- **Non-blocking.** Webhook delivery must never slow down the operation
  that triggered it. Fire in a goroutine with a short timeout (5s).
- **Logged.** Every webhook attempt (success or failure) is logged to
  chronicle. Failed deliveries include the HTTP status or error.
- **Idempotent consumers encouraged.** Events may include a unique event
  ID so consumers can deduplicate if sol ever adds retry semantics.

### What Webhooks Are NOT

- Not a message queue. No guaranteed delivery, no ordering, no replay.
- Not a streaming API. Each event is an independent HTTP request.
- Not bidirectional. Sol pushes events. It does not accept commands via
  webhook. Commands come through the CLI.

---

## Part 3: What We Explicitly Won't Build

### HTTP API Server

If someone needs to expose sol over HTTP — for a web dashboard, remote
access, or multi-machine setups — that's a separate project. It would:

- Wrap sol CLI commands behind HTTP endpoints
- Subscribe to sol webhooks for real-time state
- Handle its own auth, TLS, rate limiting
- Live outside the sol repository

Sol stays a local tool. Network concerns belong at a different layer.

### Language SDKs

No Python SDK, no TypeScript SDK, no Go client library. The integration
surface is:

- Shell out to `sol ... --json` for commands
- Listen on an HTTP endpoint for webhooks

Both are language-agnostic. Any language that can run a subprocess or
serve HTTP can integrate with sol.

### Plugin System

No plugin loading, no extension points, no hook registries beyond what
exists for Claude Code hooks. Sol's behavior is defined by its source code
and configuration. External systems integrate at the boundary (CLI +
webhooks), not inside the process.

---

## Implementation Notes (for when this becomes work)

### Sequencing

1. **`--json` on state query commands** — highest value, lowest risk.
   Some commands may already have partial JSON support. Audit and
   standardize.
2. **`--json` on mutation commands** — return the created/modified object.
   Straightforward extension of step 1.
3. **Event webhook infrastructure** — generalize consul's webhook code
   into a shared package. Wire it into the event points listed above.
4. **Configuration and documentation** — TOML schema for webhook config,
   document payload shapes, add to cli.md.

### Architectural Fit

- Webhook dispatch should live in a shared `internal/webhook/` package,
  not duplicated across components.
- Events should be emitted at the store/dispatch layer, not in CLI
  command handlers. This ensures webhooks fire regardless of how the
  operation is triggered (CLI, sentinel auto-recast, consul patrol).
- Webhook config follows the existing layered config pattern
  (`sol.toml` → `world.toml`).

### Testing

- Unit tests for webhook dispatch (mock HTTP server)
- Integration test: create writ, verify webhook payload
- Failure test: webhook endpoint down, verify operation still succeeds
  and failure is logged

---

*Written 2026-03-10 during 0.1.0 release planning. Revisit when sol's
feature set stabilizes and concrete integration needs emerge.*
