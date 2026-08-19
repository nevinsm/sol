package events

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeRawEvents overwrites the raw feed file at dir/.events.jsonl with the
// given events, one JSON line per event — bypassing Logger so tests get
// deterministic, explicit timestamps instead of time.Now().
func writeRawEvents(t *testing.T, dir string, evts []Event) {
	t.Helper()
	path := filepath.Join(dir, ".events.jsonl")
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

func testEvent(seq int, base time.Time, evType string) Event {
	return Event{
		Timestamp:  base.Add(time.Duration(seq) * time.Second),
		Source:     "sol",
		Type:       evType,
		Actor:      "autarch",
		Visibility: "feed",
		Payload: map[string]any{
			"seq": seq,
		},
	}
}

func TestReadSince_FreshBootstrap(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	evts := []Event{
		testEvent(1, base, EventCast),
		testEvent(2, base, EventCast),
		testEvent(3, base, EventCast),
	}
	writeRawEvents(t, dir, evts)

	r := NewReader(dir, false)
	page, err := r.ReadSince("", ReadOpts{})
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(page.Events) != 3 {
		t.Fatalf("got %d events, want 3", len(page.Events))
	}
	if page.NextCursor == "" {
		t.Fatal("expected a non-empty next cursor after a non-empty bootstrap read")
	}
	if !IsCursor(page.NextCursor) {
		t.Errorf("next cursor %q does not look like a cursor token", page.NextCursor)
	}
}

func TestReadSince_FreshBootstrapEmptyFile(t *testing.T) {
	dir := t.TempDir()
	r := NewReader(dir, false)
	page, err := r.ReadSince("", ReadOpts{})
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(page.Events) != 0 {
		t.Fatalf("got %d events, want 0", len(page.Events))
	}
	if page.NextCursor != "" {
		t.Errorf("expected empty next cursor when nothing has ever been written, got %q", page.NextCursor)
	}
}

func TestReadSince_IncrementalReturnsOnlyNewEvents(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEvents(t, dir, []Event{
		testEvent(1, base, EventCast),
		testEvent(2, base, EventCast),
	})

	r := NewReader(dir, false)
	first, err := r.ReadSince("", ReadOpts{})
	if err != nil {
		t.Fatalf("bootstrap ReadSince: %v", err)
	}
	if len(first.Events) != 2 {
		t.Fatalf("bootstrap got %d events, want 2", len(first.Events))
	}

	// Append new events (simulating another process writing to the feed).
	writeRawEvents(t, dir, []Event{
		testEvent(1, base, EventCast),
		testEvent(2, base, EventCast),
		testEvent(3, base, EventResolve),
		testEvent(4, base, EventResolve),
	})

	second, err := r.ReadSince(first.NextCursor, ReadOpts{})
	if err != nil {
		t.Fatalf("incremental ReadSince: %v", err)
	}
	if len(second.Events) != 2 {
		t.Fatalf("incremental got %d events, want 2 (only the new ones)", len(second.Events))
	}
	for _, ev := range second.Events {
		if ev.Type != EventResolve {
			t.Errorf("unexpected event leaked into increment: %+v", ev)
		}
	}
	if second.NextCursor == first.NextCursor {
		t.Error("next cursor should have advanced past the new events")
	}
}

func TestReadSince_EmptyIncrement(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEvents(t, dir, []Event{
		testEvent(1, base, EventCast),
	})

	r := NewReader(dir, false)
	first, err := r.ReadSince("", ReadOpts{})
	if err != nil {
		t.Fatalf("bootstrap ReadSince: %v", err)
	}

	// No new events written — re-read with the same cursor.
	second, err := r.ReadSince(first.NextCursor, ReadOpts{})
	if err != nil {
		t.Fatalf("empty increment ReadSince: %v", err)
	}
	if len(second.Events) != 0 {
		t.Fatalf("got %d events, want 0 for an empty increment", len(second.Events))
	}
	if second.NextCursor != first.NextCursor {
		t.Errorf("cursor should be unchanged on an empty increment: got %q, want %q",
			second.NextCursor, first.NextCursor)
	}
}

func TestReadSince_InvalidCursorToken(t *testing.T) {
	dir := t.TempDir()
	writeRawEvents(t, dir, []Event{testEvent(1, time.Now().UTC(), EventCast)})

	r := NewReader(dir, false)
	_, err := r.ReadSince("not-a-real-cursor", ReadOpts{})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got %v", err)
	}
}

