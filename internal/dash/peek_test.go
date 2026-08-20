package dash

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/nevinsm/sol/internal/session"
)

// TestTruncateCaptureLinePlain covers the no-escape-sequences case: plain
// rune truncation should still just work, matching the old style.TruncateRunes
// behavior for width but with x/ansi doing the measuring.
func TestTruncateCaptureLinePlain(t *testing.T) {
	t.Parallel()

	s := "the quick brown fox jumps over the lazy dog"
	got := truncateCaptureLine(s, 10)

	if strings.Contains(got, "\x1b") {
		t.Errorf("plain input should not gain escape sequences, got: %q", got)
	}
	if w := ansi.StringWidth(got); w > 10 {
		t.Errorf("truncated width = %d, want <= 10 (got %q)", w, got)
	}
	if got != "the quick " {
		t.Errorf("truncateCaptureLine(%q, 10) = %q, want %q", s, got, "the quick ")
	}
}

// TestTruncateCaptureLineZeroWidth ensures a non-positive width degrades to
// an empty string rather than panicking or underflowing.
func TestTruncateCaptureLineZeroWidth(t *testing.T) {
	t.Parallel()

	if got := truncateCaptureLine("hello", 0); got != "" {
		t.Errorf("truncateCaptureLine with maxWidth=0 = %q, want empty", got)
	}
	if got := truncateCaptureLine("hello", -3); got != "" {
		t.Errorf("truncateCaptureLine with negative maxWidth = %q, want empty", got)
	}
}

// TestTruncateCaptureLineNoTruncationNeeded covers a colored line that
// already fits within the width budget: content must be preserved and,
// since it carries escapes, a trailing reset must be appended so the color
// can't bleed into whatever renders after it.
func TestTruncateCaptureLineNoTruncationNeeded(t *testing.T) {
	t.Parallel()

	s := "\x1b[31mred\x1b[0m"
	got := truncateCaptureLine(s, 20)

	if !strings.HasSuffix(got, ansi.ResetStyle) {
		t.Errorf("expected trailing reset on a line with escapes, got: %q", got)
	}
	if !strings.Contains(got, "red") {
		t.Errorf("expected visible content preserved, got: %q", got)
	}
}

// TestTruncateCaptureLineEscapeStraddlesBoundary is the core acceptance-
// criteria test: a synthetic line with a color escape sequence mid-line,
// truncated at widths that land before, inside, and after the colored
// segment. In every case the result must be valid (parseable — Strip must
// not choke, and re-stripping after appending more text must not be
// affected by dangling state) and never exceed the requested visible width.
func TestTruncateCaptureLineEscapeStraddlesBoundary(t *testing.T) {
	t.Parallel()

	// "abcde" (width 5, plain) + colored "FGHIJ" (width 5) + explicit reset.
	const prefix = "abcde"
	const colored = "FGHIJ"
	s := prefix + "\x1b[31m" + colored + "\x1b[0m"

	tests := []struct {
		name       string
		maxWidth   int
		wantStrip  string // ansi.Strip(got) should equal this
	}{
		{"cut well before the escape sequence", 3, "abc"},
		{"cut exactly at the escape boundary", 5, "abcde"},
		{"cut inside the colored segment", 8, "abcdeFGH"},
		{"cut exactly at end of colored segment", 10, "abcdeFGHIJ"},
		{"no truncation needed at all", 20, "abcdeFGHIJ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := truncateCaptureLine(s, tt.maxWidth)

			if w := ansi.StringWidth(got); w > tt.maxWidth {
				t.Errorf("truncateCaptureLine(%q, %d) visible width = %d, want <= %d (got %q)",
					s, tt.maxWidth, w, tt.maxWidth, got)
			}

			if stripped := ansi.Strip(got); stripped != tt.wantStrip {
				t.Errorf("truncateCaptureLine(%q, %d) visible content = %q, want %q (raw: %q)",
					s, tt.maxWidth, stripped, tt.wantStrip, got)
			}

			// Validity check: the escape state left behind by got must not
			// bleed into subsequent content. Appending plain text and
			// stripping the combined string should yield exactly the
			// visible content of got followed by that plain text — if got
			// contained a broken/incomplete escape sequence, Strip would
			// either choke or swallow/mangle the suffix.
			suffix := "SENTINEL"
			combinedStripped := ansi.Strip(got + suffix)
			wantCombined := tt.wantStrip + suffix
			if combinedStripped != wantCombined {
				t.Errorf("truncateCaptureLine(%q, %d) leaves invalid trailing escape state: Strip(got+suffix) = %q, want %q",
					s, tt.maxWidth, combinedStripped, wantCombined)
			}

			if !strings.HasSuffix(got, ansi.ResetStyle) {
				t.Errorf("truncateCaptureLine(%q, %d) missing trailing reset, got: %q", s, tt.maxWidth, got)
			}
		})
	}
}

