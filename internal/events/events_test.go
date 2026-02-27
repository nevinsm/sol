package events

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogEvent(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)

	logger.Emit(EventSling, "gt", "operator", "both", map[string]string{
		"work_item_id": "gt-a1b2c3d4",
		"agent":        "Toast",
		"rig":          "myrig",
	})

	// Read the JSONL file, verify one line of valid JSON.
	path := filepath.Join(dir, ".events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}

	lines := splitLines(data)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var ev Event
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if ev.Type != EventSling {
		t.Errorf("type: got %q, want %q", ev.Type, EventSling)
	}
	if ev.Source != "gt" {
		t.Errorf("source: got %q, want %q", ev.Source, "gt")
	}
	if ev.Actor != "operator" {
		t.Errorf("actor: got %q, want %q", ev.Actor, "operator")
	}
	if ev.Visibility != "both" {
		t.Errorf("visibility: got %q, want %q", ev.Visibility, "both")
	}
	if ev.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestLogMultipleEvents(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)

	for i := 0; i < 5; i++ {
		logger.Emit(EventDone, "gt", "agent", "feed", map[string]int{"index": i})
	}

	path := filepath.Join(dir, ".events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}

	lines := splitLines(data)
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}

	// Verify chronological order (timestamps non-decreasing).
	var prev time.Time
	for i, line := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal line %d: %v", i, err)
		}
		if ev.Timestamp.Before(prev) {
			t.Errorf("line %d: timestamp %v is before previous %v", i, ev.Timestamp, prev)
		}
		prev = ev.Timestamp
	}
}

func TestLogBestEffort(t *testing.T) {
	// Create logger pointing to non-existent directory.
	logger := NewLogger("/nonexistent/path/that/does/not/exist")

	// Log should not panic or return error.
	logger.Log(Event{Type: "test", Source: "test", Actor: "test"})
	logger.Emit("test", "test", "test", "both", nil)
	// If we reach here without panic, the test passes.
}

func TestLogConcurrent(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(goroutine int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				logger.Emit("test", "gt", "goroutine", "feed", map[string]int{
					"goroutine": goroutine,
					"index":     i,
				})
			}
		}(g)
	}
	wg.Wait()

	path := filepath.Join(dir, ".events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}

	lines := splitLines(data)
	if len(lines) != 100 {
		t.Fatalf("expected 100 lines, got %d", len(lines))
	}

	// Each line must be valid JSON (no interleaving).
	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %s", i, line)
		}
	}
}

func TestReadEvents(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)

	// Log 10 events of mixed types.
	for i := 0; i < 5; i++ {
		logger.Emit(EventSling, "gt", "operator", "feed", nil)
	}
	for i := 0; i < 5; i++ {
		logger.Emit(EventDone, "gt", "agent", "both", nil)
	}

	reader := NewReader(dir, false)

	// Read with no filters -> 10 events.
	evts, err := reader.Read(ReadOpts{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(evts) != 10 {
		t.Fatalf("expected 10 events, got %d", len(evts))
	}

	// Read with Limit=5 -> last 5 events.
	evts, err = reader.Read(ReadOpts{Limit: 5})
	if err != nil {
		t.Fatalf("read with limit: %v", err)
	}
	if len(evts) != 5 {
		t.Fatalf("expected 5 events with limit, got %d", len(evts))
	}
	// Should be the last 5 (all "done" events).
	for _, ev := range evts {
		if ev.Type != EventDone {
			t.Errorf("expected done event, got %q", ev.Type)
		}
	}

	// Read with Type filter -> only matching events.
	evts, err = reader.Read(ReadOpts{Type: EventSling})
	if err != nil {
		t.Fatalf("read with type: %v", err)
	}
	if len(evts) != 5 {
		t.Fatalf("expected 5 sling events, got %d", len(evts))
	}
}

func TestReadSince(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)

	// Log some events.
	for i := 0; i < 3; i++ {
		logger.Emit("old", "gt", "operator", "feed", nil)
	}

	cutoff := time.Now()
	time.Sleep(10 * time.Millisecond)

	for i := 0; i < 2; i++ {
		logger.Emit("new", "gt", "operator", "feed", nil)
	}

	reader := NewReader(dir, false)
	evts, err := reader.Read(ReadOpts{Since: cutoff})
	if err != nil {
		t.Fatalf("read with since: %v", err)
	}
	if len(evts) != 2 {
		t.Fatalf("expected 2 events after cutoff, got %d", len(evts))
	}
	for _, ev := range evts {
		if ev.Type != "new" {
			t.Errorf("expected 'new' event, got %q", ev.Type)
		}
	}
}

