package agents

import (
	"time"

	"github.com/nevinsm/sol/internal/events"
)

// HandoffEvent is the CLI API representation of a handoff event.
type HandoffEvent struct {
	Timestamp time.Time `json:"occurred_at"`
	Source     string    `json:"source"`
	Type       string    `json:"type"`
	Actor      string    `json:"actor"`
	Visibility string    `json:"visibility"`
	Payload    any       `json:"payload"`
}

// FromEvent converts an events.Event to the CLI API HandoffEvent type.
func FromEvent(e events.Event) HandoffEvent {
	return HandoffEvent{
		Timestamp: e.Timestamp,
		Source:     e.Source,
		Type:       e.Type,
		Actor:      e.Actor,
		Visibility: e.Visibility,
		Payload:    e.Payload,
	}
}

// FromEvents converts a slice of events.Event to CLI API HandoffEvent types.
// Returns an empty (non-nil) slice when the input is empty, ensuring JSON
// output renders as [] rather than null.
func FromEvents(evs []events.Event) []HandoffEvent {
	result := make([]HandoffEvent, len(evs))
	for i, e := range evs {
		result[i] = FromEvent(e)
	}
	return result
}
