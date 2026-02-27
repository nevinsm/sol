# Prompt 01: Loop 3 — Mail System (Sphere Schema V2 + Messages + CLI)

You are extending the `sol` orchestration system with the mail system — the
first inter-agent communication infrastructure. This prompt adds the
`messages` table to the sphere database, message CRUD operations, protocol
message helpers, and `sol mail` CLI commands.

**Working directory:** `~/sol-src/`
**Prerequisite:** Loop 2 is complete (prompts 01–04).

Read all existing code first. Understand the store package
(`internal/store/` — schema versioning, sphere schema V1, agents), the
dispatch package (`internal/dispatch/`), and the config package
(`internal/config/config.go`). Study the Loop 2 prompt pattern in
`docs/prompts/loop2/01-merge-request-store.md` for reference.

Read `docs/target-architecture.md` Section 3.3 (Mail System) and the
`messages` table schema in Section 3.1 for design context.

---

## Task 1: Sphere Schema V2 — Messages Table

Add a V2 migration to `internal/store/schema.go` that creates the
`messages` and `escalations` tables. The V1 schema (agents) is unchanged.

### Schema

```sql
CREATE TABLE IF NOT EXISTS messages (
    id          TEXT PRIMARY KEY,
    sender      TEXT NOT NULL,
    recipient   TEXT NOT NULL,
    subject     TEXT NOT NULL,
    body        TEXT,
    priority    INTEGER NOT NULL DEFAULT 2,
    type        TEXT NOT NULL DEFAULT 'notification',
    thread_id   TEXT,
    delivery    TEXT NOT NULL DEFAULT 'pending',
    read        INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    acked_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_messages_recipient
    ON messages(recipient, delivery);
CREATE INDEX IF NOT EXISTS idx_messages_thread
    ON messages(thread_id);

CREATE TABLE IF NOT EXISTS escalations (
    id           TEXT PRIMARY KEY,
    severity     TEXT NOT NULL,
    source       TEXT NOT NULL,
    description  TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'open',
    acknowledged INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
```

**Fields (messages):**
- `id`: `"msg-"` + 8 random hex chars (e.g., `msg-a1b2c3d4`)
- `sender`: agent ID or `"operator"` (e.g., `myworld/Toast`, `operator`)
- `recipient`: agent ID, `"operator"`, or `"{world}/sentinel"` for routing
- `subject`: short description or protocol message type prefix
- `body`: message content — freeform text or JSON for protocol messages
- `priority`: 1 (urgent) to 3 (low), default 2 (normal)
- `type`: `notification` (default) or `protocol` (machine-readable)
- `thread_id`: optional grouping ID for conversation threading
- `delivery`: `pending` (undelivered) or `acked` (acknowledged by recipient)
- `read`: 0 (unread) or 1 (read by recipient)
- `created_at`: RFC3339 UTC
- `acked_at`: RFC3339 UTC when acknowledged (null until then)

**Fields (escalations):**
- `id`: `"esc-"` + 8 random hex chars
- `severity`: `low`, `medium`, `high`, `critical`
- `source`: agent ID or component that raised the escalation
- `description`: human-readable description of the issue
- `status`: `open`, `acknowledged`, `resolved`
- `acknowledged`: 0 or 1
- `created_at`, `updated_at`: RFC3339 UTC

### Migration Pattern

Follow the existing sphere V1 migration pattern. Add a `sphereSchemaV2`
constant and extend `migrateTown()`:

```go
const sphereSchemaV2 = `
CREATE TABLE IF NOT EXISTS messages (...);
CREATE INDEX IF NOT EXISTS idx_messages_recipient ON messages(recipient, delivery);
CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(thread_id);

CREATE TABLE IF NOT EXISTS escalations (...);
`

func (s *Store) migrateTown() error {
    v, err := s.schemaVersion()
    if err != nil {
        return fmt.Errorf("failed to check schema version: %w", err)
    }
    if v < 1 {
        if _, err := s.db.Exec(sphereSchemaV1); err != nil {
            return fmt.Errorf("failed to create sphere schema v1: %w", err)
        }
    }
    if v < 2 {
        if _, err := s.db.Exec(sphereSchemaV2); err != nil {
            return fmt.Errorf("failed to create sphere schema v2: %w", err)
        }
    }
    if v < 2 {
        if _, err := s.db.Exec("UPDATE schema_version SET version = 2"); err != nil {
            return fmt.Errorf("failed to set schema version: %w", err)
        }
    }
    return nil
}
```

