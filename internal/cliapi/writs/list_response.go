package writs

import (
	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/store"
)

// CaravanRef is the JSON shape for the caravan a writ belongs to.
type CaravanRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WritListItem is the JSON shape emitted by `sol writ list --json`.
// Unlike the default-marshal of store.Writ (which exposes PascalCase Go
// field names), this is an explicit snake_case surface so downstream tools
// can depend on stable field names and includes the caravan join.
type WritListItem struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	Priority    int            `json:"priority"`
	Kind        string         `json:"kind"`
	Assignee    string         `json:"assignee,omitempty"`
	ParentID    string         `json:"parent_id,omitempty"`
	CreatedBy   string         `json:"created_by"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	ClosedAt    string         `json:"closed_at,omitempty"`
	CloseReason string         `json:"close_reason,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Caravan     *CaravanRef    `json:"caravan"`
}

// WritListItemFromStore converts a store.Writ to a WritListItem.
// The caravan parameter is optional; pass nil for writs without caravan membership.
func WritListItemFromStore(w store.Writ, caravan *CaravanRef) WritListItem {
	item := WritListItem{
		ID:          w.ID,
		Title:       w.Title,
		Description: w.Description,
		Status:      w.Status,
		Priority:    w.Priority,
		Kind:        w.Kind,
		Assignee:    w.Assignee,
		ParentID:    w.ParentID,
		CreatedBy:   w.CreatedBy,
		CreatedAt:   cliformat.FormatTimestamp(w.CreatedAt),
		UpdatedAt:   cliformat.FormatTimestamp(w.UpdatedAt),
		CloseReason: w.CloseReason,
		Labels:      w.Labels,
		Metadata:    w.Metadata,
		Caravan:     caravan,
	}
	if w.ClosedAt != nil {
		item.ClosedAt = cliformat.FormatTimestamp(*w.ClosedAt)
	}
	return item
}