// TestPadRightNoCacheBypassesWidthCache verifies padRightNoCache measures
// width directly rather than trusting (possibly stale) widthCache entries —
// the whole point of introducing it for high-churn capture lines.
func TestPadRightNoCacheBypassesWidthCache(t *testing.T) {
	// Not t.Parallel(): mutates the package-level widthCache.
	s := "\x1b[32mxx\x1b[0m" // visible width 2

	orig := widthCache
	widthCache = make(map[string]int)
	t.Cleanup(func() { widthCache = orig })

	// Poison the cache with a wrong width for this exact string.
	widthCache[s] = 999

	// padRight trusts the (poisoned) cache and does nothing, since 999 >= 5.
	if got := padRight(s, 5); got != s {
		t.Fatalf("sanity check failed: expected padRight to trust the poisoned cache and skip padding, got: %q", got)
	}

	// padRightNoCache must ignore the poisoned cache and pad based on the
	// real visible width (2), reaching the requested width of 5.
	got := padRightNoCache(s, 5)
	if w := ansi.StringWidth(got); w != 5 {
		t.Errorf("padRightNoCache(%q, 5) visible width = %d, want 5 (got %q)", s, w, got)
	}
	if !strings.HasPrefix(got, s) {
		t.Errorf("padRightNoCache(%q, 5) = %q, want it to start with the original string", s, got)
	}
}