Handle V1→V2 upgrade (existing agents table untouched) and fresh
databases (both V1 and V2 applied, version set to 2).

---

## Task 2: Message Struct and Store CRUD

Create `internal/store/messages.go` with the Message type and all CRUD
operations.

### Data Structure

```go
// internal/store/messages.go
package store

import "time"

// Message represents a message in the sphere database.
type Message struct {
    ID        string
    Sender    string
    Recipient string
    Subject   string
    Body      string
    Priority  int
    Type      string     // "notification" or "protocol"
    ThreadID  string     // empty if not threaded
    Delivery  string     // "pending" or "acked"
    Read      bool
    CreatedAt time.Time
    AckedAt   *time.Time
}
```

### CRUD Operations

```go
// SendMessage creates a new message in the store.
// Returns the generated message ID (msg-XXXXXXXX).
func (s *Store) SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)

// Inbox returns pending messages for a recipient, ordered by priority ASC
// then created_at ASC (highest priority first, oldest first).
// If recipient is empty, returns all pending messages.
func (s *Store) Inbox(recipient string) ([]Message, error)

// ReadMessage returns a message by ID and marks it as read (read=1).
func (s *Store) ReadMessage(id string) (*Message, error)

// AckMessage acknowledges a message — sets delivery='acked' and acked_at=now.
func (s *Store) AckMessage(id string) error

// CountUnread returns the number of pending, unread messages for a recipient.
func (s *Store) CountUnread(recipient string) (int, error)

// ListMessages returns messages filtered by optional criteria.
// Supports filtering by recipient, type, delivery status, and thread_id.
func (s *Store) ListMessages(filters MessageFilters) ([]Message, error)
```

### MessageFilters

```go
// MessageFilters controls which messages are returned by ListMessages.
type MessageFilters struct {
    Recipient string // filter by recipient (empty = all)
    Type      string // filter by type: "notification", "protocol" (empty = all)
    Delivery  string // filter by delivery: "pending", "acked" (empty = all)
    ThreadID  string // filter by thread (empty = all)
}
```

### Implementation Notes

**ID generation:** Same pattern as work items and merge requests:
```go
func generateMessageID() string {
    b := make([]byte, 4)
    rand.Read(b)
    return "msg-" + hex.EncodeToString(b)
}
```

**SendMessage:** Insert with `delivery='pending'`, `read=0`,
`created_at=now`, `acked_at=NULL`. Thread ID can be empty string (stored
as empty, not NULL — simpler queries).

**Inbox:** Query pending messages for recipient:
```sql
SELECT ... FROM messages
WHERE recipient = ? AND delivery = 'pending'
ORDER BY priority ASC, created_at ASC
```

**ReadMessage:** Fetch the message AND update `read=1` in one operation.
Use a transaction or UPDATE then SELECT. Return not-found error if ID
doesn't exist.

**AckMessage:** Set `delivery='acked'` and `acked_at=now`. Must succeed
even if message is already acked (idempotent).

**Error messages:**
- Not found: `"message %q not found"`
- Send failure: `"failed to send message: %w"`
- Read failure: `"failed to read message %q: %w"`

---

## Task 3: Protocol Message Helpers

Create `internal/store/protocol.go` with helpers for typed protocol
messages. Protocol messages use `type='protocol'` and encode structured
data in the body as JSON.

### Protocol Message Types

```go
// Protocol message subject prefixes
const (
    ProtoPolecatDone       = "OUTPOST_DONE"
    ProtoMergeReady        = "MERGE_READY"
    ProtoMerged            = "MERGED"
    ProtoMergeFailed       = "MERGE_FAILED"
    ProtoReworkRequest     = "REWORK_REQUEST"
    ProtoRecoveryNeeded    = "RECOVERY_NEEDED"
)
```

