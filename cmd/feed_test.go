package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clievents "github.com/nevinsm/sol/internal/cliapi/events"
	"github.com/nevinsm/sol/internal/events"
)

// resetFeedFlags restores package-level feed flag state between tests. It
// also clears pflag's Changed("since") tracking: feedCmd's flags are bound
// once at init() and reused for the whole test binary, so once any test
// parses --since (even "--since=''"), Changed("since") stays true forever
// after — cobra's Parse only ever flips it true, never back to false for a
// later parse that omits the flag. Without resetting it here, a later test
// that calls feedCmd.RunE (or rootCmd.Execute with no --since) can be
// silently routed down the --since=<cursor> bootstrap branch depending on
// what earlier test happened to run first.
func resetFeedFlags() {
	feedFollow = false
	feedLimit = 20
	feedSince = ""
	feedType = ""
	feedJSON = false
	feedRaw = false
	if f := feedCmd.Flags().Lookup("since"); f != nil {
		f.Changed = false
	}
}

// captureStdout is defined in cost_test.go and shared across cmd package tests.

func TestFeedCmd_CursorSinceRequiresJSON(t *testing.T) {
	resetFeedFlags()
	defer resetFeedFlags()
	t.Setenv("SOL_HOME", t.TempDir())

	feedSince = events.EncodeCursor(events.Cursor{ID: "deadbeef", UnixNano: 1})
	feedJSON = false

	err := feedCmd.RunE(feedCmd, nil)
	if err == nil {
		t.Fatal("expected an error when --since=<cursor> is used without --json")
	}
	if !strings.Contains(err.Error(), "requires --json") {
		t.Errorf("error = %q, want it to mention --json is required", err.Error())
	}
}

func TestFeedCmd_CursorSinceRejectsFollow(t *testing.T) {
	resetFeedFlags()
	defer resetFeedFlags()
	t.Setenv("SOL_HOME", t.TempDir())

	feedSince = events.EncodeCursor(events.Cursor{ID: "deadbeef", UnixNano: 1})
	feedJSON = true
	feedFollow = true

	err := feedCmd.RunE(feedCmd, nil)
	if err == nil {
		t.Fatal("expected an error when --since=<cursor> is combined with --follow")
	}
	if !strings.Contains(err.Error(), "--follow") {
		t.Errorf("error = %q, want it to mention --follow", err.Error())
	}
}

