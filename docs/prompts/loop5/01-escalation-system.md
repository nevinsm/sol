# Prompt 01: Loop 5 — Escalation System

You are extending the `gt` orchestration system with a severity-based
escalation system. Escalations allow agents, witnesses, and operators to
flag problems that need human attention. A pluggable notifier interface
routes escalations to the appropriate channels based on severity.

**Working directory:** `~/gt-src/`
**Prerequisite:** Loop 4 is complete.

Read all existing code first. Understand the store package
(`internal/store/` — especially `schema.go` for the existing escalations
table, `messages.go` for mail, and `protocol.go`), the events package
(`internal/events/`), and the config package (`internal/config/`).

Read `docs/target-architecture.md` Section 3.7 (Deacon) for how
escalations fit into the broader Loop 5 picture.

---

## Task 1: Escalation CRUD

The `escalations` table already exists in town schema V2. Add Go
functions to operate on it.

### Types

Create `internal/store/escalations.go`:

```go
package store

import "time"

// Escalation represents a flagged problem requiring attention.
type Escalation struct {
    ID           string
    Severity     string    // "low", "medium", "high", "critical"
    Source       string    // agent ID or component that created it
    Description  string
    Status       string    // "open", "acknowledged", "resolved"
    Acknowledged bool
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

### Escalation ID Format

Escalation IDs: `"esc-"` + 8 hex chars from `crypto/rand` (same pattern
as other IDs).

### Functions

```go
// CreateEscalation creates an escalation record.
// Severity must be one of: "low", "medium", "high", "critical".
// Returns the escalation ID.
func (s *Store) CreateEscalation(severity, source, description string) (string, error)

// GetEscalation returns an escalation by ID.
func (s *Store) GetEscalation(id string) (*Escalation, error)

// ListEscalations returns escalations filtered by status.
// If status is empty, returns all escalations.
// Ordered by created_at DESC (newest first).
func (s *Store) ListEscalations(status string) ([]Escalation, error)

// AckEscalation marks an escalation as acknowledged.
// Sets acknowledged=true, status="acknowledged", updated_at=now.
func (s *Store) AckEscalation(id string) error

// ResolveEscalation marks an escalation as resolved.
// Sets status="resolved", updated_at=now.
func (s *Store) ResolveEscalation(id string) error

// CountOpen returns the number of open (unresolved) escalations.
func (s *Store) CountOpen() (int, error)
```

### Validation

`CreateEscalation` must validate severity is one of the four allowed
values. Return a descriptive error for invalid severity.

---

## Task 2: Notifier Package

Create `internal/escalation/` package with a pluggable notifier
interface and three implementations.

### Notifier Interface

```go
// internal/escalation/notifier.go
package escalation

import (
    "context"

    "github.com/nevinsm/gt/internal/store"
)

// Notifier delivers escalation notifications to a channel.
type Notifier interface {
    // Notify delivers an escalation notification.
    // Implementations must be safe for concurrent use.
    Notify(ctx context.Context, esc store.Escalation) error

    // Name returns a human-readable name for this notifier (e.g., "log", "mail", "webhook").
    Name() string
}
```

### LogNotifier

```go
// internal/escalation/log.go

// LogNotifier writes escalation events to the event feed.
type LogNotifier struct {
    logger *events.Logger
}

func NewLogNotifier(logger *events.Logger) *LogNotifier

// Notify emits an escalation_created event.
func (n *LogNotifier) Notify(ctx context.Context, esc store.Escalation) error
func (n *LogNotifier) Name() string // returns "log"
```

The log notifier emits an event with type `EventEscalationCreated` and
payload containing the escalation ID, severity, source, and description.

If the logger is nil, `Notify` is a no-op (returns nil).

### MailNotifier

```go
// internal/escalation/mail.go

// MailNotifier sends an escalation as a protocol message via the town store.
type MailNotifier struct {
    store *store.Store
}

func NewMailNotifier(townStore *store.Store) *MailNotifier

// Notify sends a mail message to "operator" with the escalation details.
// Subject: "[ESCALATION-{severity}] {description truncated to 80 chars}"
// Body: full escalation details including ID, source, timestamp.
// Priority: 1 for critical/high, 2 for medium, 3 for low.
func (n *MailNotifier) Notify(ctx context.Context, esc store.Escalation) error
func (n *MailNotifier) Name() string // returns "mail"
```

The mail message uses type `"notification"` and is sent to recipient
`"operator"`.

### WebhookNotifier

```go
// internal/escalation/webhook.go

