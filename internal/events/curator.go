package events

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// CuratorConfig holds curator configuration.
type CuratorConfig struct {
	RawPath      string        // path to raw .events.jsonl
	FeedPath     string        // path to curated .feed.jsonl
	PollInterval time.Duration // how often to check for new events (default: 2s)
	DedupWindow  time.Duration // dedup window for identical events (default: 10s)
	AggWindow    time.Duration // aggregation window for burst events (default: 30s)
	MaxFeedSize  int64         // max curated feed file size in bytes (default: 10MB)
}

// DefaultCuratorConfig returns defaults for the given GT_HOME.
func DefaultCuratorConfig(gtHome string) CuratorConfig {
	return CuratorConfig{
		RawPath:      filepath.Join(gtHome, ".events.jsonl"),
		FeedPath:     filepath.Join(gtHome, ".feed.jsonl"),
		PollInterval: 2 * time.Second,
		DedupWindow:  10 * time.Second,
		AggWindow:    30 * time.Second,
		MaxFeedSize:  10 * 1024 * 1024, // 10MB
	}
}

// Curator processes raw events into a curated feed.
type Curator struct {
	config     CuratorConfig
	offset     int64 // file offset — tracks position in raw feed
	dedupCache []dedupEntry
	aggBuffers map[string]*aggBuffer // keyed by event type
}

type dedupEntry struct {
	Type   string
	Source string
	Actor  string
	SeenAt time.Time
}

// aggBuffer holds events being aggregated for a single event type.
type aggBuffer struct {
	events []Event
}

// aggregatableTypes are event types that can be collapsed into batch events.
var aggregatableTypes = map[string]bool{
	EventSling:   true,
	EventRespawn: true,
}

// NewCurator creates a curator.
func NewCurator(config CuratorConfig) *Curator {
	return &Curator{
		config:     config,
		aggBuffers: make(map[string]*aggBuffer),
	}
}

// Run starts the curator loop. Blocks until context is cancelled.
func (c *Curator) Run(ctx context.Context) error {
	// Load checkpoint if it exists.
	c.loadCheckpoint()

	// If no checkpoint, start at current end-of-file.
	if c.offset == 0 {
		info, err := os.Stat(c.config.RawPath)
		if err == nil {
			c.offset = info.Size()
		}
		// If file doesn't exist, offset stays 0.
	}

	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.saveCheckpoint()
			return nil
		case <-ticker.C:
			if err := c.processCycle(); err != nil {
				// Best-effort: log but continue.
				fmt.Fprintf(os.Stderr, "curator cycle error: %v\n", err)
			}
		}
	}
}

// ProcessOnce runs a single processing cycle. Exported for testing.
func (c *Curator) ProcessOnce() error {
	return c.processCycle()
}

// LoadCheckpoint loads the checkpoint. Exported for testing.
func (c *Curator) LoadCheckpoint() {
	c.loadCheckpoint()
}

// Offset returns the current read offset. Exported for testing.
func (c *Curator) Offset() int64 {
	return c.offset
}

