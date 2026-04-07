package events

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReadOpts controls event filtering and limiting.
type ReadOpts struct {
	Limit  int       // max events to return (0 = unlimited)
	Since  time.Time // only events after this time (zero = all)
	Type   string    // filter by event type (empty = all)
	Source string    // filter by source (empty = all)
	Actor  string    // filter by actor (empty = all)
}

// Reader reads events from the JSONL event feed.
type Reader struct {
	path string
}

// NewReader creates an event feed reader.
// If curated=true, reads from .feed.jsonl (curated feed).
// If curated=false, reads from .events.jsonl (raw feed).
func NewReader(solHome string, curated bool) *Reader {
	filename := ".events.jsonl"
	if curated {
		filename = ".feed.jsonl"
	}
	return &Reader{
		path: filepath.Join(solHome, filename),
	}
}

// Read returns events from the feed, with optional filtering.
// Returns events in chronological order.
// When Limit > 0, returns only the last N matching events (tail semantics).
// Events with visibility="audit" are excluded (only "feed" and "both" are shown).
func (r *Reader) Read(opts ReadOpts) ([]Event, error) {
	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []Event
	br := bufio.NewReader(f)
	for {
		line, readErr := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\n")
			if trimmed != "" {
				var ev Event
				if jerr := json.Unmarshal([]byte(trimmed), &ev); jerr == nil {
					if matchEvent(ev, opts) {
						events = append(events, ev)
					}
				}
				// malformed lines are skipped silently here (Read is a
				// best-effort historical view; chronicle is the source of
				// truth for drop accounting)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, readErr
		}
	}

	// Tail semantics: return only the last N events.
	if opts.Limit > 0 && len(events) > opts.Limit {
		events = events[len(events)-opts.Limit:]
	}

	return events, nil
}

// Follow opens the feed for tailing (like tail -f).
// Sends events to the channel as they appear.
// Blocks until the context is cancelled.
func (r *Reader) Follow(ctx context.Context, opts ReadOpts, ch chan<- Event) error {
	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Wait for file to appear.
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(500 * time.Millisecond):
					f, err = os.Open(r.path)
					if err == nil {
						goto opened
					}
				}
			}
		}
		return err
	}
opened:
	defer func() { f.Close() }()

	// Seek to end to only get new events.
	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Detect file replacement (e.g., chronicle truncation).
			// The chronicle atomically renames a new file over the feed path.
			// The open fd still points to the old (unlinked) inode and would
			// never see new events without reopening.
			pathInfo, pathErr := os.Stat(r.path)
			fdInfo, fdErr := f.Stat()
			if pathErr == nil && fdErr == nil && !os.SameFile(pathInfo, fdInfo) {
				newF, err := os.Open(r.path)
				if err != nil {
					continue // file may be temporarily unavailable during rename
				}
				// Seek to end of the new file so we only deliver events written
				// after rotation. Without this, events already in the new file
				// (e.g. the tail kept by chronicle's truncateOnce) would be
				// re-delivered to the caller.
				newOffset, err := newF.Seek(0, io.SeekEnd)
				if err != nil {
					newF.Close()
					continue
				}
				f.Close()
				f = newF
				offset = newOffset
			}

			info, err := f.Stat()
			if err != nil {
				continue
			}
			if info.Size() <= offset {
				continue
			}

			// Read new data from last offset.
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				continue
			}

			// Use bufio.Reader + ReadString('\n') so a single oversize line
			// cannot stall the follow loop (CF-L5). Only complete lines
			// (terminated by '\n') advance the offset; a partial trailing
			// line is left for the next poll tick.
			br := bufio.NewReader(f)
			var consumed int64
			breakLoop := false
			for {
				line, readErr := br.ReadString('\n')
				if readErr == nil {
					consumed += int64(len(line))
					trimmed := strings.TrimRight(line, "\n")
					if trimmed == "" {
						continue
					}
					var ev Event
					if jerr := json.Unmarshal([]byte(trimmed), &ev); jerr != nil {
						fmt.Fprintf(os.Stderr, "events.Reader.Follow: skipping malformed line: %v\n", jerr)
						continue
					}
					if !matchEvent(ev, opts) {
						continue
					}
					select {
					case ch <- ev:
					case <-ctx.Done():
						return ctx.Err()
					}
					continue
				}
				if errors.Is(readErr, io.EOF) {
					// Partial trailing line — leave it unread for next tick.
					break
				}
				// Unexpected error — log and skip this tick. Don't advance.
				fmt.Fprintf(os.Stderr, "events.Reader.Follow: read error: %v\n", readErr)
				breakLoop = true
				break
			}
			if breakLoop {
				continue
			}

			// Advance offset by the bytes we successfully consumed (complete
			// lines only).
			offset += consumed
		}
	}
}

// matchEvent checks if an event matches the read filters.
func matchEvent(ev Event, opts ReadOpts) bool {
	// Filter out audit-only events from feed reads.
	if ev.Visibility == "audit" {
		return false
	}
	if !opts.Since.IsZero() && ev.Timestamp.Before(opts.Since) {
		return false
	}
	if opts.Type != "" && ev.Type != opts.Type {
		return false
	}
	if opts.Source != "" && ev.Source != opts.Source {
		return false
	}
	if opts.Actor != "" && ev.Actor != opts.Actor {
		return false
	}
	return true
}
