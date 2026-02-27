package escalation

import (
	"context"
	"fmt"

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

// Notify sends a mail message to "operator" with the escalation details.
func (n *MailNotifier) Notify(_ context.Context, esc store.Escalation) error {
	// Truncate description to 80 chars for subject.
	desc := esc.Description
	if len(desc) > 80 {
		desc = desc[:80]
	}
	subject := fmt.Sprintf("[ESCALATION-%s] %s", esc.Severity, desc)

	body := fmt.Sprintf("Escalation ID: %s\nSeverity: %s\nSource: %s\nTimestamp: %s\n\n%s",
		esc.ID, esc.Severity, esc.Source, esc.CreatedAt.Format("2006-01-02T15:04:05Z"), esc.Description)

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

	_, err := n.store.SendMessage(esc.Source, "operator", subject, body, priority, "notification")
	if err != nil {
		return fmt.Errorf("failed to send escalation mail: %w", err)
	}
	return nil
}

// Name returns "mail".
func (n *MailNotifier) Name() string { return "mail" }
