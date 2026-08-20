package dash

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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
