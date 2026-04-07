package dash

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nevinsm/sol/internal/events"
)

// feedFadeLevels is the number of brightness levels for new-event highlights.
const feedFadeLevels = 4

// feedFadeLevelDuration is how long each fade level persists before decaying.
const feedFadeLevelDuration = 375 * time.Millisecond

// feedModel manages the activity feed panel at the bottom of the dashboard.
type feedModel struct {
	solHome string
	world   string // non-empty in world view — filters events to this world
	source  string // non-empty to filter events by source (e.g., "forge", "sentinel")

	events    []events.Event
	lastSeen  time.Time
	feedLines int // display height (5-8 lines depending on terminal)

	// Highlight animation state.
	newCount  int       // number of "new" events (counting from end of slice)
	fadeStart time.Time // when the current fade cycle began
}

func newFeedModel(solHome, world string) feedModel {
	return feedModel{
		solHome:   solHome,
		world:     world,
		feedLines: 6,
	}
}

// newFeedModelWithSource creates a feed model filtered to a specific source.
func newFeedModelWithSource(solHome, world, source string) feedModel {
	return feedModel{
		solHome:   solHome,
		world:     world,
		source:    source,
		feedLines: 6,
	}
}

// loadInitial loads the last 10 events from the curated feed.
func (fm *feedModel) loadInitial() {
	reader := events.NewReader(fm.solHome, true)
	opts := events.ReadOpts{Limit: 10}
	evts, err := reader.Read(opts)
	if err != nil {
		return // best-effort
	}
	fm.events = fm.filterWorld(evts)
	if len(fm.events) > 0 {
		fm.lastSeen = fm.events[len(fm.events)-1].Timestamp
	}
}

// refresh checks for new events since the last seen timestamp.
func (fm *feedModel) refresh() {
	reader := events.NewReader(fm.solHome, true)
	opts := events.ReadOpts{Limit: 10}
	if !fm.lastSeen.IsZero() {
		// Add a nanosecond to avoid re-reading the last seen event.
		opts.Since = fm.lastSeen.Add(time.Nanosecond)
	}
	newEvts, err := reader.Read(opts)
	if err != nil {
		return // best-effort
	}
	newEvts = fm.filterWorld(newEvts)
	if len(newEvts) == 0 {
		return
	}

	fm.events = append(fm.events, newEvts...)
	// Keep at most 20 events in memory.
	if len(fm.events) > 20 {
		fm.events = fm.events[len(fm.events)-20:]
	}
	fm.lastSeen = fm.events[len(fm.events)-1].Timestamp

	// Mark new events for highlight animation.
	fm.newCount += len(newEvts)
	fm.fadeStart = time.Now()
}

// filterWorld filters events to the current world when in world view.
func (fm *feedModel) filterWorld(evts []events.Event) []events.Event {
	if fm.world == "" && fm.source == "" {
		return evts // sphere view — show all
	}

	var filtered []events.Event
	for _, ev := range evts {
		if fm.world != "" && !eventMatchesWorld(ev, fm.world) {
			continue
		}
		if fm.source != "" && !eventMatchesSource(ev, fm.source) {
			continue
		}
		filtered = append(filtered, ev)
	}
	return filtered
}

// eventMatchesWorld checks if an event relates to the given world.
func eventMatchesWorld(ev events.Event, world string) bool {
	// Check Source field (e.g., "worldname/sentinel", "worldname/forge").
	if strings.HasPrefix(ev.Source, world+"/") || ev.Source == world {
		return true
	}

	// Check payload for a "world" key.
	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		return false
	}
	if w, ok := payload["world"]; ok {
		return fmt.Sprintf("%v", w) == world
	}
	return false
}

// eventMatchesSource checks if an event relates to the given source component.
// Matches against the Source field suffix (e.g., source "forge" matches
// "myworld/forge") and against the Actor field.
func eventMatchesSource(ev events.Event, source string) bool {
	// Check Source field suffix (e.g., "worldname/forge" matches "forge").
	if ev.Source == source || strings.HasSuffix(ev.Source, "/"+source) {
		return true
	}
	// Check Actor field.
	if ev.Actor == source {
		return true
	}
	return false
}

// setHeight adjusts the feed display height based on terminal height.
func (fm *feedModel) setHeight(termHeight int) {
	switch {
	case termHeight >= 50:
		fm.feedLines = 8
	case termHeight >= 40:
		fm.feedLines = 7
	case termHeight >= 30:
		fm.feedLines = 6
	default:
		fm.feedLines = 5
	}
}

// fadeLevel computes the current fade intensity from the time elapsed since fadeStart.
// Returns feedFadeLevels (brightest) immediately after new events, decaying to 0.
func (fm *feedModel) fadeLevel() int {
	if fm.newCount == 0 || fm.fadeStart.IsZero() {
		return 0
	}
	elapsed := time.Since(fm.fadeStart)
	level := feedFadeLevels - int(elapsed/feedFadeLevelDuration)
	if level < 0 {
		return 0
	}
	return level
}

// decayAnimation is called from the root model on animation ticks.
// When the fade has fully decayed, it clears newCount so events render normally.
func (fm *feedModel) decayAnimation() {
	if fm.newCount > 0 && fm.fadeLevel() == 0 {
		fm.newCount = 0
	}
}

