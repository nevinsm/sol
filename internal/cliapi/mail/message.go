// Package mail provides the CLI API type for mail message entities.
package mail

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// Message is the CLI API representation of a mail message.
type Message struct {
	ID             string     `json:"id"`
	Sender         string     `json:"sender"`
	Recipient      string     `json:"recipient"`
	Subject        string     `json:"subject"`
	Body           string     `json:"body"`
	Priority       int        `json:"priority"`
	CreatedAt      time.Time  `json:"created_at"`
	ReadAt         *time.Time `json:"read_at,omitempty"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
}

// FromStoreMessage converts a store.Message to the CLI API Message type.
// The readAt timestamp is derived from the message's Read flag and must be
// supplied by the caller (the store tracks a bool, not a timestamp).
func FromStoreMessage(m store.Message, readAt *time.Time) Message {
	return Message{
		ID:             m.ID,
		Sender:         m.Sender,
		Recipient:      m.Recipient,
		Subject:        m.Subject,
		Body:           m.Body,
		Priority:       m.Priority,
		CreatedAt:      m.CreatedAt,
		ReadAt:         readAt,
		AcknowledgedAt: m.AckedAt,
	}
}
