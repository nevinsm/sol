package dash

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nevinsm/sol/internal/eventformat"
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
	offset    int64 // byte offset into the curated feed, for events.Reader.ReadFrom
	feedLines int   // display height (5-8 lines depending on terminal)

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

// loadInitial loads the last 10 events from the curated feed and records the
// byte offset reached, so the following refresh() calls can read
// incrementally instead of rescanning the whole file (see events.ReadFrom).
func (fm *feedModel) loadInitial() {
	reader := events.NewReader(fm.solHome, true)
	opts := events.ReadOpts{Limit: 10}
	evts, offset, _, err := reader.ReadFrom(0, opts)
	if err != nil {
		return // best-effort
	}
	fm.offset = offset
	fm.events = fm.filterWorld(evts)
	if len(fm.events) > 0 {
		fm.lastSeen = fm.events[len(fm.events)-1].Timestamp
	}
}

// refresh reads events appended to the curated feed since the last recorded
// offset. This is an O(new bytes) read via events.Reader.ReadFrom, not the
// O(file) full rescan Read() does — see sol-7d76ff96a749ddb1.
//
// Because bytes are consumed exactly once as the offset advances, the
// same-timestamp boundary-dedup problem that a Since-based read has (two
// events sharing lastSeen's exact timestamp: one already shown, one not) does
// not arise here in the steady state, so no Since filtering happens on this
// path. It only reappears on the rotation/truncation fallback path below,
// where ReadFrom had to re-read the whole file from the start because the
// offset was invalidated (see ReadFrom's doc comment) — there, events already
// displayed can resurface and are boundary-filtered by timestamp. That
// filter can still miss a genuinely-new event sharing lastSeen's exact
// timestamp; tightening it is out of scope here (sol-e63920b5d6a6e2fd) since
// it only matters on this rare fallback path now, not on every tick.
func (fm *feedModel) refresh() {
	reader := events.NewReader(fm.solHome, true)
	opts := events.ReadOpts{Limit: 10}
	newEvts, offset, rotated, err := reader.ReadFrom(fm.offset, opts)
	if err != nil {
		return // best-effort
	}
	fm.offset = offset
	if rotated && !fm.lastSeen.IsZero() {
		newEvts = filterEventsAfter(newEvts, fm.lastSeen)
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

// filterEventsAfter returns only events strictly after t. Used solely on
// refresh's rotation-fallback path, where a full re-read from the start of
// the feed may include events already displayed.
func filterEventsAfter(evts []events.Event, t time.Time) []events.Event {
	var out []events.Event
	for _, ev := range evts {
		if ev.Timestamp.After(t) {
			out = append(out, ev)
		}
	}
	return out
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
	verb := eventformat.Verb(ev.Type)
	detail := eventformat.Detail(ev)

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