// view renders the feed panel with separator.
func (fm feedModel) view(width int) string {
	var b strings.Builder

	// Separator line.
	sep := strings.Repeat("─", width)
	b.WriteString(dimStyle.Render(sep))
	b.WriteString("\n")

	if len(fm.events) == 0 {
		b.WriteString(dimStyle.Render("  No recent activity"))
		b.WriteString("\n")
		return b.String()
	}

	// Show events most-recent-first, up to feedLines.
	shown := fm.feedLines
	if shown > len(fm.events) {
		shown = len(fm.events)
	}

	level := fm.fadeLevel()
	highlightThreshold := len(fm.events) - fm.newCount // events at or after this index are "new"

	for i := len(fm.events) - 1; i >= len(fm.events)-shown; i-- {
		line := formatEvent(fm.events[i], width)
		if fm.newCount > 0 && i >= highlightThreshold && level > 0 {
			b.WriteString(feedHighlightAtLevel(level).Render(line))
		} else {
			b.WriteString(dimStyle.Render(line))
		}
		b.WriteString("\n")
	}

	return b.String()
}

// formatEvent formats a single event as a compact one-line display string.
func formatEvent(ev events.Event, maxWidth int) string {
	ts := ev.Timestamp.Local().Format("15:04")
	verb := eventVerb(ev.Type)
	detail := eventDetail(ev)

	line := fmt.Sprintf("  %s  %s %s", ts, ev.Actor, verb)
	if detail != "" {
		line += " " + detail
	}

	// Truncate if too long for terminal. Operate on rune boundaries so we
	// never split a multi-byte UTF-8 sequence (writ titles, persona names,
	// and event details may contain emoji or non-ASCII characters).
	if maxWidth > 0 && len(line) > maxWidth {
		line = truncateRunes(line, maxWidth)
	}

	return line
}

// truncateRunes truncates s so that its byte length is at most maxBytes,
// cutting only at rune boundaries. If truncation occurs and there is room,
// "..." is appended as a visual indicator. Never returns invalid UTF-8.
func truncateRunes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	const ellipsis = "..."
	if maxBytes <= len(ellipsis) {
		// No room for ellipsis — just take whole runes up to the budget.
		var n int
		for i := range s {
			if i > maxBytes {
				break
			}
			n = i
		}
		return s[:n]
	}
	budget := maxBytes - len(ellipsis)
	var end int
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		if i+size > budget {
			break
		}
		i += size
		end = i
	}
	return s[:end] + ellipsis
}

// eventVerb maps event types to human-readable past-tense verbs.
func eventVerb(eventType string) string {
	switch eventType {
	case events.EventCast:
		return "dispatched"
	case events.EventResolve:
		return "resolved"
	case events.EventMerged:
		return "merged"
	case events.EventMergeFailed:
		return "merge failed"
	case events.EventMergeQueued:
		return "queued merge"
	case events.EventMergeClaimed:
		return "claimed merge"
	case events.EventRespawn:
		return "respawned"
	case events.EventStalled:
		return "stalled"
	case events.EventEscalationCreated:
		return "escalated"
	case events.EventEscalationAcked:
		return "acknowledged escalation"
	case events.EventEscalationResolved:
		return "resolved escalation"
	case events.EventHandoff:
		return "handed off"
	case events.EventDegraded:
		return "entered degraded mode"
	case events.EventRecovered:
		return "recovered"
	case events.EventMassDeath:
		return "detected mass death"
	case events.EventPatrol:
		return "patrolled"
	case events.EventConsulPatrol:
		return "consul patrolled"
	case events.EventSessionStart:
		return "started session"
	case events.EventSessionStop:
		return "stopped session"
	case events.EventCaravanCreated:
		return "created caravan"
	case events.EventCaravanLaunched:
		return "launched caravan"
	case events.EventCaravanClosed:
		return "closed caravan"
	case events.EventRecast:
		return "recast"
	case events.EventReap:
		return "reaped"
	case "cast_batch":
		return "dispatched batch"
	case "respawn_batch":
		return "respawned batch"
	default:
		return eventType
	}
}

// eventDetail extracts a compact target/context string from the event payload.
func eventDetail(ev events.Event) string {
	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		return ""
	}

	get := func(key string) string {
		if v, ok := payload[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}

	switch ev.Type {
	case events.EventCast:
		writID := get("writ_id")
		world := get("world")
		if writID != "" && world != "" {
			return fmt.Sprintf("%s (%s)", writID, world)
		}
		return writID
	case events.EventResolve:
		return get("writ_id")
	case events.EventMerged, events.EventMergeFailed, events.EventMergeClaimed:
		mrID := get("merge_request_id")
		world := get("world")
		if mrID != "" && world != "" {
			return fmt.Sprintf("MR %s (%s)", mrID, world)
		}
		return mrID
	case events.EventRespawn:
		agent := get("agent")
		world := get("world")
		if agent != "" && world != "" {
			return fmt.Sprintf("%s (%s)", agent, world)
		}
		return agent
	case events.EventStalled:
		return get("agent")
	case events.EventEscalationCreated:
		return get("description")
	case events.EventHandoff:
		return get("writ_id")
	case events.EventCaravanCreated, events.EventCaravanLaunched, events.EventCaravanClosed:
		return get("name")
	case "cast_batch":
		return fmt.Sprintf("%s dispatches (%s)", get("count"), get("world"))
	case "respawn_batch":
		return fmt.Sprintf("%s respawns (%s)", get("count"), get("world"))
	default:
		// Fall back to a compact JSON of the payload.
		if len(payload) > 0 {
			data, err := json.Marshal(payload)
			if err == nil && len(data) < 60 {
				return string(data)
			}
		}
		return ""
	}
}
