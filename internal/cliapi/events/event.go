// Package events provides the CLI API type for event feed output.
package events

import (
	"time"

	ievents "github.com/nevinsm/sol/internal/events"
)

// Event is the CLI API representation of a feed event.
type Event struct {
	// ID is a stable content-derived fingerprint of the event (see
	// ievents.EventID) — the same event id is produced regardless of
	// which feed file it was read from. Not a database primary key;
	// external consumers should treat it as an opaque dedup/cursor key.
	ID         string    `json:"id"`
	Timestamp  time.Time `json:"occurred_at"`
	Source     string    `json:"source"`
	Type       string    `json:"type"`
	Actor      string    `json:"actor"`
	Visibility string    `json:"visibility"`
	Payload    any       `json:"payload"`
}

// FromEvent converts an internal events.Event to the CLI API Event type.
func FromEvent(ev ievents.Event) Event {
	return Event{
		ID:         ievents.EventID(ev),
		Timestamp:  ev.Timestamp,
		Source:     ev.Source,
		Type:       ev.Type,
		Actor:      ev.Actor,
		Visibility: ev.Visibility,
		Payload:    ev.Payload,
	}
}
