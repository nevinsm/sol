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

// resetFeedFlags restores package-level feed flag state between tests.
func resetFeedFlags() {
	feedFollow = false
	feedLimit = 20
	feedSince = ""
	feedType = ""
	feedJSON = false
	feedRaw = false
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