// processCycle reads new events from the raw feed, filters/dedup/aggregates,
// and appends results to the curated feed.
func (c *Curator) processCycle() error {
	// 1. Read new lines from raw feed starting at offset.
	newEvents, newOffset, err := c.readNewEvents()
	if err != nil {
		return err
	}
	c.offset = newOffset

	// 2. Filter: skip visibility="audit".
	var filtered []Event
	for _, ev := range newEvents {
		if ev.Visibility == "audit" {
			continue
		}
		filtered = append(filtered, ev)
	}

	// 3. Dedup.
	var deduped []Event
	now := time.Now()
	c.cleanDedupCache(now)
	for _, ev := range filtered {
		if c.isDuplicate(ev, now) {
			continue
		}
		c.addDedupEntry(ev, now)
		deduped = append(deduped, ev)
	}

	// 4. Route events: aggregatable go to buffers, others pass through.
	var output []Event
	for _, ev := range deduped {
		if aggregatableTypes[ev.Type] {
			buf, ok := c.aggBuffers[ev.Type]
			if !ok {
				buf = &aggBuffer{}
				c.aggBuffers[ev.Type] = buf
			}
			buf.events = append(buf.events, ev)
		} else {
			output = append(output, ev)
		}
	}

	// 5. Flush aggregation buffers that have aged past AggWindow.
	flushed := c.flushAggBuffers(now)
	output = append(output, flushed...)

	// 6. Write surviving events to curated feed.
	if len(output) > 0 {
		if err := c.appendToFeed(output); err != nil {
			return err
		}
	}

	// 7. Check feed size, truncate if needed.
	if err := c.truncateIfNeeded(); err != nil {
		return fmt.Errorf("feed truncation: %w", err)
	}

	// 8. Save checkpoint.
	c.saveCheckpoint()

	return nil
}

// readNewEvents reads events from the raw feed starting at the current offset.
func (c *Curator) readNewEvents() ([]Event, int64, error) {
	f, err := os.Open(c.config.RawPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, c.offset, nil
		}
		return nil, c.offset, err
	}
	defer f.Close()

	// Seek to offset.
	if _, err := f.Seek(c.offset, io.SeekStart); err != nil {
		return nil, c.offset, err
	}

	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue // skip malformed lines
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, c.offset, err
	}

	newOffset, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return events, c.offset, err
	}

	return events, newOffset, nil
}

// isDuplicate checks if the event matches a recent entry within DedupWindow.
func (c *Curator) isDuplicate(ev Event, now time.Time) bool {
	for _, entry := range c.dedupCache {
		if now.Sub(entry.SeenAt) > c.config.DedupWindow {
			continue
		}
		if entry.Type == ev.Type && entry.Source == ev.Source && entry.Actor == ev.Actor {
			return true
		}
	}
	return false
}

// addDedupEntry adds an event to the dedup cache.
func (c *Curator) addDedupEntry(ev Event, now time.Time) {
	c.dedupCache = append(c.dedupCache, dedupEntry{
		Type:   ev.Type,
		Source: ev.Source,
		Actor:  ev.Actor,
		SeenAt: now,
	})
}

// cleanDedupCache removes expired entries.
func (c *Curator) cleanDedupCache(now time.Time) {
	var kept []dedupEntry
	for _, entry := range c.dedupCache {
		if now.Sub(entry.SeenAt) <= c.config.DedupWindow {
			kept = append(kept, entry)
		}
	}
	c.dedupCache = kept
}

// flushAggBuffers emits aggregated or individual events for expired buffers.
func (c *Curator) flushAggBuffers(now time.Time) []Event {
	var result []Event

	for eventType, buf := range c.aggBuffers {
		if len(buf.events) == 0 {
			continue
		}

		// Check if the oldest event in the buffer is past the AggWindow.
		oldest := buf.events[0].Timestamp
		if now.Sub(oldest) < c.config.AggWindow {
			continue // not yet ready to flush
		}

		if len(buf.events) == 1 {
			// Single event — emit as-is.
			result = append(result, buf.events[0])
		} else {
			// Multiple events — emit aggregated event.
			first := buf.events[0]
			last := buf.events[len(buf.events)-1]
			result = append(result, Event{
				Timestamp:  last.Timestamp,
				Source:     "curator",
				Type:       eventType + "_batch",
				Actor:      "curator",
				Visibility: "feed",
				Payload: map[string]any{
					"type":           eventType,
					"count":          len(buf.events),
					"window_seconds": int(c.config.AggWindow.Seconds()),
					"first_ts":       first.Timestamp.Format(time.RFC3339),
					"last_ts":        last.Timestamp.Format(time.RFC3339),
				},
			})
		}

		delete(c.aggBuffers, eventType)
	}

	return result
}