func TestFeedCmd_CursorSince_FreshThenIncremental(t *testing.T) {
	resetFeedFlags()
	defer resetFeedFlags()

	home := t.TempDir()
	t.Setenv("SOL_HOME", home)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEventsForCmdTest(t, home, []events.Event{
		{Timestamp: base, Source: "sol", Type: events.EventCast, Actor: "autarch", Visibility: "feed",
			Payload: map[string]any{"seq": 1}},
	})

	feedJSON = true
	feedSince = "" // fresh bootstrap

	out := captureStdout(t, func() {
		if err := feedCmd.RunE(feedCmd, nil); err != nil {
			t.Fatalf("RunE (bootstrap): %v", err)
		}
	})

	// Fresh (no --since) still uses the human/JSONL streaming path, not the
	// cursor envelope — one JSON object per line, no next_cursor.
	var single clievents.Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &single); err != nil {
		t.Fatalf("bootstrap output should be a single event JSON line: %v\noutput: %s", err, out)
	}

	// Now fetch a cursor via the events package directly (mirrors what a
	// consumer would do with the first --json --since="" cursor read) and
	// verify the incremental --json --since=<cursor> path.
	reader := events.NewReader(home, false)
	page, err := reader.ReadSince("", events.ReadOpts{})
	if err != nil {
		t.Fatalf("ReadSince bootstrap: %v", err)
	}
	if page.NextCursor == "" {
		t.Fatal("expected a non-empty cursor from a non-empty bootstrap read")
	}

	// No new events yet — incremental read should return an empty (never
	// null) events array and exit cleanly.
	feedSince = page.NextCursor
	out = captureStdout(t, func() {
		if err := feedCmd.RunE(feedCmd, nil); err != nil {
			t.Fatalf("RunE (empty increment): %v", err)
		}
	})

	var envelope struct {
		Events     []clievents.Event `json:"events"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &envelope); err != nil {
		t.Fatalf("cursor output should be a single JSON envelope: %v\noutput: %s", err, out)
	}
	if envelope.Events == nil {
		t.Error("events field should be an empty array, not null, on an empty increment")
	}
	if len(envelope.Events) != 0 {
		t.Errorf("got %d events, want 0 for an empty increment", len(envelope.Events))
	}
	if envelope.NextCursor != page.NextCursor {
		t.Errorf("next_cursor = %q, want unchanged %q", envelope.NextCursor, page.NextCursor)
	}

	// Append a new event and confirm it shows up on the next incremental read.
	writeRawEventsForCmdTest(t, home, []events.Event{
		{Timestamp: base, Source: "sol", Type: events.EventCast, Actor: "autarch", Visibility: "feed",
			Payload: map[string]any{"seq": 1}},
		{Timestamp: base.Add(time.Second), Source: "sol", Type: events.EventResolve, Actor: "autarch", Visibility: "feed",
			Payload: map[string]any{"seq": 2}},
	})

	out = captureStdout(t, func() {
		if err := feedCmd.RunE(feedCmd, nil); err != nil {
			t.Fatalf("RunE (incremental): %v", err)
		}
	})
	envelope.Events = nil
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &envelope); err != nil {
		t.Fatalf("unmarshal incremental envelope: %v\noutput: %s", err, out)
	}
	if len(envelope.Events) != 1 {
		t.Fatalf("got %d events, want 1 new event", len(envelope.Events))
	}
	if envelope.Events[0].Type != events.EventResolve {
		t.Errorf("unexpected event type %q", envelope.Events[0].Type)
	}
}

// TestFeedCmd_ExplicitEmptySinceBootstrapsCursor exercises the only
// CLI-reachable way to enter the cursor contract from cold start: an
// explicitly empty "--since=''" (as opposed to --since simply being left
// off, which stays on the plain JSONL path — see
// TestFeedCmd_OmittedSinceStaysPlainJSONL). Must go through
// rootCmd.SetArgs/Execute rather than assigning the feedSince package var
// directly, since only real flag parsing sets Cobra's Changed("since"),
// which is what distinguishes "omitted" from "explicitly empty".
func TestFeedCmd_ExplicitEmptySinceBootstrapsCursor(t *testing.T) {
	resetFeedFlags()
	defer resetFeedFlags()

	home := t.TempDir()
	t.Setenv("SOL_HOME", home)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEventsForCmdTest(t, home, []events.Event{
		{Timestamp: base, Source: "sol", Type: events.EventCast, Actor: "autarch", Visibility: "feed",
			Payload: map[string]any{"seq": 1}},
	})

	rootCmd.SetArgs([]string{"feed", "--json", "--since="})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute (bootstrap): %v", err)
		}
	})

	var envelope struct {
		Events     []clievents.Event `json:"events"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &envelope); err != nil {
		t.Fatalf("explicit-empty --since output should be a cursor envelope: %v\noutput: %s", err, out)
	}
	if len(envelope.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(envelope.Events))
	}
	if envelope.NextCursor == "" {
		t.Fatal("expected a non-empty next_cursor from the bootstrap read")
	}
	if !events.IsCursor(envelope.NextCursor) {
		t.Errorf("next_cursor %q does not look like an opaque cursor token", envelope.NextCursor)
	}

	// The returned cursor must actually work as a --since value on the next call.
	resetFeedFlags()
	rootCmd.SetArgs([]string{"feed", "--json", "--since=" + envelope.NextCursor})
	out = captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute (incremental): %v", err)
		}
	})
	var incremental struct {
		Events     []clievents.Event `json:"events"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &incremental); err != nil {
		t.Fatalf("incremental read should be a cursor envelope: %v\noutput: %s", err, out)
	}
	if len(incremental.Events) != 0 {
		t.Errorf("got %d events, want 0 for an empty increment", len(incremental.Events))
	}
}

// TestFeedCmd_OmittedSinceStaysPlainJSONL confirms that leaving --since off
// entirely (the common "sol feed --json" invocation) is unaffected by the
// cursor bootstrap added for explicit "--since=''" — it must keep returning
// one JSON line per event, not a cursor envelope.
func TestFeedCmd_OmittedSinceStaysPlainJSONL(t *testing.T) {
	resetFeedFlags()
	defer resetFeedFlags()

	home := t.TempDir()
	t.Setenv("SOL_HOME", home)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEventsForCmdTest(t, home, []events.Event{
		{Timestamp: base, Source: "sol", Type: events.EventCast, Actor: "autarch", Visibility: "feed",
			Payload: map[string]any{"seq": 1}},
	})

	rootCmd.SetArgs([]string{"feed", "--json"})
	out := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})

	var single clievents.Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &single); err != nil {
		t.Fatalf("omitted --since output should be a single event JSON line, not an envelope: %v\noutput: %s", err, out)
	}
}

func TestFeedCmd_InvalidCursorErrorTellsConsumerToRestart(t *testing.T) {
	resetFeedFlags()
	defer resetFeedFlags()
	t.Setenv("SOL_HOME", t.TempDir())

	feedJSON = true
	feedSince = events.EncodeCursor(events.Cursor{ID: "not-present-in-any-file", UnixNano: 1})

	err := feedCmd.RunE(feedCmd, nil)
	if err == nil {
		t.Fatal("expected an error for a cursor referencing an event that was never written")
	}
	if !strings.Contains(err.Error(), "fresh cursor") {
		t.Errorf("error = %q, want guidance to restart with a fresh cursor", err.Error())
	}
}

// TestFormatEventDescription_ComposesFromEventformat pins the human-format
// description column to eventformat.Verb + eventformat.Detail so a future
// edit to eventformat can't silently drift cmd/feed.go's rendering back out
// of sync with dash's — the exact bug class this package was created to
// fix. EventMailSent is the headline regression case from the writ: it was
// the one event type cmd/feed.go's OLD copy handled that dash's copy never
// did, so it must keep rendering correctly now that both share one mapping.
func TestFormatEventDescription_ComposesFromEventformat(t *testing.T) {
	tests := []struct {
		name string
		ev   events.Event
		want string
	}{
		{
			name: "cast",
			ev: events.Event{
				Type:    events.EventCast,
				Payload: map[string]any{"writ_id": "sol-abc123", "agent": "Nova", "world": "sol-dev"},
			},
			want: "dispatched sol-abc123 → Nova (sol-dev)",
		},
		{
			name: "mail sent",
			ev: events.Event{
				Type:    events.EventMailSent,
				Payload: map[string]any{"recipient": "autarch"},
			},
			want: "sent mail autarch",
		},
		{
			name: "degraded has no detail",
			ev: events.Event{
				Type:    events.EventDegraded,
				Payload: map[string]any{},
			},
			want: "entered degraded mode",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatEventDescription(tt.ev); got != tt.want {
				t.Errorf("formatEventDescription(%s) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// TestFeedCmd_JSONOutputUnaffectedByEventformat guards the ADR-0043
// scripting-surface contract: sol feed --json must never embed the human
// verb/detail presentation this writ introduced. The JSON envelope comes
// from clievents.FromEvent, which round-trips the raw event fields only —
// this test locks that down explicitly rather than relying on it being
// true by construction.
func TestFeedCmd_JSONOutputUnaffectedByEventformat(t *testing.T) {
	resetFeedFlags()
	defer resetFeedFlags()

	home := t.TempDir()
	t.Setenv("SOL_HOME", home)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEventsForCmdTest(t, home, []events.Event{
		{Timestamp: base, Source: "sol", Type: events.EventMailSent, Actor: "Nova", Visibility: "feed",
			Payload: map[string]any{"recipient": "autarch"}},
	})

	feedJSON = true
	out := captureStdout(t, func() {
		if err := feedCmd.RunE(feedCmd, nil); err != nil {
			t.Fatalf("RunE: %v", err)
		}
	})

	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
		t.Fatalf("unmarshal event line: %v\noutput: %s", err, out)
	}
	for _, forbidden := range []string{"verb", "detail", "description"} {
		if _, present := raw[forbidden]; present {
			t.Errorf("--json output has unexpected field %q — human presentation must not leak into the scripting surface: %v", forbidden, raw)
		}
	}
	if raw["payload"] == nil {
		t.Errorf("--json output missing payload: %v", raw)
	}
}

// writeRawEventsForCmdTest overwrites $SOL_HOME/.events.jsonl directly, one
// JSON line per event, bypassing Logger so tests get deterministic
// timestamps. feedCmd falls back to the raw feed automatically when
// .feed.jsonl (the curated feed) doesn't exist, so no curated feed setup is
// needed here.
func writeRawEventsForCmdTest(t *testing.T, home string, evts []events.Event) {
	t.Helper()
	path := filepath.Join(home, ".events.jsonl")
	var data []byte
	for _, ev := range evts {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write raw events file: %v", err)
	}
}
