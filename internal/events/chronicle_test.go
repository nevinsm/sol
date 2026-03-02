package events

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRawEvent(t *testing.T, path string, ev Event) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open raw feed: %v", err)
	}
	defer f.Close()
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	f.Write(append(data, '\n'))
}

func readFeedEvents(t *testing.T, path string) []Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read feed: %v", err)
	}
	var events []Event
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal feed event: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func testChronicleConfig(dir string) ChronicleConfig {
	return ChronicleConfig{
		RawPath:      filepath.Join(dir, ".events.jsonl"),
		FeedPath:     filepath.Join(dir, ".feed.jsonl"),
		PollInterval: 100 * time.Millisecond,
		DedupWindow:  10 * time.Second,
		AggWindow:    30 * time.Second,
		MaxFeedSize:  10 * 1024 * 1024,
	}
}

func TestChronicleProcessesNewEvents(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	chronicle := NewChronicle(cfg)

	// Write 5 events to raw feed.
	for i := 0; i < 5; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventResolve,
			Actor:      "agent" + string(rune('A'+i)),
			Visibility: "feed",
			Payload:    map[string]int{"i": i},
		})
	}

	// Run one chronicle cycle.
	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}

	// Verify: 5 events appear in curated feed.
	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 5 {
		t.Fatalf("expected 5 events in curated feed, got %d", len(events))
	}
}

func TestChronicleFiltersAuditOnly(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	chronicle := NewChronicle(cfg)

	// Write events: 2 with visibility="both", 1 with visibility="audit".
	writeRawEvent(t, cfg.RawPath, Event{
		Timestamp: time.Now().UTC(), Source: "sol", Type: EventResolve,
		Actor: "agentA", Visibility: "both",
	})
	writeRawEvent(t, cfg.RawPath, Event{
		Timestamp: time.Now().UTC(), Source: "sol", Type: EventResolve,
		Actor: "agentB", Visibility: "both",
	})
	writeRawEvent(t, cfg.RawPath, Event{
		Timestamp: time.Now().UTC(), Source: "sol", Type: EventResolve,
		Actor: "agentC", Visibility: "audit",
	})

	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}

	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 2 {
		t.Fatalf("expected 2 events (audit excluded), got %d", len(events))
	}
}

func TestChronicleDeduplicates(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	chronicle := NewChronicle(cfg)

	// Write 3 identical events (same type/source/actor) within 10s.
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Source:    "sol", Type: EventResolve, Actor: "Toast", Visibility: "feed",
		})
	}

	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}

	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 event (deduped), got %d", len(events))
	}
}

func TestChronicleDeduplicateWindowExpiry(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	cfg.DedupWindow = 100 * time.Millisecond // short window for testing
	chronicle := NewChronicle(cfg)

	// Write event A.
	writeRawEvent(t, cfg.RawPath, Event{
		Timestamp: time.Now().UTC(), Source: "sol", Type: EventResolve,
		Actor: "Toast", Visibility: "feed",
	})

	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 1: %v", err)
	}

	// Wait for dedup window to expire.
	time.Sleep(150 * time.Millisecond)

	// Write event A again.
	writeRawEvent(t, cfg.RawPath, Event{
		Timestamp: time.Now().UTC(), Source: "sol", Type: EventResolve,
		Actor: "Toast", Visibility: "feed",
	})

	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 2: %v", err)
	}

	// Verify: curated feed has 2 events (dedup window expired).
	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 2 {
		t.Fatalf("expected 2 events (window expired), got %d", len(events))
	}
}

func TestChronicleAggregatesCastBurst(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	cfg.AggWindow = 100 * time.Millisecond // short window for testing
	chronicle := NewChronicle(cfg)

	// Write 10 cast events within the window.
	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			Source:    "sol", Type: EventCast,
			Actor: "operator" + string(rune('0'+i)), Visibility: "feed",
		})
	}

	// First cycle: events go into agg buffer.
	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 1: %v", err)
	}

	// Wait for agg window to expire.
	time.Sleep(150 * time.Millisecond)

	// Second cycle: flushes the agg buffer.
	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 2: %v", err)
	}

	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(events))
	}

	if events[0].Type != "cast_batch" {
		t.Errorf("expected type cast_batch, got %q", events[0].Type)
	}
	if events[0].Source != "chronicle" {
		t.Errorf("expected source chronicle, got %q", events[0].Source)
	}

	payload, ok := events[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any: %T", events[0].Payload)
	}
	count, ok := payload["count"]
	if !ok {
		t.Fatal("payload missing 'count'")
	}
	if int(count.(float64)) != 10 {
		t.Errorf("expected count=10, got %v", count)
	}
}

