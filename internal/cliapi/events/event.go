// Package events provides the CLI API type for event feed output.
package events

import (
	"time"

	ievents "github.com/nevinsm/sol/internal/events"
)

// Event is the CLI API representation of a feed event.
// JSON tags intentionally mirror the internal events.Event shape
// to preserve byte-equivalent output during the cliapi migration.
// Field renames (e.g. "ts" → "timestamp_at") are deferred to W2.1.
type Event struct {
	Timestamp  time.Time `json:"ts"`
	Source     string    `json:"source"`
	Type       string    `json:"type"`
	Actor      string    `json:"actor"`
	Visibility string    `json:"visibility"`
	Payload    any       `json:"payload"`
}

// FromEvent converts an internal events.Event to the CLI API Event type.
func FromEvent(ev ievents.Event) Event {
	return Event{
		Timestamp:  ev.Timestamp,
		Source:     ev.Source,
		Type:       ev.Type,
		Actor:      ev.Actor,
		Visibility: ev.Visibility,
		Payload:    ev.Payload,
	}
}
