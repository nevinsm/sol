package escalation

import (
	"context"

	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

// Router routes escalations to notifiers based on severity.
type Router struct {
	rules map[string][]Notifier
}

// NewRouter creates an empty router.
func NewRouter() *Router {
	return &Router{
		rules: make(map[string][]Notifier),
	}
}

// AddRule adds notifiers for a severity level. Can be called multiple
// times for the same severity — notifiers accumulate.
func (r *Router) AddRule(severity string, notifiers ...Notifier) {
	r.rules[severity] = append(r.rules[severity], notifiers...)
}

// Route sends an escalation to all notifiers registered for its severity.
// Returns the first error encountered, but continues notifying remaining
// notifiers (best-effort delivery).
// Returns nil if no rules match the severity.
func (r *Router) Route(ctx context.Context, esc store.Escalation) error {
	notifiers, ok := r.rules[esc.Severity]
	if !ok {
		return nil
	}

	var firstErr error
	for _, n := range notifiers {
		if err := n.Notify(ctx, esc); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// DefaultRouter creates a router with standard severity routing:
//
//	low:      LogNotifier
//	medium:   LogNotifier + MailNotifier
//	high:     LogNotifier + MailNotifier + WebhookNotifier (if webhookURL != "")
//	critical: LogNotifier + MailNotifier + WebhookNotifier (if webhookURL != "")
//
// If webhookURL is empty, high/critical omit the webhook notifier.
// If logger is nil, log notifier is a no-op.
func DefaultRouter(logger *events.Logger, townStore *store.Store, webhookURL string) *Router {
	logN := NewLogNotifier(logger)
	mailN := NewMailNotifier(townStore)

	r := NewRouter()
	r.AddRule("low", logN)
	r.AddRule("medium", logN, mailN)

	if webhookURL != "" {
		webhookN := NewWebhookNotifier(webhookURL)
		r.AddRule("high", logN, mailN, webhookN)
		r.AddRule("critical", logN, mailN, webhookN)
	} else {
		r.AddRule("high", logN, mailN)
		r.AddRule("critical", logN, mailN)
	}

	return r
}
