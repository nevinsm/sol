package escalation

import (
	"context"

	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

// LogNotifier writes escalation events to the event feed.
type LogNotifier struct {
	logger *events.Logger
}

// NewLogNotifier creates a LogNotifier. If logger is nil, Notify is a no-op.
func NewLogNotifier(logger *events.Logger) *LogNotifier {
	return &LogNotifier{logger: logger}
}

// Notify emits an escalation event. If the escalation has been previously
// notified (LastNotifiedAt is set), it emits EventConsulEscRenotified.
// Otherwise it emits EventEscalationCreated.
func (n *LogNotifier) Notify(_ context.Context, esc store.Escalation) error {
	if n.logger == nil {
		return nil
	}
	eventType := events.EventEscalationCreated
	if esc.LastNotifiedAt != nil {
		eventType = events.EventConsulEscRenotified
	}
	n.logger.Emit(eventType, esc.Source, "sol", "both", map[string]string{
		"id":          esc.ID,
		"severity":    esc.Severity,
		"source":      esc.Source,
		"description": esc.Description,
	})
	return nil
}

// Name returns "log".
func (n *LogNotifier) Name() string { return "log" }
