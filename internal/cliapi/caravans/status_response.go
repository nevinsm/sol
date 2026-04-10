package caravans

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// CaravanSummary is the CLI API representation of a caravan in the
// caravan status (no-args, list active) --json output. Matches the
// store.Caravan JSON shape for byte-equivalence.
type CaravanSummary struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Owner     string     `json:"owner"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

// FromStoreCaravanSummary converts a store.Caravan to a CaravanSummary.
func FromStoreCaravanSummary(c store.Caravan) CaravanSummary {
	return CaravanSummary{
		ID:        c.ID,
		Name:      c.Name,
		Status:    string(c.Status),
		Owner:     c.Owner,
		CreatedAt: c.CreatedAt,
		ClosedAt:  c.ClosedAt,
	}
}
