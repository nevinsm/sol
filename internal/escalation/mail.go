package escalation

import (
	"context"
	"fmt"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// MailNotifier sends an escalation as a protocol message via the sphere store.
type MailNotifier struct {
	store *store.Store
}

// NewMailNotifier creates a MailNotifier.
func NewMailNotifier(sphereStore *store.Store) *MailNotifier {
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
// If a pending message with the same ThreadID already exists, the
// notification is skipped to prevent duplicate messages during
// consul re-notification cycles.
func (n *MailNotifier) Notify(_ context.Context, esc store.Escalation) error {
	threadID := EscalationThreadID(esc.ID)

	// Skip if a pending message with this ThreadID already exists.
	// This prevents duplicates when consul re-routes aging escalations.
	exists, err := n.store.HasPendingThreadMessage(threadID)
	if err != nil {
		return fmt.Errorf("failed to check pending escalation mail: %w", err)
	}
	if exists {
		return nil
	}

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

	_, err = n.store.SendMessageWithThread(esc.Source, config.Autarch, subject, body, priority, "notification", threadID)
	if err != nil {
		return fmt.Errorf("failed to send escalation mail: %w", err)
	}
	return nil
}

// Name returns "mail".
func (n *MailNotifier) Name() string { return "mail" }
