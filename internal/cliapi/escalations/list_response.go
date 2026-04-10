package escalations

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// ListEscalation is the flat JSON shape emitted by "sol escalation list --json".
// All timestamp fields are pre-formatted RFC 3339 strings to preserve the
// existing byte-level output contract. Field names match the post-cli-polish
// shape; renames (if any) happen in W2.1.
type ListEscalation struct {
	ID             string `json:"id"`
	Severity       string `json:"severity"`
	Status         string `json:"status"`
	Source         string `json:"source"`
	SourceRef      string `json:"source_ref"`
	Description    string `json:"description"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastNotifiedAt string `json:"last_notified_at,omitempty"`
}

// ListEscalationsFromStore converts a slice of store.Escalation to the list
// JSON representation. The result is never nil — an empty input produces an
// empty (non-nil) slice so JSON marshalling emits "[]" rather than "null".
func ListEscalationsFromStore(escs []store.Escalation) []ListEscalation {
	out := make([]ListEscalation, len(escs))
	for i, e := range escs {
		j := ListEscalation{
			ID:          e.ID,
			Severity:    e.Severity,
			Status:      e.Status,
			Source:      e.Source,
			SourceRef:   e.SourceRef,
			Description: e.Description,
			CreatedAt:   e.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   e.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if e.LastNotifiedAt != nil {
			j.LastNotifiedAt = e.LastNotifiedAt.UTC().Format(time.RFC3339)
		}
		out[i] = j
	}
	return out
}
