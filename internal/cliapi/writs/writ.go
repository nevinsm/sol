// Package writs provides the CLI API type for writ entities.
package writs

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// Writ is the CLI API representation of a tracked writ.
type Writ struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	Description      string            `json:"description"`
	Status           string            `json:"status"`
	Kind             string            `json:"kind"`
	Priority         int               `json:"priority"`
	World            string            `json:"world"`
	Assignee         string            `json:"assignee,omitempty"`
	Labels           []string          `json:"labels"`
	CaravanID        string            `json:"caravan_id,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	ClosedAt         *time.Time        `json:"closed_at,omitempty"`
	Vitals           *Vitals           `json:"vitals,omitempty"`
	ResolutionReport *ResolutionReport `json:"resolution_report,omitempty"`
}

// Vitals is the CLI API representation of a writ's execution rollup (see
// internal/store.WritVitals): session/handoff/respawn counts, token totals,
// wall time, and the distinct agents that touched the writ. Only populated
// by commands that look it up (currently `sol writ status --json`); absent
// (nil) whenever the writ has no agent_history rows yet.
type Vitals struct {
	SessionCount int        `json:"session_count"`
	HandoffCount int        `json:"handoff_count"`
	RespawnCount int        `json:"respawn_count"`
	InputTokens  int64      `json:"input_tokens"`
	OutputTokens int64      `json:"output_tokens"`
	CacheTokens  int64      `json:"cache_tokens"`
	Agents       []string   `json:"agents"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
}

// FromStoreVitals converts a store.WritVitals to the CLI API Vitals type.
// Returns nil if v is nil (no agent_history rows for the writ).
func FromStoreVitals(v *store.WritVitals) *Vitals {
	if v == nil {
		return nil
	}
	agents := v.AgentNames
	if agents == nil {
		agents = []string{}
	}
	return &Vitals{
		SessionCount: v.SessionCount,
		HandoffCount: v.HandoffCount,
		RespawnCount: v.RespawnCount,
		InputTokens:  v.InputTokens,
		OutputTokens: v.OutputTokens,
		CacheTokens:  v.CacheTokens,
		Agents:       agents,
		StartedAt:    v.StartedAt,
		EndedAt:      v.EndedAt,
	}
}

// ResolutionReport is the CLI API representation of a writ's captured
// resolution report (see internal/resolutionreport). Only populated by
// commands that look it up (currently `sol writ status --json`); absent
// (nil) whenever no report was captured for the writ.
type ResolutionReport struct {
	Path    string `json:"path"`
	Content string `json:"content"`
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
		CaravanID:   caravanID,
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
		ClosedAt:    w.ClosedAt,
	}
}
