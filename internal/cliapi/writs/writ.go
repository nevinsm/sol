// Package writs provides the CLI API type for writ entities.
package writs

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// Writ is the CLI API representation of a tracked writ.
type Writ struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Kind        string     `json:"kind"`
	Priority    int        `json:"priority"`
	World       string     `json:"world"`
	Assignee    string     `json:"assignee,omitempty"`
	Labels      []string   `json:"labels"`
	Caravan     string     `json:"caravan,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
}

// FromStoreWrit converts a store.Writ to the CLI API Writ type.
// The world and caravanID parameters supply context not stored on the writ itself.
func FromStoreWrit(w store.Writ, world, caravanID string) Writ {
	labels := w.Labels
	if labels == nil {
		labels = []string{}
	}
	return Writ{
		ID:          w.ID,
		Title:       w.Title,
		Description: w.Description,
		Status:      w.Status,
		Kind:        w.Kind,
		Priority:    w.Priority,
		World:       world,
		Assignee:    w.Assignee,
		Labels:      labels,
		Caravan:     caravanID,
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
		ClosedAt:    w.ClosedAt,
	}
}
