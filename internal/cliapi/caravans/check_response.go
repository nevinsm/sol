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
