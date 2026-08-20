package inbox

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

// actionResultMsg carries the result of an action back to the model.
type actionResultMsg struct {
	itemID string
	action string // "ack", "resolve", "read", "dismiss"
	err    error
}

// ackCmd acknowledges an inbox item (escalation, standalone message, or
// every pending message in a grouped mail thread). Callers gate this to
// applicable selections via the context-sensitive footer (ack applies to
// both escalations and mail), so no item-type error path is needed here.
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
			if item.ThreadID != "" {
				err = ackAllMessages(src, item.ThreadMessages)
			} else {
				err = src.AckMessage(item.ID)
			}
		}
		return actionResultMsg{itemID: item.ID, action: "ack", err: err}
	}
}

// resolveCmd resolves an escalation. Callers must only invoke this for
// ItemEscalation selections (the footer only advertises [r]esolve when an
// escalation is selected — see updateListKeys/updateDetailKeys), so no
// item-type guard is needed here.
// If logger is non-nil, emits EventEscalationResolved on success.
func resolveCmd(src DataSource, item InboxItem, logger *events.Logger) tea.Cmd {
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

// readCmd marks a mail item as read in the underlying store. For a grouped
// mail thread, every pending message in the thread is marked read since
// opening the thread detail view surfaces all of them at once.
// Returns nil for escalation items (read state is mail-only).
func readCmd(src DataSource, item InboxItem) tea.Cmd {
	if item.Type != ItemMail {
		return nil
	}
	return func() tea.Msg {
		var err error
		if item.ThreadID != "" {
			for _, m := range item.ThreadMessages {
				if _, e := src.ReadMessage(m.ID); e != nil && err == nil {
					err = e
				}
			}
		} else {
			_, err = src.ReadMessage(item.ID)
		}
		return actionResultMsg{itemID: item.ID, action: "read", err: err}
	}
}

// dismissCmd dismisses a mail item — a standalone message, or every
// pending message in a grouped thread — from the inbox. Callers must only
// invoke this for ItemMail selections (the footer only advertises
// [d]ismiss when mail is selected), so no item-type guard is needed here.
// Sets delivery='dismissed' so the message(s) no longer appear in the
// inbox. Dismissed mail is not surfaced by any listing command — it sits
// until purged via "sol mail purge --dismissed".
func dismissCmd(src DataSource, item InboxItem) tea.Cmd {
	return func() tea.Msg {
		var err error
		if item.ThreadID != "" {
			err = dismissAllMessages(src, item.ThreadMessages)
		} else {
			err = src.DismissMessage(item.ID)
		}
		return actionResultMsg{itemID: item.ID, action: "dismiss", err: err}
	}
}

// ackAllMessages acks every message in msgs, returning the first error
// encountered (if any) after attempting all of them.
func ackAllMessages(src DataSource, msgs []store.Message) error {
	var firstErr error
	for _, m := range msgs {
		if err := src.AckMessage(m.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// dismissAllMessages dismisses every message in msgs, returning the first
// error encountered (if any) after attempting all of them.
func dismissAllMessages(src DataSource, msgs []store.Message) error {
	var firstErr error
	for _, m := range msgs {
		if err := src.DismissMessage(m.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
