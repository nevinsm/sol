package agents

import (
	"time"

	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/store"
)

// PostmortemReport is the CLI API response for 'sol agent postmortem --json'.
type PostmortemReport struct {
	Agent      PostmortemAgent    `json:"agent"`
	Session    PostmortemSession  `json:"session"`
	Writ       *PostmortemWrit    `json:"writ,omitempty"`
	Commits    []string           `json:"commits"`
	LastOutput string             `json:"last_output,omitempty"`
	Handoff    *PostmortemHandoff `json:"handoff,omitempty"`
}

// PostmortemAgent holds agent metadata for the postmortem report.
type PostmortemAgent struct {
	Name       string    `json:"name"`
	World      string    `json:"world"`
	Role       string    `json:"role"`
	State      string    `json:"state"`
	ActiveWrit string    `json:"active_writ,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// PostmortemSession holds session metadata for the postmortem report.
type PostmortemSession struct {
	Name      string     `json:"name"`
	Alive     bool       `json:"alive"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	Lifetime  string     `json:"lifetime,omitempty"`
}

// PostmortemWrit holds writ metadata for the postmortem report.
type PostmortemWrit struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// PostmortemHandoff holds handoff state for the postmortem report.
type PostmortemHandoff struct {
	Summary     string    `json:"summary"`
	HandedOffAt time.Time `json:"handed_off_at"`
	Commits     []string  `json:"commits,omitempty"`
}

// PostmortemAgentFromStore converts a store.Agent to PostmortemAgent.
func PostmortemAgentFromStore(a store.Agent) PostmortemAgent {
	return PostmortemAgent{
		Name:       a.Name,
		World:      a.World,
		Role:       a.Role,
		State:      a.State,
		ActiveWrit: a.ActiveWrit,
		CreatedAt:  a.CreatedAt,
		UpdatedAt:  a.UpdatedAt,
	}
}

// PostmortemWritFromStore converts a store.Writ to PostmortemWrit.
func PostmortemWritFromStore(w store.Writ) PostmortemWrit {
	return PostmortemWrit{
		ID:     w.ID,
		Title:  w.Title,
		Status: w.Status,
	}
}

// PostmortemHandoffFromState converts a handoff.State to PostmortemHandoff.
func PostmortemHandoffFromState(s handoff.State) PostmortemHandoff {
	return PostmortemHandoff{
		Summary:     s.Summary,
		HandedOffAt: s.HandedOffAt,
		Commits:     s.RecentCommits,
	}
}