func TestReadFiltersAuditOnly(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)

	// Log events with visibility="audit" — should be excluded from reads.
	logger.Emit("audit_event", "gt", "system", "audit", nil)
	// Log events with visibility="both" — should be included.
	logger.Emit("both_event", "gt", "system", "both", nil)
	// Log events with visibility="feed" — should be included.
	logger.Emit("feed_event", "gt", "system", "feed", nil)

	reader := NewReader(dir, false)
	evts, err := reader.Read(ReadOpts{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if len(evts) != 2 {
		t.Fatalf("expected 2 events (audit excluded), got %d", len(evts))
	}

	types := map[string]bool{}
	for _, ev := range evts {
		types[ev.Type] = true
	}
	if types["audit_event"] {
		t.Error("audit-only event should be excluded")
	}
	if !types["both_event"] {
		t.Error("'both' event should be included")
	}
	if !types["feed_event"] {
		t.Error("'feed' event should be included")
	}
}

func TestFollow(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)

	// Pre-create the file with one event so Follow can open it.
	logger.Emit("setup", "gt", "setup", "feed", nil)

	reader := NewReader(dir, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan Event, 16)
	errCh := make(chan error, 1)
	go func() {
		errCh <- reader.Follow(ctx, ReadOpts{}, ch)
	}()

	// Give Follow time to start and seek to end.
	time.Sleep(100 * time.Millisecond)

	// Log new events.
	for i := 0; i < 3; i++ {
		logger.Emit("follow_test", "gt", "operator", "feed", map[string]int{"i": i})
	}

	// Collect events from channel.
	var received []Event
	timeout := time.After(5 * time.Second)
	for len(received) < 3 {
		select {
		case ev := <-ch:
			received = append(received, ev)
		case <-timeout:
			t.Fatalf("timed out waiting for events, got %d", len(received))
		}
	}

	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d", len(received))
	}
	for _, ev := range received {
		if ev.Type != "follow_test" {
			t.Errorf("expected 'follow_test' event, got %q", ev.Type)
		}
	}

	// Cancel context -> Follow returns.
	cancel()
	err := <-errCh
	if err != nil && err != context.Canceled {
		t.Errorf("follow returned unexpected error: %v", err)
	}
}

func TestFollowSurvivesTruncation(t *testing.T) {
	dir := t.TempDir()
	feedPath := filepath.Join(dir, ".events.jsonl")
	logger := NewLogger(dir)

	// Write initial events so Follow can open the file.
	for i := 0; i < 5; i++ {
		logger.Emit("initial", "gt", "operator", "feed", nil)
	}

	reader := NewReader(dir, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan Event, 32)
	errCh := make(chan error, 1)
	go func() {
		errCh <- reader.Follow(ctx, ReadOpts{}, ch)
	}()

	// Give Follow time to start and seek to end.
	time.Sleep(200 * time.Millisecond)

	// Simulate curator truncation: write a new file and atomically rename
	// over the feed path. This replaces the inode.
	tmp, err := os.CreateTemp(dir, ".truncate-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	ev := Event{
		Timestamp:  time.Now().UTC(),
		Type:       "survived",
		Source:     "curator",
		Actor:      "curator",
		Visibility: "feed",
	}
	data, _ := json.Marshal(ev)
	tmp.Write(append(data, '\n'))
	tmp.Close()
	os.Rename(tmp.Name(), feedPath)

	// Write new events after truncation (appended to new inode).
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 3; i++ {
		logger.Emit("post_truncation", "gt", "operator", "feed", nil)
	}

	// Should receive the "survived" event plus the 3 post-truncation events.
	var received []Event
	timeout := time.After(5 * time.Second)
	for len(received) < 4 {
		select {
		case ev := <-ch:
			received = append(received, ev)
		case <-timeout:
			t.Fatalf("timed out waiting for events after truncation, got %d", len(received))
		}
	}

	foundSurvived := false
	postCount := 0
	for _, ev := range received {
		if ev.Type == "survived" {
			foundSurvived = true
		}
		if ev.Type == "post_truncation" {
			postCount++
		}
	}
	if !foundSurvived {
		t.Error("expected to receive 'survived' event from truncated file")
	}
	if postCount != 3 {
		t.Errorf("expected 3 post_truncation events, got %d", postCount)
	}

	cancel()
	<-errCh
}

// splitLines splits data by newline, ignoring trailing empty line.
func splitLines(data []byte) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