// WebhookNotifier POSTs escalation data to an HTTP endpoint.
type WebhookNotifier struct {
    URL     string
    Client  *http.Client
    Timeout time.Duration // default: 10 seconds
}

func NewWebhookNotifier(url string) *WebhookNotifier

// Notify sends a POST request with JSON body:
// {
//   "id": "esc-a1b2c3d4",
//   "severity": "high",
//   "source": "myrig/witness",
//   "description": "Agent Toast stalled for 30 minutes",
//   "status": "open",
//   "created_at": "2026-02-27T10:30:00Z"
// }
//
// Content-Type: application/json
// User-Agent: gt-escalation/1.0
//
// Returns error if response status is not 2xx.
// Timeout defaults to 10 seconds.
func (n *WebhookNotifier) Notify(ctx context.Context, esc store.Escalation) error
func (n *WebhookNotifier) Name() string // returns "webhook"
```

### Router

```go
// internal/escalation/router.go

// Router routes escalations to notifiers based on severity.
type Router struct {
    rules map[string][]Notifier
}

// NewRouter creates an empty router.
func NewRouter() *Router

// AddRule adds notifiers for a severity level. Can be called multiple
// times for the same severity — notifiers accumulate.
func (r *Router) AddRule(severity string, notifiers ...Notifier)

// Route sends an escalation to all notifiers registered for its severity.
// Returns the first error encountered, but continues notifying remaining
// notifiers (best-effort delivery).
// Returns nil if no rules match the severity.
func (r *Router) Route(ctx context.Context, esc store.Escalation) error

// DefaultRouter creates a router with standard severity routing:
//   low:      LogNotifier
//   medium:   LogNotifier + MailNotifier
//   high:     LogNotifier + MailNotifier + WebhookNotifier (if webhookURL != "")
//   critical: LogNotifier + MailNotifier + WebhookNotifier (if webhookURL != "")
//
// If webhookURL is empty, high/critical omit the webhook notifier.
// If logger is nil, log notifier is a no-op.
func DefaultRouter(logger *events.Logger, townStore *store.Store, webhookURL string) *Router
```

---

## Task 3: CLI Commands

### gt escalate

Create the escalation creation command in `cmd/escalate.go`:

```
gt escalate [--severity=<level>] [--source=<source>] "<description>"
```

- `--severity` (optional, default `"medium"`): one of low, medium, high, critical
- `--source` (optional, default `"operator"`): who is escalating
- `<description>` (required): first positional argument

**Behavior:**
1. Open town store
2. Create escalation via `townStore.CreateEscalation()`
3. Build router: `escalation.DefaultRouter(logger, townStore, webhookURL)`
   - `webhookURL` from `GT_ESCALATION_WEBHOOK` environment variable (empty = no webhook)
4. Route the escalation
5. Print: `Escalation created: <id> [<severity>]`
6. Exit 0

**Errors:** invalid severity, missing description → print error, exit 1.

### gt escalation list

Add `cmd/escalation.go` with the `gt escalation` command group:

```
gt escalation list [--status=<status>] [--json]
```

- `--status` (optional): filter by status (open, acknowledged, resolved)
- `--json`: output as JSON array

**Human output:**
```
Open escalations:
  esc-a1b2c3d4  [critical]  myrig/witness  Agent Toast stalled for 30m  (2m ago)
  esc-e5f6a7b8  [medium]    operator       Tests flaky on staging       (1h ago)

2 escalation(s)
```

**JSON output:**
```json
[{"id":"esc-a1b2c3d4","severity":"critical","source":"myrig/witness","description":"Agent Toast stalled for 30m","status":"open","created_at":"..."}]
```

### gt escalation ack

```
gt escalation ack <id>
```

**Behavior:**
1. Call `townStore.AckEscalation(id)`
2. Print: `Acknowledged: <id>`

### gt escalation resolve

```
gt escalation resolve <id>
```

**Behavior:**
1. Call `townStore.ResolveEscalation(id)`
2. Print: `Resolved: <id>`

---

## Task 4: Event Types

Add escalation event type constants to `internal/events/events.go`:

```go
const (
    EventEscalationCreated  = "escalation_created"
    EventEscalationAcked    = "escalation_acked"
    EventEscalationResolved = "escalation_resolved"
)
```

Add formatter cases in `cmd/feed.go`'s `formatEventDescription`:

```go
case events.EventEscalationCreated:
    return fmt.Sprintf("[%s] Escalation: %s (from %s)", get("severity"), get("description"), get("source"))
