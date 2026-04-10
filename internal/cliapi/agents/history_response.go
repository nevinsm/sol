package agents

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// HistoryEntry is the CLI API representation of an agent history row.
// It mirrors the existing JSON output shape of 'sol agent history --json'.
type HistoryEntry struct {
	ID        string     `json:"id"`
	AgentName string     `json:"agent_name"`
	WritID    string     `json:"writ_id,omitempty"`
	Action    string     `json:"action"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Duration  string     `json:"duration,omitempty"`
	Outcome   string     `json:"outcome"`
	Summary   string     `json:"summary,omitempty"`
}

// FromStoreHistoryEntry converts a store.HistoryEntry to the CLI API
// HistoryEntry type. The duration and outcome parameters supply derived
// values computed by the command layer (outcome is inferred from writ
// status; duration is formatted from ended_at - started_at).
func FromStoreHistoryEntry(e store.HistoryEntry, duration, outcome string) HistoryEntry {
	return HistoryEntry{
		ID:        e.ID,
		AgentName: e.AgentName,
		WritID:    e.WritID,
		Action:    e.Action,
		StartedAt: e.StartedAt,
		EndedAt:   e.EndedAt,
		Duration:  duration,
		Outcome:   outcome,
		Summary:   e.Summary,
	}
}
