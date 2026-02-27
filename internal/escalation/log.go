package escalation

import (
	"context"

	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/store"
)

// LogNotifier writes escalation events to the event feed.
type LogNotifier struct {
	logger *events.Logger
}

// NewLogNotifier creates a LogNotifier. If logger is nil, Notify is a no-op.
func NewLogNotifier(logger *events.Logger) *LogNotifier {
	return &LogNotifier{logger: logger}
}

// Notify emits an escalation_created event.
func (n *LogNotifier) Notify(_ context.Context, esc store.Escalation) error {
	if n.logger == nil {
		return nil
	}
	n.logger.Emit(events.EventEscalationCreated, esc.Source, "gt", "both", map[string]string{
		"id":          esc.ID,
		"severity":    esc.Severity,
		"source":      esc.Source,
		"description": esc.Description,
	})
	return nil
}

// Name returns "log".
func (n *LogNotifier) Name() string { return "log" }