### Helper Functions

```go
// SendProtocolMessage sends a typed protocol message with a JSON body.
// The subject is the protocol type (e.g., "OUTPOST_DONE").
// The body is JSON-encoded from the payload.
func (s *Store) SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)

// PendingProtocol returns pending protocol messages for a recipient,
// filtered by protocol type. If protoType is empty, returns all protocol messages.
func (s *Store) PendingProtocol(recipient, protoType string) ([]Message, error)
```

### Protocol Payloads

Define payload structs for the protocol messages used in Loop 3:

```go
// PolecatDonePayload is sent when a outpost completes its work.
type PolecatDonePayload struct {
    WorkItemID string `json:"work_item_id"`
    AgentID    string `json:"agent_id"`
    Branch     string `json:"branch"`
    World        string `json:"world"`
}

// MergeReadyPayload is sent when a sentinel verifies outpost work.
type MergeReadyPayload struct {
    MergeRequestID string `json:"merge_request_id"`
    WorkItemID     string `json:"work_item_id"`
    Branch         string `json:"branch"`
}

// MergedPayload is sent when the forge successfully merges work.
type MergedPayload struct {
    MergeRequestID string `json:"merge_request_id"`
    WorkItemID     string `json:"work_item_id"`
}

// MergeFailedPayload is sent when a merge fails (conflict or gate failure).
type MergeFailedPayload struct {
    MergeRequestID string `json:"merge_request_id"`
    WorkItemID     string `json:"work_item_id"`
    Reason         string `json:"reason"`
}

// RecoveryNeededPayload is sent when a sentinel detects a outpost issue.
type RecoveryNeededPayload struct {
    AgentID    string `json:"agent_id"`
    WorkItemID string `json:"work_item_id"`
    Reason     string `json:"reason"`
    Attempts   int    `json:"attempts"`
}
```

**Implementation:** `SendProtocolMessage` marshals the payload to JSON,
then calls `SendMessage` with `type="protocol"`, `subject=protoType`,
`body=jsonString`, and `priority=1` (protocol messages are always
urgent).

---

## Task 4: CLI Commands

Create `cmd/mail.go` with the `sol mail` command group.

### Commands

**`sol mail send`** — Send a message:
```
sol mail send --to=<recipient> --subject=<text> [--body=<text>] [--priority=N]
```
- `--to` (required): recipient agent ID or "operator"
- `--subject` (required): message subject
- `--body`: message body (optional, can be empty)
- `--priority`: 1-3, default 2
- Output: `Sent: msg-XXXXXXXX → <recipient>`

**`sol mail inbox`** — List pending messages:
```
sol mail inbox [--identity=<addr>] [--json]
```
- `--identity`: recipient to check (default: "operator")
- `--json`: output as JSON array
- Human output: tabwriter table with ID, FROM, PRIORITY, SUBJECT, AGE
- If empty: `No pending messages.`

**`sol mail read`** — Read a message (marks as read):
```
sol mail read <message-id>
```
- Output: full message content (From, To, Subject, Date, Body)
- Marks the message as read

**`sol mail ack`** — Acknowledge a message:
```
sol mail ack <message-id>
```
- Output: `Acknowledged: msg-XXXXXXXX`
- Marks delivery as acked

**`sol mail check`** — Count unread messages:
```
sol mail check [--identity=<addr>]
```
- `--identity`: recipient to check (default: "operator")
- Output: `3 unread messages` or `No unread messages.`
- Exit code 0 if messages, 1 if none (useful for scripting)

### Registration

Register the `mail` command group under the root command in
`cmd/root.go`, following the same pattern as other command groups
(forge, prefect, etc.).

---

## Task 5: Tests

### Schema Migration Tests

Add to `internal/store/store_test.go` (or a new file):

```go
func TestMigrateTownV2(t *testing.T)
    // Open a fresh sphere store
    // Verify messages table exists
    // Verify escalations table exists
    // Verify schema_version is 2

func TestMigrateTownV1ToV2(t *testing.T)
    // Open a sphere store (creates V1)
    // Close and reopen (should apply V2 migration)
    // Verify messages table exists
    // Verify existing agents are untouched
    // Verify schema_version is 2
```

