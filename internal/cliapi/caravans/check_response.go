package caravans

import "github.com/nevinsm/sol/internal/store"

// CheckResponse is the CLI API response for caravan check and caravan status
// (single-caravan detail view) --json output.
type CheckResponse struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Status    string      `json:"status"`
	BlockedBy []string    `json:"blocked_by_caravans,omitempty"`
	Items     []CheckItem `json:"items"`
	// TotalTokens/TotalSessions are caravan-level vitals sums across items
	// that have execution history (see WithVitals). Omitted (zero) when no
	// item has any agent_history rows yet.
	TotalTokens   int64 `json:"total_tokens,omitempty"`
	TotalSessions int   `json:"total_sessions,omitempty"`
}

// CheckItem is a caravan item in a check/status response. Field names preserve
// the existing JSON shape (writ_status, not status) — renames happen in W2.1.
type CheckItem struct {
	WritID     string `json:"writ_id"`
	World      string `json:"world"`
	Phase      int    `json:"phase"`
	WritStatus string `json:"writ_status"`
	Ready      bool   `json:"ready"`
	Assignee   string `json:"assignee,omitempty"`
	// TotalTokens/SessionCount are populated by WithVitals; both stay zero
	// (and are omitted) for an item with no agent_history rows yet.
	TotalTokens  int64 `json:"total_tokens,omitempty"`
	SessionCount int   `json:"session_count,omitempty"`
}

// NewCheckResponse builds a CheckResponse from store types.
func NewCheckResponse(c *store.Caravan, statuses []store.CaravanItemStatus, blockedBy []string) CheckResponse {
	items := make([]CheckItem, 0, len(statuses))
	for _, st := range statuses {
		items = append(items, CheckItem{
			WritID:     st.WritID,
			World:      st.World,
			Phase:      st.Phase,
			WritStatus: st.WritStatus,
			Ready:      st.Ready,
			Assignee:   st.Assignee,
		})
	}
	resp := CheckResponse{
		ID:        c.ID,
		Name:      c.Name,
		Status:    string(c.Status),
		BlockedBy: blockedBy,
		Items:     items,
	}
	if resp.Items == nil {
		resp.Items = []CheckItem{}
	}
	return resp
}

// WithVitals attaches per-item execution vitals (total tokens and session
// count) to an already-built CheckResponse, keyed by writ ID, and rolls them
// up into the caravan-level TotalTokens/TotalSessions. Items with no entry
// in vitals (e.g. never cast, or the lookup failed) are left at zero.
func (r CheckResponse) WithVitals(vitals map[string]*store.WritVitals) CheckResponse {
	for i := range r.Items {
		v, ok := vitals[r.Items[i].WritID]
		if !ok || v == nil {
			continue
		}
		r.Items[i].TotalTokens = v.InputTokens + v.OutputTokens + v.CacheTokens
		r.Items[i].SessionCount = v.SessionCount
		r.TotalTokens += r.Items[i].TotalTokens
		r.TotalSessions += r.Items[i].SessionCount
	}
	return r
}
