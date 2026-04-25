package inbox

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
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
	Priority    int    // 1 = highest (P1), 2, 3, 4
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
		return 4
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
	DismissMessage(id string) error
}

// FetchItems queries escalations and messages, deduplicates, and returns
// a unified sorted list of inbox items. Any fetch errors are returned so
// callers can surface them to the user rather than silently treating
// unavailability as an empty inbox.
func FetchItems(src DataSource) ([]InboxItem, error) {
	var items []InboxItem
	var errs []string

	// Fetch open + acknowledged (not resolved) escalations.
	escs, err := src.ListOpenEscalations()
	if err != nil {
		errs = append(errs, fmt.Sprintf("escalations: %v", err))
	} else {
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

	// Fetch pending messages for the operator.
	msgs, err := src.Inbox(config.Autarch)
	if err != nil {
		errs = append(errs, fmt.Sprintf("inbox: %v", err))
	} else {
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

	if len(errs) > 0 {
		return items, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return items, nil
}
