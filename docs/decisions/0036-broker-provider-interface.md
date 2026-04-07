# ADR-0036: Broker Provider Interface

Status: Accepted

## Context

The broker's health probing and the quota system's rate limit detection are hardcoded to Claude/Anthropic:

- **Health probing**: `broker/health.go` makes an HTTP GET to `https://api.anthropic.com/v1/models` and interprets any 2xx/3xx/4xx as healthy, 5xx/network errors as unhealthy.
- **Rate limit detection**: `quota/quota.go` matches against six Claude-specific regex patterns (e.g., `You've hit your .*limit`, `OAuth token revoked`).
- **Credential expiry**: The broker checks token expiry assuming OAuth tokens expire and API keys don't.

These assumptions are correct for Claude but not generalizable. A future runtime (e.g., Codex) would probe health via a different endpoint, detect rate limits from different output patterns or exit codes, and have different credential expiry semantics.

This parallels the situation that led to `RuntimeAdapter` (ADR-0031): Claude-specific primitives woven directly into infrastructure code. The solution follows the same pattern.

## Decision

Define a `broker.Provider` interface with four methods, parallel to `adapter.RuntimeAdapter`:

```go
type Provider interface {
    Name() string
    ProbeHealth(ctx context.Context) error
    DetectRateLimit(output string) *RateLimitSignal
    CredentialExpires(credType string) bool
}
```

`RateLimitSignal` carries parsed rate limit information:

```go
type RateLimitSignal struct {
    Account  string
    ResetsAt time.Time
    ResetsIn time.Duration
}
```

A provider registry (`RegisterProvider`/`GetProvider`) follows the exact same pattern as `adapter.Register`/`adapter.Get`. Providers register themselves via `init()` alongside their `RuntimeAdapter` registration.

The Claude provider (`internal/adapter/claude/provider.go`) extracts the existing health probe HTTP logic and rate limit regex patterns. The broker resolves its provider from the registry at construction time. The quota system delegates `DetectRateLimit` calls to the registered provider.

### Why Interface, Not Struct

Future runtimes may:
- Probe health via gRPC or WebSocket instead of HTTP GET
- Detect rate limits from exit codes, structured logs, or different error patterns
- Have novel credential expiry semantics (e.g., short-lived tokens that always expire)

An interface makes these variations compile-time safe: adding a new runtime requires implementing all four methods.

## Consequences

**Positive**:
- Broker and quota delegate to the provider instead of hardcoding Claude behavior
- Adding a new runtime's provider is a single file with four method implementations
- Existing behavior is unchanged — the Claude provider contains the exact same logic that was previously inline
- Pattern is consistent with `RuntimeAdapter` — both use init-registered interfaces

**Negative**:
- `quota.DetectRateLimit` now depends on the broker package (for `GetProvider`), adding a cross-package dependency
- The `parseResetTime` helper is duplicated in the Claude provider (moved from quota) rather than shared, since it's Claude-specific parsing logic