func TestChronicleAggregatesSameActorCastBurst(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	cfg.AggWindow = 100 * time.Millisecond
	chronicle := NewChronicle(cfg)

	// Write 5 cast events from the SAME actor within the window.
	// These must not be deduped — they should aggregate into a cast_batch.
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  now.Add(time.Duration(i) * time.Millisecond),
			Source:     "sol",
			Type:       EventCast,
			Actor:      "operator",
			Visibility: "feed",
			Payload:    map[string]string{"item": "sol-" + string(rune('a'+i))},
		})
	}

	// First cycle: events go into agg buffer.
	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 1: %v", err)
	}

	// Wait for agg window to expire.
	time.Sleep(150 * time.Millisecond)

	// Second cycle: flushes the agg buffer.
	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 2: %v", err)
	}

	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(events))
	}

	if events[0].Type != "cast_batch" {
		t.Errorf("expected type cast_batch, got %q", events[0].Type)
	}

	payload, ok := events[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map[string]any: %T", events[0].Payload)
	}
	count, ok := payload["count"]
	if !ok {
		t.Fatal("payload missing 'count'")
	}
	if int(count.(float64)) != 5 {
		t.Errorf("expected count=5, got %v", count)
	}
}

func TestChronicleDoesNotAggregateNonBatchable(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	cfg.AggWindow = 100 * time.Millisecond
	chronicle := NewChronicle(cfg)

	// Write 3 "done" events within 30s (different actors to avoid dedup).
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			Source:    "sol", Type: EventResolve,
			Actor: "agent" + string(rune('A'+i)), Visibility: "feed",
		})
	}

	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}

	// Done events are not aggregated — they pass through directly.
	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 3 {
		t.Fatalf("expected 3 individual events, got %d", len(events))
	}
	for _, ev := range events {
		if ev.Type != EventResolve {
			t.Errorf("expected done event, got %q", ev.Type)
		}
	}
}

func TestChronicleTruncatesFeed(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	cfg.MaxFeedSize = 1024 // 1KB for testing
	chronicle := NewChronicle(cfg)

	// Write enough events to exceed the limit.
	for i := 0; i < 50; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventResolve,
			Actor:      "agent" + string(rune('A'+(i%26))),
			Visibility: "feed",
			Payload:    map[string]string{"data": "padding-to-make-event-bigger-for-truncation-test"},
		})
	}

	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}

	// Check that truncation occurred.
	info, err := os.Stat(cfg.FeedPath)
	if err != nil {
		t.Fatalf("stat feed: %v", err)
	}

	// After truncation, size should be roughly 75% of max.
	if info.Size() > cfg.MaxFeedSize {
		t.Errorf("feed size %d exceeds max %d after truncation", info.Size(), cfg.MaxFeedSize)
	}

	// Verify remaining events are valid JSON lines.
	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) == 0 {
		t.Fatal("feed should have events after truncation")
	}

	// Verify no truncated/partial lines.
	data, _ := os.ReadFile(cfg.FeedPath)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			t.Errorf("invalid JSON line after truncation: %s", line)
		}
	}
}

func TestChronicleCheckpoint(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	chronicle := NewChronicle(cfg)

	// Write 5 events, run chronicle.
	for i := 0; i < 5; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventResolve,
			Actor:      "agent" + string(rune('A'+i)),
			Visibility: "feed",
		})
	}

	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 1: %v", err)
	}

	// Stop chronicle, verify checkpoint file exists with offset.
	checkpointPath := filepath.Join(dir, ".chronicle-checkpoint")
	if _, err := os.Stat(checkpointPath); os.IsNotExist(err) {
		t.Fatal("checkpoint file should exist")
	}
	savedOffset := chronicle.Offset()
	if savedOffset == 0 {
		t.Fatal("offset should be non-zero after processing events")
	}

	// Write 5 more events.
	for i := 0; i < 5; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventResolve,
			Actor:      "new-agent" + string(rune('A'+i)),
			Visibility: "feed",
		})
	}

	// Start new chronicle (should resume from checkpoint).
	chronicle2 := NewChronicle(cfg)
	chronicle2.LoadCheckpoint()
	if chronicle2.Offset() != savedOffset {
		t.Fatalf("new chronicle offset %d != saved offset %d", chronicle2.Offset(), savedOffset)
	}

	if err := chronicle2.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 2: %v", err)
	}

	// Verify: total events in feed = 10 (5 from first + 5 from second).
	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 10 {
		t.Fatalf("expected 10 total events, got %d", len(events))
	}
}

func TestChronicleRunLifecycle(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	cfg.PollInterval = 50 * time.Millisecond // fast polling for test
	chronicle := NewChronicle(cfg)

	// Start chronicle with cancellable context.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- chronicle.Run(ctx)
	}()

	// Give it time to start and set initial offset.
	time.Sleep(100 * time.Millisecond)

	// Write events to raw feed.
	for i := 0; i < 3; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventResolve,
			Actor:      "agent" + string(rune('A'+i)),
			Visibility: "feed",
		})
	}

	// Wait for one poll cycle.
	time.Sleep(200 * time.Millisecond)

	// Verify events appear in curated feed.
	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 3 {
		t.Fatalf("expected 3 events in curated feed, got %d", len(events))
	}

	// Cancel context, verify clean shutdown.
	cancel()
	err := <-errCh
	if err != nil {
		t.Errorf("Run returned unexpected error: %v", err)
	}

	// Verify checkpoint was saved on shutdown.
	checkpointPath := filepath.Join(dir, ".chronicle-checkpoint")
	if _, err := os.Stat(checkpointPath); os.IsNotExist(err) {
		t.Error("checkpoint should be saved on shutdown")
	}
}
