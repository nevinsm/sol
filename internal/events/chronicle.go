package events

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/chronicle"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/logutil"
)

// ChronicleConfig holds chronicle configuration.
type ChronicleConfig struct {
	RawPath      string        // path to raw .events.jsonl
	FeedPath     string        // path to curated .feed.jsonl
	PollInterval time.Duration // how often to check for new events (default: 2s)
	DedupWindow  time.Duration // dedup window for identical events (default: 10s)
	AggWindow    time.Duration // aggregation window for burst events (default: 30s)
	MaxFeedSize  int64         // max curated feed file size in bytes (default: 10MB)
	MaxRawSize   int64         // max raw events file size in bytes (default: 10MB)
}

// DefaultChronicleConfig returns defaults for the given SOL_HOME.
func DefaultChronicleConfig(solHome string) ChronicleConfig {
	return ChronicleConfig{
		RawPath:      filepath.Join(solHome, ".events.jsonl"),
		FeedPath:     filepath.Join(solHome, ".feed.jsonl"),
		PollInterval: 2 * time.Second,
		DedupWindow:  10 * time.Second,
		AggWindow:    30 * time.Second,
		MaxFeedSize:  10 * 1024 * 1024, // 10MB
		MaxRawSize:   10 * 1024 * 1024, // 10MB
	}
}

// Chronicle processes raw events into a curated feed.
type Chronicle struct {
	config     ChronicleConfig
	logger     *Logger
	offset     int64 // file offset — tracks position in raw feed
	dedupCache []dedupEntry
	aggBuffers map[string]*aggBuffer // keyed by event type

	eventsProcessed int64 // total events processed across all cycles
	cycleCount      int   // total processing cycles

	// testHookBeforeRotate, if non-nil, is invoked inside processCycle just
	// after the post-write offset commit and before raw-feed rotation. Tests
	// use it to deterministically simulate the race window where new events
	// arrive in the raw feed after read but before truncation. Production
	// code never sets this field.
	testHookBeforeRotate func()
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
	EventCast:    true,
	EventRespawn: true,
}

