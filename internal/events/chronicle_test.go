package events

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
			Actor: "autarch" + string(rune('0'+i)), Visibility: "feed",
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
			Actor:      "autarch",
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

func TestChronicleRawFeedTruncationResetsOffset(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	cfg.MaxRawSize = 2048 // small threshold for testing
	c := NewChronicle(cfg)

	// Write enough events to push the raw feed over MaxRawSize.
	// Each event is ~120 bytes as JSON; 30 × 120 = ~3600 bytes > 2048.
	for i := range 30 {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventResolve,
			Actor:      fmt.Sprintf("agent-%d", i),
			Visibility: "feed",
		})
	}

	// Confirm the raw file exceeds the threshold.
	info, err := os.Stat(cfg.RawPath)
	if err != nil {
		t.Fatalf("stat raw file: %v", err)
	}
	originalRawSize := info.Size()
	if originalRawSize <= cfg.MaxRawSize {
		t.Skipf("raw file size %d did not exceed threshold %d; adjust test", originalRawSize, cfg.MaxRawSize)
	}

	// First cycle: reads all 30 events and truncates the raw feed.
	if err := c.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 1: %v", err)
	}

	// Confirm truncation occurred: file should be smaller than before.
	// (TruncateIfNeeded keeps ~75%, so the file may still exceed MaxRawSize
	// after a single pass — that is fine; the important thing is it shrank.)
	info, err = os.Stat(cfg.RawPath)
	if err != nil {
		t.Fatalf("stat raw file after truncation: %v", err)
	}
	if info.Size() >= originalRawSize {
		t.Errorf("raw file not truncated: size %d unchanged from %d", info.Size(), originalRawSize)
	}

	// Record feed state before writing new events.
	prevFeedEvents := readFeedEvents(t, cfg.FeedPath)

	// Write 3 more events AFTER the truncation cycle.
	// The offset reset must allow these to be picked up in the next cycle.
	for i := range 3 {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventTether,
			Actor:      fmt.Sprintf("post-trunc-agent-%d", i),
			Visibility: "feed",
		})
	}

	// Second cycle: should pick up the 3 post-truncation events.
	if err := c.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce 2: %v", err)
	}

	feedEvents := readFeedEvents(t, cfg.FeedPath)
	newInFeed := len(feedEvents) - len(prevFeedEvents)
	if newInFeed != 3 {
		t.Errorf("expected 3 new events in feed after truncation, got %d", newInFeed)
	}

	// Verify they are the post-truncation events.
	for _, ev := range feedEvents[len(prevFeedEvents):] {
		if ev.Type != EventTether {
			t.Errorf("expected event type %q, got %q", EventTether, ev.Type)
		}
		if !strings.HasPrefix(ev.Actor, "post-trunc-agent-") {
			t.Errorf("unexpected actor %q in post-truncation events", ev.Actor)
		}
	}
}

func TestChronicleWriteFailurePreservesOffset(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	c := NewChronicle(cfg)

	// Write 3 events to raw feed.
	for i := 0; i < 3; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventResolve,
			Actor:      "agent" + string(rune('A'+i)),
			Visibility: "feed",
		})
	}

	// Make feed path a directory so appendToFeed fails.
	if err := os.MkdirAll(cfg.FeedPath, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Cycle should fail.
	offsetBefore := c.Offset()
	err := c.ProcessOnce()
	if err == nil {
		t.Fatal("expected error from ProcessOnce with unwritable feed")
	}

	// Offset must NOT have advanced.
	if c.Offset() != offsetBefore {
		t.Errorf("offset advanced from %d to %d on write failure", offsetBefore, c.Offset())
	}

	// Remove the directory so the feed is writable again.
	os.RemoveAll(cfg.FeedPath)

	// Retry: the same events should be re-read and written successfully.
	if err := c.ProcessOnce(); err != nil {
		t.Fatalf("retry ProcessOnce: %v", err)
	}

	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 3 {
		t.Fatalf("expected 3 events after retry, got %d", len(events))
	}
}

