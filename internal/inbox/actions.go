package inbox

import (
	tea "github.com/charmbracelet/bubbletea"
)

// actionResultMsg carries the result of an action back to the model.
type actionResultMsg struct {
	itemID string
	action string // "ack", "resolve", "read"
	err    error
}

// ackCmd acknowledges an inbox item (escalation or message).
func ackCmd(src DataSource, item InboxItem) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch item.Type {
		case ItemEscalation:
			err = src.AckEscalation(item.ID)
		case ItemMail:
			err = src.AckMessage(item.ID)
		}
		return actionResultMsg{itemID: item.ID, action: "ack", err: err}
	}
}

// resolveCmd resolves an escalation. No-op for messages.
func resolveCmd(src DataSource, item InboxItem) tea.Cmd {
	if item.Type != ItemEscalation {
		return nil
	}
	return func() tea.Msg {
		err := src.ResolveEscalation(item.ID)
		return actionResultMsg{itemID: item.ID, action: "resolve", err: err}
	}
}

// readCmd marks a message as read. No-op for escalations.
func readCmd(src DataSource, item InboxItem) tea.Cmd {
	if item.Type != ItemMail {
		return nil
	}
	return func() tea.Msg {
		_, err := src.ReadMessage(item.ID)
		return actionResultMsg{itemID: item.ID, action: "read", err: err}
	}
}
