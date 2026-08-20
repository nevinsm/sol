package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	clievents "github.com/nevinsm/sol/internal/cliapi/events"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/eventformat"
	"github.com/nevinsm/sol/internal/events"
	"github.com/spf13/cobra"
)

var (
	feedFollow bool
	feedLimit  int
	feedSince  string
	feedType   string
	feedJSON   bool
	feedRaw    bool
)

var feedCmd = &cobra.Command{
	Use:   "feed",
	Short: "View the event activity feed",
	Long: `View the event activity feed.

--since accepts either a duration ("1h", "30m" — events from that far back)
or an opaque cursor token from a previous --json --since read's
"next_cursor" field. Cursor mode implements the external automation
contract in ADR-0043 decision 2: a resumable, lossless incremental read.
With a cursor, --since requires --json and cannot be combined with
--follow; the output is a single JSON object ({"events": [...],
"next_cursor": "..."}) instead of one JSON line per event. An increment
with no new events returns an empty "events" array and the same (or an
advanced) "next_cursor" — that is not an error.

A consumer with no prior cursor enters the contract with "sol feed --json
--since=''" (an explicitly empty --since, not an omitted one — plain "sol
feed --json" with --since left off is unaffected and keeps returning one
JSON line per event, no cursor involved). That bootstrap call reads like
--limit/--type/--raw say and returns the same {"events": [...],
"next_cursor": "..."} envelope, seeded from the current tail; save the
returned "next_cursor" and pass it back as --since=<cursor> from then on.

The cursor is opaque: do not parse or construct it, only pass back what a
previous read returned. If the referenced event can no longer be found in
the feed (most commonly because chronicle rotated it out of retention —
both the raw and curated feed files rotate by truncating their head in
place, so a dropped event is gone for good), the read fails; there is no
partial-recovery path, restart with --json --since='' for a fresh cursor
from the current tail.

Exit codes:
  0 - Read succeeded (including an empty increment)
  1 - Invalid --since value (bad duration, or a cursor that cannot be
      decoded or whose event has rotated out of the feed), or another error`,
	GroupID:      groupCommunication,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default: curated feed. --raw: raw event log.
		// If curated feed doesn't exist, fall back to raw silently.
		curated := !feedRaw
		if curated {
			feedPath := config.Home() + "/.feed.jsonl"
			if _, err := os.Stat(feedPath); os.IsNotExist(err) {
				curated = false
			}
		}
		reader := events.NewReader(config.Home(), curated)

		opts := events.ReadOpts{
			Limit: feedLimit,
			Type:  feedType,
		}

		// Cursor mode covers two cases: an incoming token from a prior read
		// (feedSince holds "sc1:..."), or an explicit "--since=''" bootstrap
		// request (the flag was set, but to the empty string) — distinct
		// from --since simply being left off, which keeps the plain
		// human/JSONL read below. cmd.Flags().Changed distinguishes the two
		// since the empty string can't be told apart from the zero value
		// any other way.
		cursorBootstrap := feedSince == "" && cmd.Flags().Changed("since")
		if (feedSince != "" && events.IsCursor(feedSince)) || cursorBootstrap {
			if feedFollow {
				return errors.New("feed: --since=<cursor> cannot be combined with --follow")
			}
			if !feedJSON {
				return errors.New("feed: --since=<cursor> requires --json")
			}
			return runFeedSince(reader, feedSince, opts)
		}

		if feedSince != "" {
			dur, err := time.ParseDuration(feedSince)
			if err != nil {
				return fmt.Errorf("invalid --since duration %q: %w", feedSince, err)
			}
			opts.Since = time.Now().Add(-dur)
		}

		if feedFollow {
			return followFeed(cmd.Context(), reader, opts)
		}

		evts, err := reader.Read(opts)
		if err != nil {
			return err
		}

		for _, ev := range evts {
			printEvent(ev)
		}
		return nil
	},
}

// sincePage is the wire shape of a cursor-based --json --since read: the new
// events plus the cursor to pass on the next call. See ADR-0043 decision 2.
type sincePage struct {
	Events     []clievents.Event `json:"events"`
	NextCursor string            `json:"next_cursor"`
}

// runFeedSince handles the --since=<cursor> --json path: a resumable,
// lossless incremental read, returned as a single JSON object rather than
// the human/JSONL streaming output the rest of this command produces.
func runFeedSince(reader *events.Reader, cursor string, opts events.ReadOpts) error {
	page, err := reader.ReadSince(cursor, opts)
	if err != nil {
		if errors.Is(err, events.ErrInvalidCursor) {
			return fmt.Errorf("feed: %w — restart with --json --since='' for a fresh cursor", err)
		}
		return err
	}

	out := sincePage{
		// Non-nil even when empty so the JSON field is "[]", never "null" —
		// an empty increment is a normal, successful outcome for a
		// scripting consumer and must not look like a missing field.
		Events:     make([]clievents.Event, 0, len(page.Events)),
		NextCursor: page.NextCursor,
	}
	for _, ev := range page.Events {
		out.Events = append(out.Events, clievents.FromEvent(ev))
	}

	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("feed: failed to marshal cursor page: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func followFeed(ctx context.Context, reader *events.Reader, opts events.ReadOpts) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sigCh; cancel() }()

	ch := make(chan events.Event, 64)
	errCh := make(chan error, 1)
	go func() { errCh <- reader.Follow(ctx, opts, ch) }()

	for {
		select {
		case ev := <-ch:
			printEvent(ev)
		case err := <-errCh:
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
}

func printEvent(ev events.Event) {
	if feedJSON {
		data, err := json.Marshal(clievents.FromEvent(ev))
		if err != nil {
			fmt.Fprintf(os.Stderr, "feed: failed to marshal event (type=%s): %v\n", ev.Type, err)
			return
		}
		fmt.Println(string(data))
		return
	}

	ts := ev.Timestamp.Local().Format("15:04:05")
	desc := formatEventDescription(ev)
	fmt.Printf("[%s] %-12s %-12s %s\n", ts, ev.Type, ev.Actor, desc)
}

// formatEventDescription composes the human-readable description column of
// `sol feed`'s plain-text output from the shared eventformat mapping (verb
// + detail), keeping sol feed's own layout ("<verb> <detail>") intact.
// Wording comes from eventformat.Verb/Detail — the single source of truth
// shared with sol dash's activity feed — rather than a second, independently
// maintained set of per-type sentences.
func formatEventDescription(ev events.Event) string {
	verb := eventformat.Verb(ev.Type)
	detail := eventformat.Detail(ev)
	if detail == "" {
		return verb
	}
	return fmt.Sprintf("%s %s", verb, detail)
}

func init() {
	rootCmd.AddCommand(feedCmd)
	feedCmd.Flags().BoolVarP(&feedFollow, "follow", "f", false, "tail mode — stream events as they appear")
	feedCmd.Flags().IntVarP(&feedLimit, "limit", "n", 20, "show only the last N events")
	feedCmd.Flags().StringVar(&feedSince, "since", "", "duration (e.g., 1h, 30m); a cursor from a prior --json --since read's next_cursor; or '' (explicitly, with --json) to bootstrap a fresh cursor")
	feedCmd.Flags().StringVar(&feedType, "type", "", "filter by event type")
	feedCmd.Flags().BoolVar(&feedJSON, "json", false, "output raw JSONL")
	feedCmd.Flags().BoolVar(&feedRaw, "raw", false, "read raw event log instead of curated feed")
}
