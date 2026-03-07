# ADR-0016: Ledger as Sphere-Scoped OTel Receiver for Agent Token Tracking

Status: accepted
Date: 2026-03-05

## Context

Sol orchestrates multiple concurrent AI agents, each consuming API tokens
across sessions. Today there is no visibility into token spend — operators
cannot answer "how many tokens did this writ cost?" or "which agent
consumes the most cache-read tokens?" Without this data, cost attribution,
budget enforcement, and efficiency analysis are impossible.

The data naturally lives in the world database: token usage is tied to
agent sessions that work on world-scoped writs. Each session has a
lifecycle (started, ended) and produces token consumption records broken
down by model and token type (input, output, cache read, cache creation).

OpenTelemetry (OTel) defines the standard shape for this kind of
telemetry data. Modeling our schema after OTel's resource → span →
metrics hierarchy keeps the door open for future export to external
observability backends without requiring one today.

## Decision

Add two tables to the world database:

**`agent_history`** — records agent session lifecycle events. Each row
represents a discrete agent action (cast, resolve, respawn, etc.) tied
to a writ. This is the "span" in OTel terms.

| Column       | Type | Notes                          |
|--------------|------|--------------------------------|
| id           | TEXT | PK, `ah-` + 16 hex chars       |
| agent_name   | TEXT | NOT NULL                       |
| writ_id | TEXT | FK to writs(id), nullable |
| action       | TEXT | NOT NULL (cast, resolve, etc.) |
| started_at   | TEXT | NOT NULL, RFC3339              |
| ended_at     | TEXT | nullable, RFC3339              |
| summary      | TEXT | nullable, free-form            |

**`token_usage`** — records per-model token consumption within a history
entry. Multiple rows per history entry (one per model used). This is the
"metric" attached to a span.

| Column                | Type    | Notes                          |
|-----------------------|---------|--------------------------------|
| id                    | TEXT    | PK, `tu-` + 16 hex chars       |
| history_id            | TEXT    | FK to agent_history(id)        |
| model                 | TEXT    | NOT NULL                       |
| input_tokens          | INTEGER | NOT NULL, default 0            |
| output_tokens         | INTEGER | NOT NULL, default 0            |
| cache_read_tokens     | INTEGER | NOT NULL, default 0            |
| cache_creation_tokens | INTEGER | NOT NULL, default 0            |

Store methods:

- `WriteHistory` — insert an agent_history record, return generated ID.
- `GetHistory` — fetch a single history entry by ID.
- `ListHistory` — list history entries filtered by agent name.
- `WriteTokenUsage` — insert a token_usage record linked to a history entry.
- `AggregateTokens` — sum token usage across all history entries for
  an agent, grouped by model. Returns per-model totals.

Schema version: world DB v6.

## Consequences

- Operators can query per-agent and per-writ token spend through
  the store directly. Future CLI commands (`sol status`, `sol ledger`)
  can surface this data.
- The schema mirrors OTel's span/metric model, so exporting to an
  external OTel collector is a natural extension without schema changes.
- Token data accumulates in the world database. For long-lived worlds,
  periodic pruning or archival may be needed — deferred until usage
  patterns are clear.
- The agent_history table doubles as an audit log for agent lifecycle
  events, useful for debugging session failures and understanding
  agent activity patterns.
