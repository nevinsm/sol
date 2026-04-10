// Package escalations provides the CLI API type for escalation entities.
package escalations

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// Escalation is the CLI API representation of an escalation.
type Escalation struct {
	ID             string     `json:"id"`
	Severity       string     `json:"severity"`
	Status         string     `json:"status"`
	World          string     `json:"world"`
	Component      string     `json:"component"`
	SourceRef      string     `json:"source_ref,omitempty"`
	Message        string     `json:"message"`
	CreatedAt      time.Time  `json:"created_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

// FromStoreEscalation converts a store.Escalation to the CLI API Escalation type.
// The world parameter supplies context not stored on the escalation itself.
// The acknowledgedAt and resolvedAt parameters are derived from the escalation's
// status and timestamps rather than stored directly.
func FromStoreEscalation(e store.Escalation, world string, acknowledgedAt, resolvedAt *time.Time) Escalation {
	return Escalation{
		ID:             e.ID,
		Severity:       e.Severity,
		Status:         e.Status,
		World:          world,
		Component:      e.Source,
		SourceRef:      e.SourceRef,
		Message:        e.Description,
		CreatedAt:      e.CreatedAt,
		AcknowledgedAt: acknowledgedAt,
		ResolvedAt:     resolvedAt,
	}
}
