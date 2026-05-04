# Integration API

Status: **Part 1 first pass landed (experimental).** Schemas are documented in
[docs/api/](api/README.md) and contract-tested via `internal/jsoncontract/`.
Schemas may change in any release until sol v1.0 — see
[docs/api/README.md](api/README.md) for the experimental disclaimer. Part 2
(event webhooks) is still future work.

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

**Pre-1.0 (current):** Schemas are documented and contract-tested so that
drift is detected, but they carry no stability guarantee yet. Field names,
shapes, and enum values may change in any release. See
[docs/api/README.md](api/README.md) for the full experimental disclaimer and
guidance on pinning to a specific sol version.

**At v1.0:** These schemas become part of sol's public API per semver.
Fields may be added but never removed or renamed within a major version.
This is semantic versioning applied to CLI output.

### Priority Commands

See [docs/api/README.md](api/README.md) for the canonical list of commands
with documented schemas.

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
- `--json` suppresses all non-JSON stdout (progress bars, spinners, lipgloss)

### Which Commands Expose `--json`

**Rule:** Commands that produce structured output for an external consumer
expose `--json`. Commands whose primary output is interactive (TUI) or
human-only (wizards, prompts, dashboards) do not.

Examples of the rule in practice:

- `sol writ get`, `sol writ list`, `sol mr list`, `sol caravan show`,
  `sol agent list` — structured output, all expose `--json`.
- `sol inbox`, `sol dash`, `sol init`, `sol envoy create` (interactive
  flow) — interactive UIs, do not expose `--json`. Use the underlying
  plumbing commands (`sol mail list --json`, `sol escalation list --json`)
  to get the same data programmatically.

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

## Implementation Notes

### Sequencing

1. ~~**`--json` on state query commands**~~ — **DONE.** Canonical types
   live in `internal/cliapi/`, JSON schemas are generated in `docs/api/`,
   and contract tests in `internal/jsoncontract/` detect drift.
2. ~~**`--json` on mutation commands**~~ — **DONE.** Mutation commands
   (cast, writ create, caravan create, etc.) return the created/modified
   object through the same `cliapi` types.
3. **Event webhook infrastructure** — next milestone. Generalize consul's
   webhook code into a shared package. Wire it into the event points
   listed in Part 2 above.
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

*Originally written 2026-03-10 during 0.1.0 release planning. Updated
2026-04-11: Part 1 first pass landed — `--json` schemas documented in
docs/api/ and contract-tested. Part 2 (event webhooks) remains future work.*
