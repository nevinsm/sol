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

// SincePage is the result of a cursor-based incremental read: the new
// events since the given cursor, and the cursor to pass on the next call.
type SincePage struct {
	Events     []Event
	NextCursor string
}

// ReadSince returns events after the given cursor, along with a next cursor
// to pass on the following call. It implements decision 2 of ADR-0043: a
// resumable, lossless read for external consumers that cannot tail sol's
// event files directly.
//
// cursor == "" is the bootstrap case: it reads exactly like Read(opts)
// (including Limit's existing tail-truncation — the most recent N matches),
// and returns a NextCursor positioned after the last event in that page so
// the following call picks up where this one left off. Consumers that want
// full history from the start should pass a large Limit (or 0) on the
// bootstrap call.
//
// For a non-empty cursor, unlike Read's tail-truncation, a positive
// opts.Limit here caps the *head* of the new-events page: it stops at the
// Nth new match and returns NextCursor pointing at that match, rather than
// jumping to the newest N and skipping the rest. Repeated calls therefore
// drain any backlog instead of silently losing events — this is what makes
// the read lossless.
//
// Returns an error wrapping ErrInvalidCursor if the cursor cannot be
// decoded, or if its event can no longer be located in the feed being read
// (most commonly because chronicle's raw-feed or curated-feed rotation
// dropped it — both rotate by truncating the head of the file in place, so
// an event once rotated out is gone for good). There is no partial recovery
// from that state: callers should restart from a fresh (empty) cursor.
func (r *Reader) ReadSince(cursor string, opts ReadOpts) (SincePage, error) {
	if cursor == "" {
		evts, err := r.Read(opts)
		if err != nil {
			return SincePage{}, err
		}
		page := SincePage{Events: evts}
		if len(evts) > 0 {
			last := evts[len(evts)-1]
			page.NextCursor = EncodeCursor(Cursor{ID: EventID(last), UnixNano: last.Timestamp.UnixNano()})
		}
		return page, nil
	}

	want, err := DecodeCursor(cursor)
	if err != nil {
		return SincePage{}, err
	}

	f, err := os.Open(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			// The consumer holds a cursor pointing at real history, but
			// there is no feed file at all. Treat as expired rather than
			// silently resetting — silently resetting would mask event
			// loss as if it were a normal empty increment.
			return SincePage{}, fmt.Errorf("%w: feed file does not exist", ErrInvalidCursor)
		}
		return SincePage{}, err
	}
	defer f.Close()

	var (
		found     bool
		haveFirst bool
		firstTS   time.Time
		haveLast  bool
		lastID    string
		lastTS    time.Time
		out       []Event
	)

	br := bufio.NewReader(f)
	for {
		line, readErr := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\n")
			if trimmed != "" {
				var ev Event
				if jerr := json.Unmarshal([]byte(trimmed), &ev); jerr == nil {
					if !haveFirst {
						firstTS = ev.Timestamp
						haveFirst = true
					}
					id := EventID(ev)
					lastID = id
					lastTS = ev.Timestamp
					haveLast = true

					if !found {
						if id == want.ID {
							found = true
						}
					} else if matchEvent(ev, opts) {
						out = append(out, ev)
						if opts.Limit > 0 && len(out) >= opts.Limit {
							return SincePage{
								Events:     out,
								NextCursor: EncodeCursor(Cursor{ID: id, UnixNano: ev.Timestamp.UnixNano()}),
							}, nil
						}
					}
				}
				// malformed lines are skipped silently, same as Read.
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return SincePage{}, readErr
		}
	}

	if !found {
		if !haveFirst {
			return SincePage{}, fmt.Errorf("%w: feed is empty", ErrInvalidCursor)
		}
		if want.UnixNano < firstTS.UnixNano() {
			return SincePage{}, fmt.Errorf("%w: event has rotated out of the retained feed", ErrInvalidCursor)
		}
		return SincePage{}, ErrInvalidCursor
	}

	page := SincePage{Events: out}
	if haveLast {
		page.NextCursor = EncodeCursor(Cursor{ID: lastID, UnixNano: lastTS.UnixNano()})
	} else {
		// Unreachable in practice — found implies at least one line was
		// read — but fall back to echoing the input cursor rather than
		// an empty token.
		page.NextCursor = cursor
	}
	return page, nil
}

// rotationFreshWindow is the mtime threshold used by the rotation handler in
// Follow. If the replacement file is newer than this window, seek to the start
// to capture events appended between the rename and the next poll tick.
// If it is older, seek to the end to avoid re-delivering the tail that
// chronicle preserves during truncation.
const rotationFreshWindow = 2 * time.Second

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
				// Rotation seek strategy: if the new file was modified recently
				// (within rotationFreshWindow of now), seek to the beginning so
				// any events appended between the rename and this tick are
				// delivered. If the file is older, seek to the end to avoid
				// re-delivering the tail that chronicle preserved during
				// truncation.
				var newOffset int64
				if newFInfo, infoErr := newF.Stat(); infoErr == nil &&
					time.Since(newFInfo.ModTime()) < rotationFreshWindow {
					// Fresh rotation — start from beginning.
					newOffset = 0
				} else {
					// Stale file — skip preserved tail to avoid re-delivery.
					newOffset, err = newF.Seek(0, io.SeekEnd)
					if err != nil {
						newF.Close()
						continue
					}
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
