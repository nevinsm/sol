// Package inbox provides CLI API types for the inbox command.
package inbox

import (
	inboxpkg "github.com/nevinsm/sol/internal/inbox"
)

// Item is the CLI API representation of a unified inbox item.
// It matches the JSON shape produced by 'sol inbox --json'.
type Item struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Priority    int    `json:"priority"`
	Source      string `json:"source"`
	Description string `json:"description"`
	Age         string `json:"age"`
	CreatedAt   string `json:"created_at"`
}

// FromInboxItem converts an internal inbox.InboxItem to the CLI API Item type.
func FromInboxItem(item inboxpkg.InboxItem) Item {
	return Item{
		ID:          item.ID,
		Type:        item.TypeString(),
		Priority:    item.Priority,
		Source:      item.Source,
		Description: item.Description,
		Age:         item.Age(),
		CreatedAt:   item.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// FromInboxItems converts a slice of internal inbox.InboxItem to CLI API Items.
func FromInboxItems(items []inboxpkg.InboxItem) []Item {
	out := make([]Item, len(items))
	for i, item := range items {
		out[i] = FromInboxItem(item)
	}
	return out
}
