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
