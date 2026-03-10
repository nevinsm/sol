package inbox

import (
	"sort"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
)

// ItemType distinguishes escalations from messages in the unified inbox.
type ItemType int

const (
	ItemEscalation ItemType = iota
	ItemMail
)

// InboxItem is the unified representation of an escalation or message.
type InboxItem struct {
	ID          string
	Type        ItemType
	Priority    int    // 1 = highest (P1), 2, 3
	Source      string // escalation source or message sender
	Description string // escalation description or message subject
	CreatedAt   time.Time

	// Full content for detail view.
	Escalation *store.Escalation // set when Type == ItemEscalation
	Message    *store.Message    // set when Type == ItemMail
}

// TypeString returns "escalation" or "mail".
func (i InboxItem) TypeString() string {
	if i.Type == ItemEscalation {
		return "escalation"
	}
	return "mail"
}

// Age returns a human-readable duration since creation.
func (i InboxItem) Age() string {
	return status.FormatDuration(time.Since(i.CreatedAt))
}

// escalationPriority maps escalation severity to a unified priority number.
// Lower = higher priority.
func escalationPriority(severity string) int {
	switch severity {
	case "critical":
		return 1
	case "high":
		return 2
	case "medium":
		return 3
	case "low":
		return 3
	default:
		return 3
	}
}

// DataSource abstracts the store methods needed by the inbox.
type DataSource interface {
	ListOpenEscalations() ([]store.Escalation, error)
	Inbox(recipient string) ([]store.Message, error)
	AckEscalation(id string) error
	ResolveEscalation(id string) error
	AckMessage(id string) error
	ReadMessage(id string) (*store.Message, error)
}

// FetchItems queries escalations and messages, deduplicates, and returns
// a unified sorted list of inbox items.
func FetchItems(src DataSource) []InboxItem {
	var items []InboxItem

	// Fetch open + acknowledged (not resolved) escalations.
	if escs, err := src.ListOpenEscalations(); err == nil {
		for i := range escs {
			esc := escs[i]
			items = append(items, InboxItem{
				ID:          esc.ID,
				Type:        ItemEscalation,
				Priority:    escalationPriority(esc.Severity),
				Source:      esc.Source,
				Description: esc.Description,
				CreatedAt:   esc.CreatedAt,
				Escalation:  &esc,
			})
		}
	}

	// Fetch pending messages for the operator (autarch).
	if msgs, err := src.Inbox("autarch"); err == nil {
		for i := range msgs {
			msg := msgs[i]

			// Filter out escalation notification duplicates (ThreadID starts with "esc:").
			if strings.HasPrefix(msg.ThreadID, "esc:") {
				continue
			}

			items = append(items, InboxItem{
				ID:          msg.ID,
				Type:        ItemMail,
				Priority:    msg.Priority,
				Source:      msg.Sender,
				Description: msg.Subject,
				CreatedAt:   msg.CreatedAt,
				Message:     &msg,
			})
		}
	}

	// Sort by priority ASC (lower number = higher priority), then age DESC (oldest first = created_at ASC).
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})

	return items
}
