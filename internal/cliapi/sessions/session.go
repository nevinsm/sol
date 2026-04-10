// Package sessions provides the CLI API type for session entities.
package sessions

import (
	"time"

	"github.com/nevinsm/sol/internal/session"
)

// Session is the CLI API representation of a tmux session.
type Session struct {
	Name           string     `json:"name"`
	Role           string     `json:"role"`
	World          string     `json:"world"`
	Alive          bool       `json:"alive"`
	StartedAt      time.Time  `json:"started_at"`
	LastActivityAt *time.Time `json:"last_activity_at,omitempty"`
}

// FromSessionInfo converts a session.SessionInfo to the CLI API Session type.
// The lastActivityAt parameter supplies an optional last-activity timestamp.
func FromSessionInfo(s session.SessionInfo, lastActivityAt *time.Time) Session {
	return Session{
		Name:           s.Name,
		Role:           s.Role,
		World:          s.World,
		Alive:          s.Alive,
		StartedAt:      s.StartedAt,
		LastActivityAt: lastActivityAt,
	}
}