// FlushAllAggBuffers forces all aggregation buffers to flush, regardless of window.
// Exported for testing.
func (c *Curator) FlushAllAggBuffers() error {
	var output []Event
	for eventType, buf := range c.aggBuffers {
		if len(buf.events) == 0 {
			continue
		}
		if len(buf.events) == 1 {
			output = append(output, buf.events[0])
		} else {
			first := buf.events[0]
			last := buf.events[len(buf.events)-1]
			output = append(output, Event{
				Timestamp:  last.Timestamp,
				Source:     "curator",
				Type:       eventType + "_batch",
				Actor:      "curator",
				Visibility: "feed",
				Payload: map[string]any{
					"type":           eventType,
					"count":          len(buf.events),
					"window_seconds": int(c.config.AggWindow.Seconds()),
					"first_ts":       first.Timestamp.Format(time.RFC3339),
					"last_ts":        last.Timestamp.Format(time.RFC3339),
				},
			})
		}
		delete(c.aggBuffers, eventType)
	}
	if len(output) > 0 {
		return c.appendToFeed(output)
	}
	return nil
}

// appendToFeed appends events to the curated feed file with flock.
func (c *Curator) appendToFeed(events []Event) error {
	f, err := os.OpenFile(c.config.FeedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open curated feed: %w", err)
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to lock curated feed: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			continue // skip unserializable events
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("failed to write to curated feed: %w", err)
		}
	}

	return nil
}

// truncateIfNeeded checks feed size and truncates if it exceeds MaxFeedSize.
// Repeats truncation until the file is within bounds.
func (c *Curator) truncateIfNeeded() error {
	for {
		info, err := os.Stat(c.config.FeedPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		if info.Size() <= c.config.MaxFeedSize {
			return nil
		}

		if err := c.truncateOnce(); err != nil {
			return err
		}
	}
}

// truncateOnce removes the first 25% of the curated feed.
func (c *Curator) truncateOnce() error {
	// Read entire file.
	data, err := os.ReadFile(c.config.FeedPath)
	if err != nil {
		return err
	}

	// Find the byte offset at 25% mark.
	cutoff := len(data) / 4

	// Find the next newline after the cutoff (don't split a JSON line).
	idx := cutoff
	for idx < len(data) && data[idx] != '\n' {
		idx++
	}
	if idx < len(data) {
		idx++ // skip the newline itself
	}

	remaining := data[idx:]

	// Write to temp file, then atomically rename.
	dir := filepath.Dir(c.config.FeedPath)
	tmp, err := os.CreateTemp(dir, ".feed-truncate-*.jsonl")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(remaining); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	// Acquire flock on the curated feed during rename.
	lockFile, err := os.OpenFile(c.config.FeedPath, os.O_RDONLY, 0644)
	if err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		lockFile.Close()
		os.Remove(tmpName)
		return err
	}

	err = os.Rename(tmpName, c.config.FeedPath)
	syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	lockFile.Close()

	if err != nil {
		os.Remove(tmpName)
		return err
	}

	return nil
}

// checkpointPath returns the path to the curator checkpoint file.
func (c *Curator) checkpointPath() string {
	dir := filepath.Dir(c.config.RawPath)
	return filepath.Join(dir, ".curator-checkpoint")
}

// loadCheckpoint reads the curator's byte offset from the checkpoint file.
func (c *Curator) loadCheckpoint() {
	data, err := os.ReadFile(c.checkpointPath())
	if err != nil {
		return // no checkpoint, start fresh
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return // corrupted checkpoint, ignore
	}
	c.offset = offset
}

// saveCheckpoint writes the curator's current byte offset to the checkpoint file.
// Uses temp-file-then-rename for crash safety.
func (c *Curator) saveCheckpoint() {
	tmp, err := os.CreateTemp(filepath.Dir(c.checkpointPath()), ".curator-checkpoint-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(strconv.FormatInt(c.offset, 10)); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	os.Rename(tmpName, c.checkpointPath())
}
