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

func TestReadFrom_Bootstrap(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEvents(t, dir, []Event{
		testEvent(1, base, EventCast),
		testEvent(2, base, EventCast),
	})

	r := NewReader(dir, false)
	evts, offset, rotated, err := r.ReadFrom(0, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if len(evts) != 2 {
		t.Fatalf("got %d events, want 2", len(evts))
	}
	if rotated {
		t.Error("bootstrap read (offset=0) should not report rotated")
	}
	if offset <= 0 {
		t.Errorf("expected a positive offset after a non-empty bootstrap read, got %d", offset)
	}
}

func TestReadFrom_BootstrapMissingFile(t *testing.T) {
	dir := t.TempDir()
	r := NewReader(dir, false)
	evts, offset, rotated, err := r.ReadFrom(0, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if len(evts) != 0 || offset != 0 || rotated {
		t.Errorf("got (%v, %d, %v), want (empty, 0, false)", evts, offset, rotated)
	}
}

func TestReadFrom_IncrementalReturnsOnlyNewEvents(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEvents(t, dir, []Event{
		testEvent(1, base, EventCast),
		testEvent(2, base, EventCast),
	})

	r := NewReader(dir, false)
	evts, offset, rotated, err := r.ReadFrom(0, ReadOpts{})
	if err != nil {
		t.Fatalf("bootstrap ReadFrom: %v", err)
	}
	if len(evts) != 2 || rotated {
		t.Fatalf("unexpected bootstrap result: evts=%d rotated=%v", len(evts), rotated)
	}

	// Grow the file (bytes [0,offset) are preserved, only the suffix is
	// new — mirrors chronicle's append-only writer).
	writeRawEvents(t, dir, []Event{
		testEvent(1, base, EventCast),
		testEvent(2, base, EventCast),
		testEvent(3, base, EventResolve),
		testEvent(4, base, EventResolve),
	})

	evts, offset2, rotated, err := r.ReadFrom(offset, ReadOpts{})
	if err != nil {
		t.Fatalf("incremental ReadFrom: %v", err)
	}
	if rotated {
		t.Error("plain growth should not report rotated")
	}
	if len(evts) != 2 {
		t.Fatalf("got %d events, want 2 (only the new ones)", len(evts))
	}
	for _, ev := range evts {
		if ev.Type != EventResolve {
			t.Errorf("unexpected event leaked into increment: %+v", ev)
		}
	}
	if offset2 <= offset {
		t.Errorf("offset should have advanced: got %d, want > %d", offset2, offset)
	}

	// A third call with no new writes returns nothing and an unchanged offset.
	evts, offset3, rotated, err := r.ReadFrom(offset2, ReadOpts{})
	if err != nil {
		t.Fatalf("empty increment ReadFrom: %v", err)
	}
	if len(evts) != 0 || rotated || offset3 != offset2 {
		t.Errorf("got (%d events, rotated=%v, offset=%d), want (0, false, %d)", len(evts), rotated, offset3, offset2)
	}
}

func TestReadFrom_MissingFileWithPriorOffsetReportsRotated(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	writeRawEvents(t, dir, []Event{testEvent(1, base, EventCast)})

	r := NewReader(dir, false)
	_, offset, _, err := r.ReadFrom(0, ReadOpts{})
	if err != nil {
		t.Fatalf("bootstrap ReadFrom: %v", err)
	}

	if err := os.Remove(filepath.Join(dir, ".events.jsonl")); err != nil {
		t.Fatalf("remove feed file: %v", err)
	}

	evts, newOffset, rotated, err := r.ReadFrom(offset, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadFrom after file removal: %v", err)
	}
	if !rotated {
		t.Error("expected rotated=true when the file disappeared out from under a nonzero offset")
	}
	if len(evts) != 0 || newOffset != 0 {
		t.Errorf("got (%d events, offset=%d), want (0, 0)", len(evts), newOffset)
	}
}

// TestReadFrom_ShrinkTriggersFallback covers the rotation/truncation
// scenario: chronicle's truncateOnce (curated feed) and TruncateIfNeeded
// (raw feed) both replace the file in place via temp-file + atomic rename,
// dropping the oldest portion and shrinking the file. A reader holding an
// offset from before that shrink must recover with a full re-read instead
// of seeking past EOF or erroring.
func TestReadFrom_ShrinkTriggersFallback(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var evts []Event
	for i := 0; i < 50; i++ {
		evts = append(evts, testEvent(i, base, EventCast))
	}
	writeRawEvents(t, dir, evts)

	r := NewReader(dir, false)
	_, offset, rotated, err := r.ReadFrom(0, ReadOpts{})
	if err != nil {
		t.Fatalf("bootstrap ReadFrom: %v", err)
	}
	if rotated {
		t.Fatal("bootstrap should not report rotated")
	}

	// Simulate chronicle's rotation: replace the file with a much shorter
	// one (as truncateOnce's head-drop-and-rename does), whose size is well
	// below the offset we're holding.
	replacement := []Event{testEvent(999, base.Add(time.Hour), EventResolve)}
	writeRawEvents(t, dir, replacement)

	if info, statErr := os.Stat(filepath.Join(dir, ".events.jsonl")); statErr != nil {
		t.Fatalf("stat replacement file: %v", statErr)
	} else if info.Size() >= offset {
		t.Fatalf("test setup invalid: replacement file (%d bytes) must be smaller than the held offset (%d)", info.Size(), offset)
	}

	got, newOffset, rotated, err := r.ReadFrom(offset, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadFrom after shrink: %v", err)
	}
	if !rotated {
		t.Error("expected rotated=true after the file shrank below the held offset")
	}
	if len(got) != 1 || got[0].Type != EventResolve {
		t.Fatalf("expected the fallback full-read to surface the replacement file's contents, got %+v", got)
	}
	if newOffset <= 0 {
		t.Errorf("expected a positive offset after the fallback read, got %d", newOffset)
	}

	// The reader must keep working normally on the next call — no repeat
	// crash, no re-triggering of the fallback path for unrelated growth.
	writeRawEvents(t, dir, append(replacement, testEvent(1000, base.Add(2*time.Hour), EventResolve)))
	got, _, rotated, err = r.ReadFrom(newOffset, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadFrom after recovery: %v", err)
	}
	if rotated {
		t.Error("normal growth right after a fallback should not itself report rotated")
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1 (events may re-appear once across a rotation, but not repeatedly)", len(got))
	}
}

// TestReadFrom_PartialTrailingLineNotConsumed ensures a write-in-progress
// (a line without its terminating newline yet) is not parsed and does not
// advance the returned offset, so a later call re-reads it complete.
func TestReadFrom_PartialTrailingLineNotConsumed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".events.jsonl")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	complete, err := json.Marshal(testEvent(1, base, EventCast))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	partial, err := json.Marshal(testEvent(2, base, EventCast))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Complete line, then a partial line with no trailing '\n'.
	var data []byte
	data = append(data, complete...)
	data = append(data, '\n')
	data = append(data, partial[:len(partial)/2]...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := NewReader(dir, false)
	evts, offset, rotated, err := r.ReadFrom(0, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if rotated {
		t.Error("bootstrap should not report rotated")
	}
	if len(evts) != 1 {
		t.Fatalf("got %d events, want 1 (partial trailing line must not parse)", len(evts))
	}
	if int(offset) != len(complete)+1 {
		t.Errorf("offset = %d, want %d (only the complete line's bytes)", offset, len(complete)+1)
	}

	// Complete the second line and confirm it's now picked up.
	var completed []byte
	completed = append(completed, complete...)
	completed = append(completed, '\n')
	completed = append(completed, partial...)
	completed = append(completed, '\n')
	if err := os.WriteFile(path, completed, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	evts, _, rotated, err = r.ReadFrom(offset, ReadOpts{})
	if err != nil {
		t.Fatalf("ReadFrom after completing line: %v", err)
	}
	if rotated {
		t.Error("completing the trailing line should not report rotated")
	}
	if len(evts) != 1 {
		t.Fatalf("got %d events, want 1 (the now-complete line)", len(evts))
	}
}

// BenchmarkReader_Read_LargeLog measures Read's full-rescan cost against a
// large synthetic log — the O(file) cost the dash feed paid on every 3s
// refresh before ReadFrom existed.
func BenchmarkReader_Read_LargeLog(b *testing.B) {
	dir := b.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var evts []Event
	for i := 0; i < 100_000; i++ {
		evts = append(evts, testEvent(i, base, EventCast))
	}
	writeRawEventsB(b, dir, evts)

	r := NewReader(dir, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Read(ReadOpts{Limit: 10}); err != nil {
			b.Fatalf("Read: %v", err)
		}
	}
}

// BenchmarkReader_ReadFrom_SteadyState measures ReadFrom's incremental cost
// once caught up to a large synthetic log: each call should only pay for the
// handful of bytes appended since the last call, not the whole file.
func BenchmarkReader_ReadFrom_SteadyState(b *testing.B) {
	dir := b.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var evts []Event
	for i := 0; i < 100_000; i++ {
		evts = append(evts, testEvent(i, base, EventCast))
	}
	writeRawEventsB(b, dir, evts)

	r := NewReader(dir, false)
	_, offset, _, err := r.ReadFrom(0, ReadOpts{})
	if err != nil {
		b.Fatalf("bootstrap ReadFrom: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := r.ReadFrom(offset, ReadOpts{}); err != nil {
			b.Fatalf("ReadFrom: %v", err)
		}
	}
}

// writeRawEventsB is writeRawEvents for benchmarks (testing.TB has no
// t.Helper()-compatible Fatalf-on-failure friendliness difference here, but
// benchmarks use *testing.B rather than *testing.T).
func writeRawEventsB(b *testing.B, dir string, evts []Event) {
	b.Helper()
	path := filepath.Join(dir, ".events.jsonl")
	var data []byte
	for _, ev := range evts {
		line, err := json.Marshal(ev)
		if err != nil {
			b.Fatalf("marshal event: %v", err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		b.Fatalf("write raw events file: %v", err)
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