// TestSelectedIsCapturing checks the right-panel classification used to
// decide whether to bypass widthCache: only a peekable, alive, non-caravan
// item is rendering live capture content.
func TestSelectedIsCapturing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		item peekItem
		want bool
	}{
		{"peekable and alive", peekItem{peekable: true, alive: true}, true},
		{"peekable but not alive", peekItem{peekable: true, alive: false}, false},
		{"alive but not peekable", peekItem{peekable: false, alive: true}, false},
		{"caravan item never counts as capturing", peekItem{peekable: true, alive: true, isCaravan: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pm := peekModel{items: []peekItem{tt.item}, cursor: 0}
			if got := pm.selectedIsCapturing(); got != tt.want {
				t.Errorf("selectedIsCapturing() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("cursor out of range", func(t *testing.T) {
		t.Parallel()
		pm := peekModel{items: nil, cursor: 0}
		if pm.selectedIsCapturing() {
			t.Error("selectedIsCapturing() with no items should be false")
		}
	})
}

// capturingPeekModel returns a peekModel with a single peekable, alive item
// selected — the "live capture panel" state scrollCapture and the scroll
// keys are gated on.
func capturingPeekModel() peekModel {
	return peekModel{
		items:      []peekItem{{name: "agent1", sessionName: "sol-x-agent1", peekable: true, alive: true}},
		cursor:     0,
		width:      60,
		height:     20,
		listWidth:  defaultListWidth,
		sessionMgr: &session.Manager{},
	}
}

// TestScrollCaptureEntersScrollbackAndClampsAtZero covers the offset
// bookkeeping in scrollCapture: crossing from live-follow into scrollback
// fires an immediate deep capture (so there's history to scroll into ahead
// of the next tick), staying within scrollback does not re-fire on every
// key press, and the offset never goes negative.
func TestScrollCaptureEntersScrollbackAndClampsAtZero(t *testing.T) {
	t.Parallel()

	pm := capturingPeekModel()
	if pm.captureScroll != 0 {
		t.Fatalf("initial captureScroll = %d, want 0 (live-follow)", pm.captureScroll)
	}

	// First scroll-up crosses from live into scrollback: expect an
	// immediate capture command.
	pm, cmd := pm.scrollCapture(capturePageStep)
	if pm.captureScroll != capturePageStep {
		t.Errorf("captureScroll after first scroll-up = %d, want %d", pm.captureScroll, capturePageStep)
	}
	if cmd == nil {
		t.Error("scrollCapture entering scrollback should return an immediate capture command, got nil")
	}

	// Staying in scrollback: offset keeps moving, but no extra immediate
	// capture is needed since captureCmd() already uses the deep window
	// while captureScroll > 0.
	pm, cmd = pm.scrollCapture(capturePageStep)
	if want := 2 * capturePageStep; pm.captureScroll != want {
		t.Errorf("captureScroll after second scroll-up = %d, want %d", pm.captureScroll, want)
	}
	if cmd != nil {
		t.Error("scrollCapture while already scrolled should not return an immediate capture command")
	}

	// Scrolling down past zero clamps at zero (live tail), not negative.
	pm, cmd = pm.scrollCapture(-1000)
	if pm.captureScroll != 0 {
		t.Errorf("captureScroll after large scroll-down = %d, want 0 (clamped)", pm.captureScroll)
	}
	if cmd != nil {
		t.Error("scrollCapture returning to the tail (not from live) should not return an immediate capture command")
	}
}

// TestPeekUpdateScrollKeysGatedOnCapturing verifies pgup/pgdn and
// ctrl+u/ctrl+d only move the capture scroll offset when the selected
// item's right panel is actually rendering live capture content — for a
// non-capturing item (e.g. no active session) the keys must be a no-op
// rather than silently accumulating an offset nothing will ever render.
func TestPeekUpdateScrollKeysGatedOnCapturing(t *testing.T) {
	t.Parallel()

	t.Run("non-capturing item ignores scroll keys", func(t *testing.T) {
		t.Parallel()
		pm := peekModel{items: []peekItem{{name: "dead", peekable: false, alive: false}}, cursor: 0}
		pm, cmd := pm.update(tea.KeyMsg{Type: tea.KeyPgUp})
		if pm.captureScroll != 0 {
			t.Errorf("captureScroll = %d after pgup on non-capturing item, want 0", pm.captureScroll)
		}
		if cmd != nil {
			t.Error("pgup on non-capturing item should not return a command")
		}
	})

	t.Run("capturing item responds to pgup/pgdn and ctrl+u/ctrl+d", func(t *testing.T) {
		t.Parallel()
		pm := capturingPeekModel()

		pm, cmd := pm.update(tea.KeyMsg{Type: tea.KeyPgUp})
		if pm.captureScroll != capturePageStep {
			t.Fatalf("captureScroll after pgup = %d, want %d", pm.captureScroll, capturePageStep)
		}
		if cmd == nil {
			t.Error("pgup entering scrollback should return an immediate capture command")
		}

		pm, _ = pm.update(tea.KeyMsg{Type: tea.KeyCtrlD})
		if want := capturePageStep - captureHalfStep; pm.captureScroll != want {
			t.Errorf("captureScroll after ctrl+d = %d, want %d", pm.captureScroll, want)
		}

		pm, _ = pm.update(tea.KeyMsg{Type: tea.KeyCtrlU})
		if want := capturePageStep - captureHalfStep + captureHalfStep; pm.captureScroll != want {
			t.Errorf("captureScroll after ctrl+u = %d, want %d", pm.captureScroll, want)
		}

		pm, _ = pm.update(tea.KeyMsg{Type: tea.KeyPgDown})
		if pm.captureScroll != 0 {
			t.Errorf("captureScroll after pgdown = %d, want 0 (back to live tail)", pm.captureScroll)
		}
	})
}

// TestRenderCaptureScrollWindow is the core scrollback acceptance test: with
// a capture buffer deeper than the visible panel, captureScroll == 0 shows
// the tail (live-follow, matching prior tail-only behavior), a positive
// offset shows an older window without disturbing what was captured, the
// offset is reported via a "scrolled: N above live tail" indicator, and an
// offset beyond the available history clamps rather than underflowing the
// slice.
func TestRenderCaptureScrollWindow(t *testing.T) {
	t.Parallel()

	const total = 20
	lines := make([]string, total)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i)
	}
	capture := strings.Join(lines, "\n")

	pm := capturingPeekModel()
	pm.capture = capture

	const maxHeight = 6 // header + 5 content lines
	const availHeight = maxHeight - 1

	visible := func(rendered []string) []string {
		out := make([]string, len(rendered))
		for i, l := range rendered {
			out[i] = strings.TrimPrefix(ansi.Strip(l), " ")
		}
		return out
	}

	t.Run("live-follow shows the tail", func(t *testing.T) {
		pm := pm
		pm.captureScroll = 0
		out := visible(pm.renderCapture(maxHeight))
		want := []string{"line15", "line16", "line17", "line18", "line19"}
		got := out[1:] // skip header
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("live-follow content = %v, want %v", got, want)
		}
		if strings.Contains(out[0], "scrolled") {
			t.Errorf("header should not show a scroll indicator at offset 0, got %q", out[0])
		}
	})

	t.Run("scrolled offset shifts the window and shows the indicator", func(t *testing.T) {
		pm := pm
		pm.captureScroll = 3
		out := visible(pm.renderCapture(maxHeight))
		want := []string{"line12", "line13", "line14", "line15", "line16"}
		got := out[1:]
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("scrolled content = %v, want %v", got, want)
		}
		if !strings.Contains(out[0], "scrolled: 3 above live tail") {
			t.Errorf("header should show scroll indicator, got %q", out[0])
		}
	})

	t.Run("offset beyond history clamps to the oldest window", func(t *testing.T) {
		pm := pm
		pm.captureScroll = 1000
		out := visible(pm.renderCapture(maxHeight))
		want := []string{"line0", "line1", "line2", "line3", "line4"}
		got := out[1:]
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("clamped content = %v, want %v", got, want)
		}
		maxOffset := total - availHeight
		if !strings.Contains(out[0], fmt.Sprintf("scrolled: %d above live tail", maxOffset)) {
			t.Errorf("header should report the clamped offset %d, got %q", maxOffset, out[0])
		}
	})
}
