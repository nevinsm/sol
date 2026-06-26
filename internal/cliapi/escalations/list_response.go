package escalations

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// ListEscalation is the flat JSON shape emitted by "sol escalation list --json".
// Timestamp fields use time.Time so the JSON encoder produces RFC3339 automatically.
// Field names match the post-cli-polish shape; renames (if any) happen in W2.1.
type ListEscalation struct {
	ID             string     `json:"id"`
	Severity       string     `json:"severity"`
	Status         string     `json:"status"`
	Source         string     `json:"source"`
	SourceRef      string     `json:"source_ref"`
	Description    string     `json:"description"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastNotifiedAt *time.Time `json:"last_notified_at,omitempty"`
}

// ListEscalationsFromStore converts a slice of store.Escalation to the list
// JSON representation. The result is never nil — an empty input produces an
// empty (non-nil) slice so JSON marshalling emits "[]" rather than "null".
func ListEscalationsFromStore(escs []store.Escalation) []ListEscalation {
	out := make([]ListEscalation, len(escs))
	for i, e := range escs {
		out[i] = ListEscalation{
			ID:             e.ID,
			Severity:       e.Severity,
			Status:         e.Status,
			Source:         e.Source,
			SourceRef:      e.SourceRef,
			Description:    e.Description,
			CreatedAt:      e.CreatedAt,
			UpdatedAt:      e.UpdatedAt,
			LastNotifiedAt: e.LastNotifiedAt,
		}
	}
	return out
}
