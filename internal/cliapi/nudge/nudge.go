// Package nudge provides the CLI API types for nudge queue entities.
package nudge

import (
	"time"
)

// Nudge is the CLI API representation of a queued nudge message.
type Nudge struct {
	ID       string    `json:"id"`
	Target   string    `json:"target"`
	Body     string    `json:"body"`
	Source   string    `json:"source"`
	QueuedAt time.Time `json:"queued_at"`
}

// NudgeQueueSummary is a per-agent summary of pending nudges.
type NudgeQueueSummary struct {
	Agent        string `json:"agent"`
	World        string `json:"world"`
	PendingCount int    `json:"pending_count"`
}
