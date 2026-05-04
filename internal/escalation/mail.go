package escalation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// MailNotifier sends an escalation as a protocol message via the sphere store.
type MailNotifier struct {
	store *store.SphereStore
	// mu serializes check-and-send within a single process as a fast path
	// (avoids redundant DB writes when the same escalation is re-notified
	// in tight succession). The authoritative dedup is the partial UNIQUE
	// index on messages(thread_id) for pending non-empty thread_ids
	// (sphere schema v16) — that constraint protects against the race
	// across multiple consul / coordinator processes.
	mu sync.Mutex
}

// NewMailNotifier creates a MailNotifier.
func NewMailNotifier(sphereStore *store.SphereStore) *MailNotifier {
	return &MailNotifier{store: sphereStore}
}

// EscalationThreadID returns the ThreadID for an escalation-generated message.
func EscalationThreadID(escID string) string {
	return "esc:" + escID
}

// Notify sends a mail message to the autarch with the escalation details.
// Messages are linked via ThreadID="esc:{esc.ID}" so the inbox TUI can
// deduplicate them against the escalation itself.
//
// Dedup is enforced at the database layer via SendMessageWithThreadIfAbsent
// (a partial UNIQUE index ensures at most one pending message per
// thread_id). If a pending message with the same ThreadID already exists,
// the insert is silently skipped — including when concurrent senders race
// across processes.
func (n *MailNotifier) Notify(_ context.Context, esc store.Escalation) error {
	threadID := EscalationThreadID(esc.ID)

	// In-process fast path: serialize concurrent Notify calls within a
	// single MailNotifier so we don't bother the DB with redundant
	// INSERTs the constraint would just reject. The DB constraint remains
	// authoritative for cross-process races.
	n.mu.Lock()
	defer n.mu.Unlock()

	// Truncate description to 80 runes for subject.
	desc := esc.Description
	if len([]rune(desc)) > 80 {
		desc = string([]rune(desc)[:80])
	}
	subject := fmt.Sprintf("[ESCALATION-%s] %s", esc.Severity, desc)

	body := fmt.Sprintf("Escalation ID: %s\nSeverity: %s\nSource: %s\nTimestamp: %s\n\n%s",
		esc.ID, esc.Severity, esc.Source, esc.CreatedAt.Format(time.RFC3339), esc.Description)

	// Priority: 1 for critical/high, 2 for medium, 3 for low.
	priority := 2
	switch esc.Severity {
	case "critical", "high":
		priority = 1
	case "medium":
		priority = 2
	case "low":
		priority = 3
	}

	// SendMessageWithThreadIfAbsent uses INSERT OR IGNORE backed by the
	// partial UNIQUE index. A returned (_, false, nil) means a pending
	// message with this thread_id already exists — treat as already-
	// notified (no error).
	_, _, err := n.store.SendMessageWithThreadIfAbsent(esc.Source, config.Autarch, subject, body, priority, "notification", threadID)
	if err != nil {
		return fmt.Errorf("failed to send escalation mail: %w", err)
	}
	return nil
}

// Name returns "mail".
func (n *MailNotifier) Name() string { return "mail" }