func TestChronicleWriteFailureRollsDedupCache(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	c := NewChronicle(cfg)

	// Write a non-aggregatable event.
	writeRawEvent(t, cfg.RawPath, Event{
		Timestamp:  time.Now().UTC(),
		Source:     "sol",
		Type:       EventResolve,
		Actor:      "Toast",
		Visibility: "feed",
	})

	// Make feed path a directory so appendToFeed fails.
	if err := os.MkdirAll(cfg.FeedPath, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Cycle should fail.
	err := c.ProcessOnce()
	if err == nil {
		t.Fatal("expected error from ProcessOnce with unwritable feed")
	}

	// Remove the directory so the feed is writable again.
	os.RemoveAll(cfg.FeedPath)

	// Retry: the event must NOT be deduped — it should appear in the feed.
	if err := c.ProcessOnce(); err != nil {
		t.Fatalf("retry ProcessOnce: %v", err)
	}

	events := readFeedEvents(t, cfg.FeedPath)
	if len(events) != 1 {
		t.Fatalf("expected 1 event after retry (dedup rolled back), got %d", len(events))
	}
	if events[0].Actor != "Toast" {
		t.Errorf("expected actor Toast, got %q", events[0].Actor)
	}
}

// TestChronicleAggBufferRollback verifies that an appendToFeed failure mid-cycle
// fully rolls back the aggregation buffer state, so a retry on the next cycle
// does not produce a doubled batch count (CF-M17).
//
// Scenario:
//  1. Cycle A: 3 cast events arrive — go into agg buffer (window not yet expired).
//  2. Sleep past AggWindow.
//  3. 2 more cast events arrive.
//  4. Feed path is replaced by a directory so appendToFeed fails.
//  5. Cycle B: tries to flush 5 events (3 + 2). Write fails. Rollback must
//     restore the buffer to {3 events from cycle A} — NOT leave 5 in place,
//     because the offset is also rolled back and the 2 new events will be
//     re-read on the next cycle.
//  6. Feed path is repaired.
//  7. Cycle C: re-reads the 2 new events, re-aggregates → buffer has 5 →
//     flushes → batch count must be exactly 5, not 7 or 8.
func TestChronicleAggBufferRollback(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	cfg.AggWindow = 80 * time.Millisecond
	c := NewChronicle(cfg)

	// Cycle A: 3 cast events.
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  now,
			Source:     "sol",
			Type:       EventCast,
			Actor:      fmt.Sprintf("a-%d", i),
			Visibility: "feed",
		})
	}
	if err := c.ProcessOnce(); err != nil {
		t.Fatalf("cycle A: %v", err)
	}

	// Wait for the aggregation window to expire so the next cycle will flush.
	time.Sleep(150 * time.Millisecond)

	// 2 more cast events.
	for i := 0; i < 2; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventCast,
			Actor:      fmt.Sprintf("b-%d", i),
			Visibility: "feed",
		})
	}

	// Make feed unwritable so appendToFeed fails.
	if err := os.MkdirAll(cfg.FeedPath, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Cycle B (failing).
	if err := c.ProcessOnce(); err == nil {
		t.Fatal("cycle B: expected failure with unwritable feed")
	}

	// Repair the feed.
	os.RemoveAll(cfg.FeedPath)

	// Cycle C: retry — should produce a single cast_batch with count=5.
	if err := c.ProcessOnce(); err != nil {
		t.Fatalf("cycle C: %v", err)
	}

	events := readFeedEvents(t, cfg.FeedPath)
	var batches []Event
	for _, ev := range events {
		if ev.Type == "cast_batch" {
			batches = append(batches, ev)
		}
	}
	if len(batches) != 1 {
		t.Fatalf("expected exactly 1 cast_batch event, got %d (%+v)", len(batches), events)
	}
	payload, ok := batches[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload is not map: %T", batches[0].Payload)
	}
	count, ok := payload["count"].(float64)
	if !ok {
		t.Fatalf("count missing/wrong type: %v", payload["count"])
	}
	if int(count) != 5 {
		t.Errorf("expected cast_batch count=5 (no duplication), got %d", int(count))
	}
}

// TestChronicleEmitsDroppedEventOnRotation verifies that when raw-feed
// rotation drops events the chronicle has not yet read, a chronicle_dropped
// event is emitted with the count of lost lines (CF-M18). Uses
// testHookBeforeRotate to deterministically inject the race-window writes.
func TestChronicleEmitsDroppedEventOnRotation(t *testing.T) {
	dir := t.TempDir()
	cfg := testChronicleConfig(dir)
	cfg.MaxRawSize = 800 // small threshold so test data triggers rotation
	cfg.AggWindow = 24 * time.Hour
	cfg.DedupWindow = 24 * time.Hour

	// Logger writes to a separate observer file so we don't recurse into
	// the raw events file the chronicle is reading.
	observerPath := filepath.Join(dir, "observer.jsonl")
	logger := &Logger{path: observerPath}

	c := NewChronicle(cfg, WithLogger(logger))

	// Pre-write 2 small events the chronicle will consume normally.
	for i := 0; i < 2; i++ {
		writeRawEvent(t, cfg.RawPath, Event{
			Timestamp:  time.Now().UTC(),
			Source:     "sol",
			Type:       EventResolve,
			Actor:      fmt.Sprintf("pre-%d", i),
			Visibility: "feed",
		})
	}

	// The hook fires after readNewEvents commits the offset and before
	// raw-feed rotation. It appends a flood of events that push the file
	// over MaxRawSize and (because they sit beyond the chronicle's offset)
	// will be the ones eligible to be dropped by the rotation clamp.
	c.testHookBeforeRotate = func() {
		f, err := os.OpenFile(cfg.RawPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatalf("hook open: %v", err)
		}
		defer f.Close()
		// Write a large number of small events to push the file well over
		// MaxRawSize (800B). Each line is roughly 110 bytes, so 30 lines is
		// well above the threshold.
		for i := 0; i < 30; i++ {
			ev := Event{
				Timestamp:  time.Now().UTC(),
				Source:     "sol",
				Type:       EventResolve,
				Actor:      fmt.Sprintf("race-%02d", i),
				Visibility: "feed",
			}
			data, _ := json.Marshal(ev)
			f.Write(append(data, '\n'))
		}
	}

	if err := c.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}

	// Inspect the observer file for a chronicle_dropped event.
	obsData, err := os.ReadFile(observerPath)
	if err != nil {
		t.Fatalf("read observer: %v", err)
	}

	var dropped []Event
	for _, line := range strings.Split(strings.TrimRight(string(obsData), "\n"), "\n") {
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal observer line: %v", err)
		}
		if ev.Type == EventChronicleDropped {
			dropped = append(dropped, ev)
		}
	}
	if len(dropped) == 0 {
		t.Fatalf("expected at least one chronicle_dropped event in observer; observer=%s", obsData)
	}

	// The chronicle_dropped event should record a positive count and
	// reason="raw_feed_rotation".
	payload, ok := dropped[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("dropped payload not map: %T", dropped[0].Payload)
	}
	if reason, _ := payload["reason"].(string); reason != "raw_feed_rotation" {
		t.Errorf("expected reason=raw_feed_rotation, got %q", reason)
	}
	count, ok := payload["dropped_count"].(float64)
	if !ok {
		t.Fatalf("dropped_count missing/wrong type: %v", payload["dropped_count"])
	}
	if int(count) <= 0 {
		t.Errorf("expected dropped_count > 0, got %d", int(count))
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