// NewChronicle creates a chronicle.
func NewChronicle(config ChronicleConfig, opts ...ChronicleOption) *Chronicle {
	c := &Chronicle{
		config:     config,
		aggBuffers: make(map[string]*aggBuffer),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ChronicleOption configures optional Chronicle settings.
type ChronicleOption func(*Chronicle)

// WithLogger sets the event logger on the Chronicle.
func WithLogger(logger *Logger) ChronicleOption {
	return func(c *Chronicle) {
		c.logger = logger
	}
}

// Run starts the chronicle loop. Blocks until context is cancelled.
func (c *Chronicle) Run(ctx context.Context) error {
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

	// Emit start event.
	if c.logger != nil {
		c.logger.Emit(EventChronicleStart, "chronicle", "chronicle", "audit",
			map[string]any{"checkpoint_offset": c.offset})
	}

	// Write initial heartbeat.
	c.writeHeartbeat("running")

	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()

	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	// Patrol summary every 5 minutes.
	patrolTicker := time.NewTicker(5 * time.Minute)
	defer patrolTicker.Stop()

	var consecutiveErrs int
	var patrolEventsProcessed int64

	for {
		select {
		case <-ctx.Done():
			c.FlushAllAggBuffers()
			c.saveCheckpoint()
			// Emit stop event.
			if c.logger != nil {
				c.logger.Emit(EventChronicleStop, "chronicle", "chronicle", "audit",
					map[string]any{
						"events_processed":  c.eventsProcessed,
						"checkpoint_offset": c.offset,
					})
			}
			// Write final heartbeat.
			c.writeHeartbeat("stopping")
			return nil
		case <-heartbeatTicker.C:
			c.writeHeartbeat("running")
		case <-patrolTicker.C:
			// Emit patrol summary.
			if c.logger != nil {
				processed := c.eventsProcessed - patrolEventsProcessed
				patrolEventsProcessed = c.eventsProcessed
				c.logger.Emit(EventChroniclePatrol, "chronicle", "chronicle", "feed",
					map[string]any{
						"events_processed":  processed,
						"total_processed":   c.eventsProcessed,
						"checkpoint_offset": c.offset,
						"cycles":            c.cycleCount,
					})
			}
		case <-ticker.C:
			if err := c.processCycle(); err != nil {
				consecutiveErrs++
				// Log on first error, then every 32 cycles to avoid spam.
				if consecutiveErrs == 1 || consecutiveErrs%32 == 0 {
					fmt.Fprintf(os.Stderr, "chronicle cycle error (count=%d): %v\n", consecutiveErrs, err)
					if c.logger != nil {
						c.logger.Emit(EventChronicleError, "chronicle", "chronicle", "audit",
							map[string]any{"error": err.Error(), "consecutive_count": consecutiveErrs})
					}
				}
			} else {
				consecutiveErrs = 0
			}
		}
	}
}

// writeHeartbeat writes the chronicle heartbeat file.
func (c *Chronicle) writeHeartbeat(status string) {
	hb := &chronicle.Heartbeat{
		Timestamp:        time.Now().UTC(),
		Status:           status,
		EventsProcessed:  c.eventsProcessed,
		CheckpointOffset: c.offset,
	}
	if err := chronicle.WriteHeartbeat(hb); err != nil {
		fmt.Fprintf(os.Stderr, "chronicle: failed to write heartbeat: %v\n", err)
	}
}

// ProcessOnce runs a single processing cycle. Exported for testing.
func (c *Chronicle) ProcessOnce() error {
	return c.processCycle()
}

// LoadCheckpoint loads the checkpoint. Exported for testing.
func (c *Chronicle) LoadCheckpoint() {
	c.loadCheckpoint()
}

// Offset returns the current read offset. Exported for testing.
func (c *Chronicle) Offset() int64 {
	return c.offset
}

// processCycle reads new events from the raw feed, filters/dedup/aggregates,
// and appends results to the curated feed.
func (c *Chronicle) processCycle() error {
	// 1. Read new lines from raw feed starting at offset.
	oldOffset := c.offset
	newEvents, newOffset, err := c.readNewEvents()
	if err != nil {
		return fmt.Errorf("failed to read new events: %w", err)
	}

	// 2. Filter: skip visibility="audit".
	var filtered []Event
	for _, ev := range newEvents {
		if ev.Visibility == "audit" {
			continue
		}
		filtered = append(filtered, ev)
	}

	// 3. Snapshot dedup + aggregation state BEFORE any cycle mutation, so
	// the entire cycle can be rolled back atomically on a feed-write failure.
	// Without snapshotting the full agg buffers, any events appended in
	// step 4 to non-expired buffers would survive the rollback and then be
	// re-aggregated on the next cycle (since the offset is not advanced),
	// causing inflated counts in the eventual batch event (CF-M17).
	now := time.Now()
	c.cleanDedupCache(now)
	dedupSnapshot := len(c.dedupCache)
	aggSnapshot := snapshotAggBuffers(c.aggBuffers)

	// 4. Dedup (skip aggregatable types — they get batched, not deduped).
	var deduped []Event
	for _, ev := range filtered {
		if aggregatableTypes[ev.Type] {
			deduped = append(deduped, ev)
			continue
		}
		if c.isDuplicate(ev, now) {
			continue
		}
		c.addDedupEntry(ev, now)
		deduped = append(deduped, ev)
	}

	// 5. Route events: aggregatable go to buffers, others pass through.
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

	// 6. Flush aggregation buffers that have aged past AggWindow.
	flushed := c.flushAggBuffers(now)
	output = append(output, flushed...)

	// 7. Write surviving events to curated feed.
	if len(output) > 0 {
		if err := c.appendToFeed(output); err != nil {
			// Roll back the entire cycle: agg buffers, dedup cache, offset.
			c.aggBuffers = aggSnapshot
			c.dedupCache = c.dedupCache[:dedupSnapshot]
			// Offset is not advanced — next cycle will re-read the same events.
			return fmt.Errorf("failed to append to curated feed: %w", err)
		}
	}

	// 8. Commit offset now that the write succeeded.
	c.offset = newOffset

	// 9. Track events processed.
	c.eventsProcessed += int64(len(newEvents))
	c.cycleCount++

	// 10. Check feed size, truncate if needed.
	if err := c.truncateIfNeeded(); err != nil {
		return fmt.Errorf("feed truncation: %w", err)
	}

	// 11. Best-effort log rotation — chronicle's own log and raw event feed.
	logutil.TruncateIfNeeded(filepath.Join(config.RuntimeDir(), "chronicle.log"), logutil.DefaultMaxLogSize) //nolint:errcheck
	rawMaxSize := c.config.MaxRawSize
	if rawMaxSize <= 0 {
		rawMaxSize = logutil.DefaultMaxLogSize
	}
	// Test seam: lets tests deterministically inject the read-vs-rotation
	// race that CF-M18 describes. Production sets this to nil.
	if c.testHookBeforeRotate != nil {
		c.testHookBeforeRotate()
	}

	// savedOffset is the chronicle's current read position after the cycle's
	// committed offset advance. If the upcoming rotation moves the file's
	// tail past this point, the events in [savedOffset, tailStart) are gone
	// and must be surfaced as a chronicle_dropped event.
	savedOffset := c.offset
	rawTruncated := false

	// Pre-read the unprocessed tail (bytes from savedOffset to EOF) so that
	// if the rotation drops events, we can count them and emit a
	// chronicle_dropped event. Without this pre-read, the rotation clamp at
	// max(0, savedOffset-tailStart) would silently lose events (CF-M18).
	preTruncTail := c.snapshotRawTail(savedOffset, rawMaxSize)

	if truncated, tailStart, _ := logutil.TruncateIfNeeded(c.config.RawPath, rawMaxSize); truncated {
		// The new file starts at tailStart bytes into the original file.
		// Unprocessed events start at savedOffset in the original, which maps
		// to savedOffset-tailStart in the new file. If savedOffset is before
		// tailStart (file was much larger than offset), the events in
		// [savedOffset, tailStart) are gone — we must surface this loss.
		if savedOffset < tailStart {
			droppedBytes := tailStart - savedOffset
			droppedCount := 0
			if int64(len(preTruncTail)) >= droppedBytes {
				droppedCount = bytes.Count(preTruncTail[:droppedBytes], []byte("\n"))
			} else if len(preTruncTail) > 0 {
				// Best effort: tail file may have shrunk between snapshot
				// and truncation. Count whatever we managed to capture.
				droppedCount = bytes.Count(preTruncTail, []byte("\n"))
			}
			if c.logger != nil {
				c.logger.Emit(EventChronicleDropped, "chronicle", "chronicle", "both",
					map[string]any{
						"dropped_count": droppedCount,
						"dropped_bytes": droppedBytes,
						"reason":        "raw_feed_rotation",
						"saved_offset":  savedOffset,
						"tail_start":    tailStart,
					})
			}
			fmt.Fprintf(os.Stderr, "chronicle: raw-feed rotation dropped %d events (%d bytes)\n",
				droppedCount, droppedBytes)
		}
		c.offset = max(0, savedOffset-tailStart)
		rawTruncated = true
	}

	// 11. Save checkpoint only when new events were read or the raw file was
	// rotated — avoid redundant atomic writes during idle periods.
	if newOffset != oldOffset || rawTruncated {
		c.saveCheckpoint()
	}

	return nil
}

// readNewEvents reads events from the raw feed starting at the current offset.
//
// Uses a bufio.Reader with ReadString('\n') rather than bufio.Scanner so that
// arbitrarily long lines do not cause the read to stall (CF-L5). Only complete
// lines (terminated by '\n') advance the returned offset — a partial trailing
// line is left for the next cycle, which handles the case where an event is
// being written concurrently.
func (c *Chronicle) readNewEvents() ([]Event, int64, error) {
	f, err := os.Open(c.config.RawPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, c.offset, nil
		}
		return nil, c.offset, fmt.Errorf("failed to open raw events file: %w", err)
	}
	defer f.Close()

	if _, err := f.Seek(c.offset, io.SeekStart); err != nil {
		return nil, c.offset, fmt.Errorf("failed to seek raw events file: %w", err)
	}

	var events []Event
	br := bufio.NewReader(f)
	var bytesRead int64
	for {
		line, readErr := br.ReadString('\n')
		if readErr == nil {
			// Complete line terminated by '\n'. Advance offset past it.
			bytesRead += int64(len(line))
			trimmed := strings.TrimRight(line, "\n")
			if trimmed == "" {
				continue
			}
			var ev Event
			if err := json.Unmarshal([]byte(trimmed), &ev); err != nil {
				// Malformed line — skip and warn. We still advance the offset
				// (via bytesRead above), so a single bad line cannot stall the
				// chronicle. This is the one acceptable silent drop documented
				// in ADR-0004; we surface it via stderr.
				fmt.Fprintf(os.Stderr, "chronicle: skipping malformed raw event line at offset %d: %v\n",
					c.offset+bytesRead-int64(len(line)), err)
				continue
			}
			events = append(events, ev)
			continue
		}
		if errors.Is(readErr, io.EOF) {
			// Partial trailing line (no '\n') — do NOT advance past it.
			// Next cycle will re-read once the writer completes the line.
			break
		}
		return events, c.offset + bytesRead, fmt.Errorf("failed to read raw events: %w", readErr)
	}

	return events, c.offset + bytesRead, nil
}

// snapshotAggBuffers returns a deep copy of the aggregation buffers map so
// that the cycle can be rolled back atomically on a write failure.
func snapshotAggBuffers(src map[string]*aggBuffer) map[string]*aggBuffer {
	dst := make(map[string]*aggBuffer, len(src))
	for k, buf := range src {
		copied := make([]Event, len(buf.events))
		copy(copied, buf.events)
		dst[k] = &aggBuffer{events: copied}
	}
	return dst
}

// snapshotRawTail reads the bytes of the raw feed from savedOffset to EOF,
// but only if the file size exceeds rawMaxSize (i.e. a rotation is imminent).
// Returns nil on any error — this is a best-effort capture used to count
// events that would otherwise be silently dropped by raw-feed rotation.
func (c *Chronicle) snapshotRawTail(savedOffset, rawMaxSize int64) []byte {
	info, err := os.Stat(c.config.RawPath)
	if err != nil || info.Size() <= rawMaxSize {
		return nil
	}
	f, err := os.Open(c.config.RawPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	if _, err := f.Seek(savedOffset, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	return data
}

// isDuplicate checks if the event matches a recent entry within DedupWindow.
func (c *Chronicle) isDuplicate(ev Event, now time.Time) bool {
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
func (c *Chronicle) addDedupEntry(ev Event, now time.Time) {
	c.dedupCache = append(c.dedupCache, dedupEntry{
		Type:   ev.Type,
		Source: ev.Source,
		Actor:  ev.Actor,
		SeenAt: now,
	})
}

// cleanDedupCache removes expired entries.
func (c *Chronicle) cleanDedupCache(now time.Time) {
	var kept []dedupEntry
	for _, entry := range c.dedupCache {
		if now.Sub(entry.SeenAt) <= c.config.DedupWindow {
			kept = append(kept, entry)
		}
	}
	c.dedupCache = kept
}

// flushAggBuffers emits aggregated or individual events for expired buffers.
func (c *Chronicle) flushAggBuffers(now time.Time) []Event {
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
				Source:     "chronicle",
				Type:       eventType + "_batch",
				Actor:      "chronicle",
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
func (c *Chronicle) FlushAllAggBuffers() error {
	var output []Event
	var flushedTypes []string
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
				Source:     "chronicle",
				Type:       eventType + "_batch",
				Actor:      "chronicle",
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
		flushedTypes = append(flushedTypes, eventType)
	}
	if len(output) > 0 {
		if err := c.appendToFeed(output); err != nil {
			return err
		}
	}
	for _, eventType := range flushedTypes {
		delete(c.aggBuffers, eventType)
	}
	return nil
}

// appendToFeed appends events to the curated feed file with flock.
func (c *Chronicle) appendToFeed(events []Event) error {
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

	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync curated feed: %w", err)
	}

	return nil
}

// truncateIfNeeded checks feed size and truncates if it exceeds MaxFeedSize.
// Repeats truncation until the file is within bounds.
func (c *Chronicle) truncateIfNeeded() error {
	for {
		info, err := os.Stat(c.config.FeedPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("failed to stat feed file: %w", err)
		}

		if info.Size() <= c.config.MaxFeedSize {
			return nil
		}

		if err := c.truncateOnce(); err != nil {
			return fmt.Errorf("failed to truncate feed: %w", err)
		}
	}
}

// truncateOnce removes the first 25% of the curated feed.
func (c *Chronicle) truncateOnce() error {
	// Acquire flock FIRST to prevent event loss from concurrent appendToFeed writes.
	lockFile, err := os.OpenFile(c.config.FeedPath, os.O_RDONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open feed file for locking: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to lock feed file: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	// Read entire file under the lock so no events are missed.
	data, err := os.ReadFile(c.config.FeedPath)
	if err != nil {
		return fmt.Errorf("failed to read feed file: %w", err)
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
		return fmt.Errorf("failed to create temp file for truncation: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(remaining); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to write truncated feed: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to sync truncated feed: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpName, c.config.FeedPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to rename truncated feed: %w", err)
	}

	return nil
}

// checkpointPath returns the path to the chronicle checkpoint file.
func (c *Chronicle) checkpointPath() string {
	dir := filepath.Dir(c.config.RawPath)
	return filepath.Join(dir, ".chronicle-checkpoint")
}

// loadCheckpoint reads the chronicle's byte offset from the checkpoint file.
func (c *Chronicle) loadCheckpoint() {
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

// saveCheckpoint writes the chronicle's current byte offset to the checkpoint file.
// Uses temp-file-then-rename for crash safety.
func (c *Chronicle) saveCheckpoint() {
	data := []byte(strconv.FormatInt(c.offset, 10))
	if err := fileutil.AtomicWrite(c.checkpointPath(), data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "chronicle: failed to save checkpoint: %v\n", err)
	}
}
