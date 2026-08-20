package inbox

import (
	"sort"

	"github.com/nevinsm/sol/internal/store"
)

// groupThreads collapses mail items that share a non-empty message
// thread_id into a single representative row, so a multi-message thread
// renders as one row in the TUI instead of cluttering the list with one
// row per message. Escalations and standalone mail (empty ThreadID) pass
// through unchanged.
//
// items must already be in display order (as FetchItems returns them —
// priority ASC, then created_at ASC). Grouping preserves the position of
// each thread's first-encountered message so that ordering survives.
//
// This is purely a TUI presentation transform: it operates on the
// []InboxItem already produced by FetchItems and is never applied to the
// --json path, so scripting consumers keep seeing one row per message.
func groupThreads(items []InboxItem) []InboxItem {
	out := make([]InboxItem, 0, len(items))
	firstIdx := make(map[string]int)
	msgsByThread := make(map[string][]store.Message)

	for _, it := range items {
		if it.Type != ItemMail || it.Message == nil || it.Message.ThreadID == "" {
			out = append(out, it)
			continue
		}
		tid := it.Message.ThreadID
		msgsByThread[tid] = append(msgsByThread[tid], *it.Message)
		if _, exists := firstIdx[tid]; exists {
			// A later message in a thread already represented earlier in
			// the list — fold it into that row instead of adding a new one.
			continue
		}
		firstIdx[tid] = len(out)
		// Placeholder; replaced below once every message in the thread has
		// been collected, so the representative row reflects the full group.
		out = append(out, it)
	}

	for tid, idx := range firstIdx {
		msgs := msgsByThread[tid]
		sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].CreatedAt.Before(msgs[j].CreatedAt) })

		newest := &msgs[len(msgs)-1]
		minPriority := msgs[0].Priority
		for _, m := range msgs[1:] {
			if m.Priority < minPriority {
				minPriority = m.Priority
			}
		}

		out[idx] = InboxItem{
			ID:             tid,
			Type:           ItemMail,
			Priority:       minPriority,
			Source:         newest.Sender,
			Description:    newest.Subject,
			CreatedAt:      newest.CreatedAt,
			Message:        newest,
			ThreadID:       tid,
			ThreadMessages: msgs,
		}
	}
	return out
}

// sectionOrder stably partitions items into escalations-then-mail order for
// sectioned TUI display. FetchItems sorts globally by priority ASC then
// created_at ASC across BOTH types (verified by TestFetchItemsSortsByPriorityThenDate),
// so a P1 message can sort ahead of a P4 escalation in FetchItems's own
// output — the two types are genuinely interleaved, not already segregated.
// buildListRows renders "Escalations" and "Mail" as two blocks, and the
// model's cursor moves by simple ±1 over item index, so the display order
// itself must be escalations-block-then-mail-block for cursor navigation
// to visually track "next item in the section" rather than jumping between
// sections at arbitrary priority boundaries.
//
// Because the input is already sorted by (priority, created_at), a stable
// filter into two type-only passes preserves that ordering within each
// resulting section — satisfying "priority ASC then created_at ASC within
// each section" without re-sorting.
func sectionOrder(items []InboxItem) []InboxItem {
	out := make([]InboxItem, 0, len(items))
	for _, it := range items {
		if it.Type == ItemEscalation {
			out = append(out, it)
		}
	}
	for _, it := range items {
		if it.Type != ItemEscalation {
			out = append(out, it)
		}
	}
	return out
}

// findItemByID locates an item by ID in items, for re-locating a pinned
// detail-view item after a refresh. Returns ok=false if no item matches
// (the item was acked/resolved/dismissed elsewhere, or a thread's last
// pending message cleared and the thread row disappeared).
func findItemByID(items []InboxItem, id string) (InboxItem, bool) {
	if id == "" {
		return InboxItem{}, false
	}
	for _, it := range items {
		if it.ID == id {
			return it, true
		}
	}
	return InboxItem{}, false
}
