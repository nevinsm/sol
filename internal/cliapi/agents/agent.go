// Package agents provides the CLI API type for agent entities.
package agents

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// Agent is the CLI API representation of an agent.
type Agent struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	World      string     `json:"world"`
	Role       string     `json:"role"`
	State      string     `json:"state"`
	ActiveWrit string     `json:"active_writ,omitempty"`
	Model      string     `json:"model,omitempty"`
	Account    string     `json:"account,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
}

// FromStoreAgent converts a store.Agent to the CLI API Agent type.
// The model and account parameters supply runtime context not stored on the agent record.
func FromStoreAgent(a store.Agent, model, account string, lastSeenAt *time.Time) Agent {
	return Agent{
		ID:         a.ID,
		Name:       a.Name,
		World:      a.World,
		Role:       a.Role,
		State:      a.State,
		ActiveWrit: a.ActiveWrit,
		Model:      model,
		Account:    account,
		LastSeenAt: lastSeenAt,
	}
}
