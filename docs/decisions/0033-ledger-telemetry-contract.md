# ADR-0033: Ledger Telemetry Contract

Status: Accepted

## Context

The ledger receives OTLP logs from agent sessions. Originally it was hardcoded for Claude Code's event names and attribute names. With multi-runtime support, we need a contract between adapters and the ledger so that the ledger can route telemetry to the correct extraction logic without embedding runtime-specific knowledge.

## Decision

- Adapters set `OTEL_RESOURCE_ATTRIBUTES` with required attributes: `agent.name`, `world`, `service.name`. Optional: `writ_id` (set when the agent is tethered to a writ).
- `service.name` is the routing key — the ledger uses it to select the correct `ExtractTelemetry` implementation for that runtime.
- Adapters implement `ExtractTelemetry` to normalize their runtime's OTLP attributes into a canonical `TelemetryRecord` struct. All runtime-specific knowledge (event names, attribute keys, cost extraction) lives in the adapter.
- The ledger is runtime-agnostic — it receives OTLP data, looks up the extractor by `service.name`, and delegates all parsing to it.
- Cost is runtime-provided (via a `cost_usd` attribute in the OTLP data), not computed by sol.

### Attribute ordering convention

Adapters emit `OTEL_RESOURCE_ATTRIBUTES` in this order: `agent.name`, `world`, `writ_id` (if present), `service.name`. This is a convention for readability; the ledger does not depend on ordering.

## Consequences

**Positive**:
- Adding a new runtime requires implementing `RuntimeAdapter.ExtractTelemetry` and registering the extractor in the ledger. No ledger code changes needed.
- The contract is explicit — adapters declare their `service.name` in `OTEL_RESOURCE_ATTRIBUTES` rather than relying on the runtime's internal OTel configuration to set it.
- The ledger can validate incoming telemetry against the contract (reject data with missing required attributes).

**Negative / Trade-offs**:
- Each adapter must know its own `service.name` and keep it consistent with the ledger's extractor registry. A mismatch silently drops telemetry.