### Message CRUD Tests

Create `internal/store/messages_test.go`:

```go
func TestSendMessage(t *testing.T)
    // Send a message
    // Verify: ID starts with "msg-", delivery is "pending", read is false
    // Read it back and verify all fields

func TestInbox(t *testing.T)
    // Send 3 messages to "operator" with different priorities
    // Inbox("operator") -> all 3, ordered by priority then age
    // Send a message to "other" -> not in operator's inbox
    // Ack one message -> no longer in inbox

func TestReadMessage(t *testing.T)
    // Send a message
    // ReadMessage -> returns full message, marks as read
    // ReadMessage again -> still returns (idempotent read)

func TestAckMessage(t *testing.T)
    // Send a message
    // AckMessage -> delivery='acked', acked_at set
    // AckMessage again -> no error (idempotent)
    // Message no longer appears in Inbox

func TestCountUnread(t *testing.T)
    // No messages -> 0
    // Send 3 messages -> 3
    // Read one -> still 3 (read doesn't affect count, only ack does)
    // Ack one -> 2

func TestListMessages(t *testing.T)
    // Send messages of different types and to different recipients
    // Filter by recipient -> only matching
    // Filter by type -> only matching
    // Filter by delivery -> only matching
    // Filter by thread -> only matching
    // No filters -> all messages

func TestSendProtocolMessage(t *testing.T)
    // Send a OUTPOST_DONE protocol message
    // Verify: type='protocol', subject='OUTPOST_DONE', body is valid JSON
    // Parse body back into PolecatDonePayload, verify fields
    // PendingProtocol(recipient, "OUTPOST_DONE") -> returns message
    // PendingProtocol(recipient, "MERGE_READY") -> empty (wrong type)

func TestMessageNotFound(t *testing.T)
    // ReadMessage with bogus ID -> error containing "not found"
    // AckMessage with bogus ID -> error containing "not found"
```

---

## Task 6: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   bin/sol mail send --to=operator --subject="Test message" --body="Hello world"
   bin/sol mail inbox
   bin/sol mail check
   bin/sol mail read <msg-id>
   bin/sol mail ack <msg-id>
   bin/sol mail inbox   # should be empty now
   # Verify in SQLite:
   sqlite3 /tmp/sol-test/.store/sphere.db "SELECT id, sender, recipient, subject FROM messages"
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- The `messages` table is in the **sphere** database (not world). Messages
  are cross-world by nature — a sentinel in world A could theoretically
  message an agent in world B.
- Message IDs use the `msg-` prefix to distinguish from work items
  (`sol-`), merge requests (`mr-`), and escalations (`esc-`).
- Protocol messages use `priority=1` (urgent) so they sort first in inbox.
- The `delivery` field tracks notification delivery, not whether the
  recipient has acted on the message. `acked` means "I've seen this and
  processed it."
- The `read` field is informational only — for human-readable inbox
  displays. Ack is the meaningful state transition.
- The escalations table is created now but CRUD operations are deferred
  to Loop 5 (Consul). We create the table now to avoid another sphere
  schema migration later.
- **Protocol message wiring is deferred.** The payload structs and
  `SendProtocolMessage` helper are defined now for schema stability,
  but only `RECOVERY_NEEDED` has a sender (the sentinel, prompt 04)
  and consumer (the operator) in Loop 3. The following wiring is
  deferred to future loops: `OUTPOST_DONE` emission from `sol resolve`,
  `MERGED`/`MERGE_FAILED` emission from forge subcommands,
  `MERGE_READY` emission from the sentinel.
- **Nudge-based delivery is deferred.** Messages are written to the
  store and polled by recipients. No real-time push notification or
  nudge queue is implemented in this loop.
- Don't modify the world schema. Messages are sphere-level.
- Keep backward compatibility — all Loop 0, 1, and 2 tests must pass.
- Commit after tests pass with message:
  `feat(store): add mail system with messages table, CRUD, and protocol helpers`
