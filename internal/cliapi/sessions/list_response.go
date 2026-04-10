package sessions

import (
	"time"

	"github.com/nevinsm/sol/internal/session"
)

// ListItem is the CLI API representation of a session in list output.
// It preserves the exact JSON shape of session.SessionInfo for backward
// compatibility; field normalization happens in W2.1.
type ListItem struct {
	Name      string    `json:"name"`
	PID       int       `json:"pid"`
	Role      string    `json:"role"`
	World     string    `json:"world"`
	WorkDir   string    `json:"workdir"`
	StartedAt time.Time `json:"started_at"`
	CreatedAt time.Time `json:"created_at"`
	Alive     bool      `json:"alive"`
}

// FromSessionInfoSlice converts a slice of session.SessionInfo to a
// []ListItem suitable for JSON output. The returned slice is never nil —
// an empty input produces an empty (non-nil) slice so that JSON output
// renders as [] rather than null.
func FromSessionInfoSlice(infos []session.SessionInfo) []ListItem {
	items := make([]ListItem, len(infos))
	for i, s := range infos {
		items[i] = ListItem{
			Name:      s.Name,
			PID:       s.PID,
			Role:      s.Role,
			World:     s.World,
			WorkDir:   s.WorkDir,
			StartedAt: s.StartedAt,
			CreatedAt: s.CreatedAt,
			Alive:     s.Alive,
		}
	}
	return items
}