case events.EventEscalationAcked:
    return fmt.Sprintf("Escalation acknowledged: %s", get("id"))
case events.EventEscalationResolved:
    return fmt.Sprintf("Escalation resolved: %s", get("id"))
```

Emit `EventEscalationAcked` from the `ack` CLI command and
`EventEscalationResolved` from the `resolve` CLI command (when an event
logger is available). The `EventEscalationCreated` event is emitted by
the `LogNotifier`.

---

## Task 5: Tests

### Escalation Store Tests

Create `internal/store/escalations_test.go`:

```go
func TestCreateEscalation(t *testing.T)
    // Create → returns valid ID with "esc-" prefix
    // Verify with GetEscalation: all fields match

func TestCreateEscalationInvalidSeverity(t *testing.T)
    // severity="invalid" → error

func TestListEscalations(t *testing.T)
    // Create 3 escalations → list all → 3
    // List by status="open" → filters correctly
    // Newest first ordering

func TestAckEscalation(t *testing.T)
    // Create → ack → get: acknowledged=true, status="acknowledged"

func TestResolveEscalation(t *testing.T)
    // Create → resolve → get: status="resolved"

func TestCountOpen(t *testing.T)
    // Create 3, resolve 1 → CountOpen returns 2
```

### Notifier Tests

Create `internal/escalation/notifier_test.go`:

```go
func TestLogNotifier(t *testing.T)
    // Create logger with temp file
    // Notify → event written to feed
    // Nil logger → no error

func TestMailNotifier(t *testing.T)
    // Create town store
    // Notify → message sent to "operator"
    // Verify subject includes severity
    // Verify priority mapping (critical→1, low→3)

func TestWebhookNotifier(t *testing.T)
    // Create httptest.Server that records requests
    // Notify → server receives POST with correct JSON body
    // Verify Content-Type and User-Agent headers
    // Server returns 500 → Notify returns error

func TestWebhookNotifierTimeout(t *testing.T)
    // Create slow httptest.Server
    // Short timeout → Notify returns error

func TestRouterDefaultRouting(t *testing.T)
    // Create DefaultRouter with all notifiers
    // Route low → only log fires
    // Route medium → log + mail fire
    // Route high → log + mail + webhook fire

func TestRouterNoWebhook(t *testing.T)
    // DefaultRouter with empty webhookURL
    // Route high → log + mail fire (no webhook)

func TestRouterBestEffort(t *testing.T)
    // Router with failing notifier + working notifier
    // Route → working notifier still fires despite failure
```

### CLI Smoke Tests

Add to a new `test/integration/cli_loop5_test.go`:

```go
func TestCLIEscalateHelp(t *testing.T)
func TestCLIEscalationListHelp(t *testing.T)
func TestCLIEscalationAckHelp(t *testing.T)
func TestCLIEscalationResolveHelp(t *testing.T)
```

---

## Task 6: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export GT_HOME=/tmp/gt-test
   mkdir -p /tmp/gt-test/.store

   # Create an escalation
   bin/gt escalate --severity=high --source=operator "Tests failing on myrig"

   # List escalations
   bin/gt escalation list
   bin/gt escalation list --json

   # Acknowledge
   bin/gt escalation ack esc-<id>
   bin/gt escalation list

   # Resolve
   bin/gt escalation resolve esc-<id>
   bin/gt escalation list --status=resolved

   # Test webhook (optional — requires running server)
   export GT_ESCALATION_WEBHOOK=http://localhost:8080/webhook
   bin/gt escalate --severity=critical "Production down"
   ```
4. Clean up `/tmp/gt-test` after verification.

---

## Guidelines

- The escalations table already exists in the town schema (V2). Do NOT
  create a new migration — just add Go functions to operate on the
  existing table.
- The notifier interface is intentionally simple — `Notify()` + `Name()`.
  Implementations are stateless (except WebhookNotifier's HTTP client).
- Router uses best-effort delivery: if one notifier fails, the others
  still fire. The first error is returned but does not short-circuit.
- WebhookNotifier respects context cancellation and has a configurable
  timeout (default 10s). Use `http.NewRequestWithContext`.
- The `gt escalate` command is designed to be called by agents from
  within their sessions (e.g., when stuck). The `--source` flag defaults
  to `"operator"` for manual use, but agents will pass their ID.
- The `gt escalation` command group is for operators managing
  escalations. Agents should not need to list/ack/resolve escalations.
- Event emission follows the nil-logger-safe pattern used throughout
  the codebase.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(escalation): add escalation system with pluggable notifiers`
