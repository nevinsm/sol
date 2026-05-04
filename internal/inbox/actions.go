package inbox

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/events"
)

// actionResultMsg carries the result of an action back to the model.
type actionResultMsg struct {
	itemID string
	action string // "ack", "resolve", "read", "dismiss"
	err    error
}

// ackCmd acknowledges an inbox item (escalation or message).
// If logger is non-nil and the item is an escalation, emits EventEscalationAcked on success.
func ackCmd(src DataSource, item InboxItem, logger *events.Logger) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch item.Type {
		case ItemEscalation:
			err = src.AckEscalation(item.ID)
			if err == nil && logger != nil {
				logger.Emit(events.EventEscalationAcked, item.Source, "sol", "both", map[string]string{
					"id":     item.ID,
					"source": item.Source,
				})
			}
		case ItemMail:
			err = src.AckMessage(item.ID)
		}
		return actionResultMsg{itemID: item.ID, action: "ack", err: err}
	}
}

// resolveCmd resolves an escalation. No-op for messages.
// If logger is non-nil, emits EventEscalationResolved on success.
func resolveCmd(src DataSource, item InboxItem, logger *events.Logger) tea.Cmd {
	if item.Type != ItemEscalation {
		return func() tea.Msg {
			return actionResultMsg{itemID: item.ID, action: "resolve", err: fmt.Errorf("resolve only applies to escalations")}
		}
	}
	return func() tea.Msg {
		err := src.ResolveEscalation(item.ID)
		if err == nil && logger != nil {
			logger.Emit(events.EventEscalationResolved, item.Source, "sol", "both", map[string]string{
				"id":     item.ID,
				"source": item.Source,
			})
		}
		return actionResultMsg{itemID: item.ID, action: "resolve", err: err}
	}
}

// readCmd marks a mail item as read in the underlying store.
// Returns nil for escalation items (read state is mail-only).
// On success the underlying store records read=1 for the message.
func readCmd(src DataSource, item InboxItem) tea.Cmd {
	if item.Type != ItemMail {
		return nil
	}
	return func() tea.Msg {
		_, err := src.ReadMessage(item.ID)
		return actionResultMsg{itemID: item.ID, action: "read", err: err}
	}
}

// dismissCmd dismisses a message from the inbox. No-op for escalations.
// Sets delivery='dismissed' so the message no longer appears in the inbox
// but remains accessible via sol mail list.
func dismissCmd(src DataSource, item InboxItem) tea.Cmd {
	if item.Type != ItemMail {
		return func() tea.Msg {
			return actionResultMsg{itemID: item.ID, action: "dismiss", err: fmt.Errorf("dismiss only applies to messages")}
		}
	}
	return func() tea.Msg {
		err := src.DismissMessage(item.ID)
		return actionResultMsg{itemID: item.ID, action: "dismiss", err: err}
	}
}