func TestReadSince_CursorEventNoLongerPresent(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEvents(t, dir, []Event{
		testEvent(1, base, EventCast),
	})

	r := NewReader(dir, false)
	first, err := r.ReadSince("", ReadOpts{})
	if err != nil {
		t.Fatalf("bootstrap ReadSince: %v", err)
	}

	// Simulate chronicle's rotation: the file is replaced with content that
	// no longer contains the referenced event, and everything remaining is
	// newer than it (as head-truncating rotation guarantees).
	writeRawEvents(t, dir, []Event{
		testEvent(100, base.Add(time.Hour), EventResolve),
	})

	_, err = r.ReadSince(first.NextCursor, ReadOpts{})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor after rotation dropped the cursor's event, got %v", err)
	}
}

func TestReadSince_SurvivesRotationBoundary(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEvents(t, dir, []Event{
		testEvent(1, base, EventCast),
		testEvent(2, base, EventCast),
	})

	r := NewReader(dir, false)
	first, err := r.ReadSince("", ReadOpts{})
	if err != nil {
		t.Fatalf("bootstrap ReadSince: %v", err)
	}

	// Simulate a rotation that dropped older history but preserved the
	// cursor's own event in the retained tail, plus new events after it —
	// the common case, since rotation always keeps the tail.
	writeRawEvents(t, dir, []Event{
		testEvent(2, base, EventCast), // the cursor's event, preserved
		testEvent(3, base, EventResolve),
	})

	page, err := r.ReadSince(first.NextCursor, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadSince across rotation boundary: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("got %d events, want 1 (the event appended after the rotation)", len(page.Events))
	}
	if page.Events[0].Type != EventResolve {
		t.Errorf("unexpected event survived: %+v", page.Events[0])
	}
}

func TestReadSince_LimitCapsHeadNotTail(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEvents(t, dir, []Event{testEvent(1, base, EventCast)})

	r := NewReader(dir, false)
	first, err := r.ReadSince("", ReadOpts{})
	if err != nil {
		t.Fatalf("bootstrap ReadSince: %v", err)
	}

	writeRawEvents(t, dir, []Event{
		testEvent(1, base, EventCast),
		testEvent(2, base, EventResolve),
		testEvent(3, base, EventResolve),
		testEvent(4, base, EventResolve),
	})

	page, err := r.ReadSince(first.NextCursor, ReadOpts{Limit: 2})
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("got %d events, want 2 (limit cap)", len(page.Events))
	}
	// The first new event (seq 2) must be the one returned, not the tail
	// (seq 3-4) — losslessness requires draining oldest-first.
	payload, _ := page.Events[0].Payload.(map[string]any)
	if payload["seq"] != float64(2) {
		t.Errorf("expected head event (seq=2) first, got payload %+v", page.Events[0].Payload)
	}

	// The remaining backlog must be reachable from the returned cursor.
	rest, err := r.ReadSince(page.NextCursor, ReadOpts{})
	if err != nil {
		t.Fatalf("draining ReadSince: %v", err)
	}
	if len(rest.Events) != 1 {
		t.Fatalf("got %d remaining events, want 1", len(rest.Events))
	}
}

func TestEventID_StableForIdenticalEvent(t *testing.T) {
	ev := testEvent(1, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EventCast)
	id1 := EventID(ev)
	id2 := EventID(ev)
	if id1 != id2 {
		t.Errorf("EventID not stable: %q != %q", id1, id2)
	}
	other := testEvent(2, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), EventCast)
	if EventID(other) == id1 {
		t.Error("distinct events should not share an id")
	}
}

func TestDecodeCursor_RoundTrip(t *testing.T) {
	c := Cursor{ID: "abc123", UnixNano: 12345}
	token := EncodeCursor(c)
	if !IsCursor(token) {
		t.Fatalf("encoded cursor not recognized by IsCursor: %q", token)
	}
	got, err := DecodeCursor(token)
	if err != nil {
		t.Fatalf("DecodeCursor: %v", err)
	}
	if got != c {
		t.Errorf("round trip mismatch: got %+v, want %+v", got, c)
	}
}

func TestDecodeCursor_RejectsDurationStrings(t *testing.T) {
	for _, s := range []string{"1h", "30m", "", "garbage"} {
		if IsCursor(s) {
			t.Errorf("IsCursor(%q) = true, want false", s)
		}
		if _, err := DecodeCursor(s); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("DecodeCursor(%q) error = %v, want ErrInvalidCursor", s, err)
		}
	}
}
