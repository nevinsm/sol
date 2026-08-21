package dash

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/events"
)

// feedNavMsg is emitted when enter is pressed on the focused feed's selected
// event, carrying enough information for Model to route to the right
// destination — world view with a merge-queue/agent row focused, or a
// caravan peek. This is a deliberately closed set of links (see the writ
// description) rather than a general navigation framework: only the event
// types handled in navTarget below produce one.
type feedNavMsg struct {
	kind string // "mr", "agent", or "caravan"

	// world is the resolved target world. It comes from the event's own
	// payload when present, else falls back to the feed's own filter
	// context (fm.world — set in world view, empty in sphere view). It can
	// still end up empty: several event types never carry a "world" key in
	// their payload today (see the doc comment on navTarget) — that's a
	// genuine emission-side gap, not something this code should guess at.
	world string

	mrID      string
	agentName string
	caravanID string
}

// eventPayloadString reads a single string-shaped key out of an event's
// payload map, returning "" if the payload isn't a map or the key is
// absent. This mirrors the extraction eventformat.Detail does internally,
// but is kept separate from that package on purpose: Detail's job is
// formatted display text, not raw identifiers for lookups, and duplicating
// its per-type switch here would recreate the exact drift eventformat was
// built to kill (see internal/eventformat's doc comment). We only need a
// handful of raw keys, read directly.
func eventPayloadString(ev events.Event, key string) string {
	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := payload[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// navTarget computes the cross-panel navigation target for ev, or ok=false
// for event types outside the closed link set this writ implements.
//
// Payload completeness (verified against the actual Emit call sites, not
// guessed):
//   - EventCast and EventMergeQueued always carry "world".
//   - EventMergeClaimed, EventMerged, EventMergeFailed, EventResolve,
//     EventRespawn, and EventStalled do NOT carry "world" at any of their
//     current emission sites — only enough to identify the MR/agent, not
//     which world it belongs to. When ev's payload lacks "world", we fall
//     back to fromWorld (the feed's own filter context: the active world in
//     world view, "" in sphere view). In sphere view that leaves world ""
//     for those types, and the caller must degrade gracefully rather than
//     guess — see Model.handleFeedNav. This is exactly the kind of gap the
//     writ asks to be reported rather than patched inline: a small
//     follow-up to have forge/dispatch/sentinel stamp "world" onto these
//     payloads would close it at the source.
//   - All three caravan event types (created/launched/closed) always carry
//     "caravan_id", so the caravan link never has this problem.
func navTarget(ev events.Event, fromWorld string) (feedNavMsg, bool) {
	world := eventPayloadString(ev, "world")
	if world == "" {
		world = fromWorld
	}

	switch ev.Type {
	case events.EventMergeQueued, events.EventMergeClaimed, events.EventMerged, events.EventMergeFailed:
		return feedNavMsg{kind: "mr", world: world, mrID: eventPayloadString(ev, "merge_request_id")}, true
	case events.EventCast, events.EventResolve, events.EventRespawn, events.EventStalled:
		return feedNavMsg{kind: "agent", world: world, agentName: eventPayloadString(ev, "agent")}, true
	case events.EventCaravanCreated, events.EventCaravanLaunched, events.EventCaravanClosed:
		return feedNavMsg{kind: "caravan", world: world, caravanID: eventPayloadString(ev, "caravan_id")}, true
	default:
		return feedNavMsg{}, false
	}
}

// navigateCmd returns a tea.Cmd that emits a feedNavMsg for the currently
// selected event, or nil when there's no selection or the event type isn't
// part of the closed link set (a silent no-op, not an error — most event
// types simply aren't linkable).
func (fm feedModel) navigateCmd() tea.Cmd {
	ev, ok := fm.selectedEvent()
	if !ok {
		return nil
	}
	target, ok := navTarget(ev, fm.world)
	if !ok {
		return nil
	}
	return func() tea.Msg { return target }
}
