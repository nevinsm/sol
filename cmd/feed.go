package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
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

		if feedSince != "" {
			dur, err := time.ParseDuration(feedSince)
			if err != nil {
				return fmt.Errorf("invalid --since duration %q: %w", feedSince, err)
			}
			opts.Since = time.Now().Add(-dur)
		}

		if feedFollow {
			return followFeed(reader, opts)
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

func followFeed(reader *events.Reader, opts events.ReadOpts) error {
	ctx, cancel := context.WithCancel(context.Background())
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
			if err == context.Canceled {
				return nil
			}
			return err
		}
	}
}

func printEvent(ev events.Event) {
	if feedJSON {
		data, err := json.Marshal(ev)
		if err != nil {
			return
		}
		fmt.Println(string(data))
		return
	}

	ts := ev.Timestamp.Local().Format("15:04:05")
	desc := formatEventDescription(ev)
	fmt.Printf("[%s] %-12s %-12s %s\n", ts, ev.Type, ev.Actor, desc)
}

func formatEventDescription(ev events.Event) string {
	payload, ok := ev.Payload.(map[string]any)
	if !ok {
		return ""
	}

	get := func(key string) string {
		if v, ok := payload[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}

	switch ev.Type {
	case events.EventCast:
		return fmt.Sprintf("Dispatched %s → %s (%s)", get("work_item_id"), get("agent"), get("world"))
	case events.EventResolve:
		return fmt.Sprintf("Completed %s", get("work_item_id"))
	case events.EventMergeClaimed:
		return fmt.Sprintf("Claimed %s for merge", get("merge_request_id"))
	case events.EventMerged:
		return fmt.Sprintf("Merged %s to main", get("merge_request_id"))
	case events.EventMergeFailed:
		return fmt.Sprintf("Merge failed %s", get("merge_request_id"))
	case events.EventRespawn:
		return fmt.Sprintf("Respawned %s (%s)", get("agent"), get("rig"))
	case events.EventMassDeath:
		return fmt.Sprintf("Mass death: %s deaths in %s", get("deaths"), get("window"))
	case events.EventDegraded:
		return "Entered degraded mode"
	case events.EventRecovered:
		return "Exited degraded mode"
	case events.EventPatrol:
		return fmt.Sprintf("Patrol complete (%s)", get("rig"))
	case events.EventStalled:
		return fmt.Sprintf("Agent stalled: %s", get("agent"))
	case events.EventMailSent:
		return fmt.Sprintf("Message sent to %s", get("recipient"))
	case events.EventAssess:
		return fmt.Sprintf("Assessed %s: %s (%s confidence)", get("agent"), get("status"), get("confidence"))
	case events.EventNudge:
		return fmt.Sprintf("Nudged %s: %s", get("agent"), get("message"))
	case events.EventWorkflowInstantiate:
		return fmt.Sprintf("Workflow %s instantiated for %s", get("formula"), get("work_item_id"))
	case events.EventWorkflowAdvance:
		return fmt.Sprintf("Advanced to step: %s (%s)", get("step"), get("work_item_id"))
	case events.EventWorkflowComplete:
		return fmt.Sprintf("Workflow complete: %s", get("work_item_id"))
	case events.EventCaravanCreated:
		return fmt.Sprintf("Caravan created: %s (%s items)", get("name"), get("count"))
	case events.EventCaravanLaunched:
		return fmt.Sprintf("Caravan launched: %s dispatched in %s", get("dispatched"), get("world"))
	case events.EventCaravanClosed:
		return fmt.Sprintf("Caravan closed: %s", get("name"))
	case events.EventEscalationCreated:
		return fmt.Sprintf("[%s] Escalation: %s (from %s)", get("severity"), get("description"), get("source"))
	case events.EventEscalationAcked:
		return fmt.Sprintf("Escalation acknowledged: %s", get("id"))
	case events.EventEscalationResolved:
		return fmt.Sprintf("Escalation resolved: %s", get("id"))
	case events.EventHandoff:
		return fmt.Sprintf("Agent %s handed off: %s", get("agent"), get("work_item_id"))
	case events.EventConsulPatrol:
		return fmt.Sprintf("Consul patrol #%s: %s stale tethers, %s caravan feeds",
			get("patrol_count"), get("stale_tethers"), get("caravan_feeds"))
	case events.EventConsulStaleTether:
		return fmt.Sprintf("Stale tether recovered: %s (%s)", get("agent_id"), get("work_item_id"))
	case events.EventConsulCaravanFeed:
		return fmt.Sprintf("Caravan needs feeding: %s (%s ready items)", get("caravan_id"), get("ready_count"))
	case "cast_batch":
		return fmt.Sprintf("Cast burst: %s dispatches in %s", get("count"), get("world"))
	case "respawn_batch":
		return fmt.Sprintf("Respawn burst: %s respawns in %s", get("count"), get("rig"))
	default:
		data, _ := json.Marshal(payload)
		return string(data)
	}
}

func init() {
	rootCmd.AddCommand(feedCmd)
	feedCmd.Flags().BoolVarP(&feedFollow, "follow", "f", false, "tail mode — stream events as they appear")
	feedCmd.Flags().IntVarP(&feedLimit, "limit", "n", 20, "show only the last N events")
	feedCmd.Flags().StringVar(&feedSince, "since", "", "show events from the last duration (e.g., 1h, 30m)")
	feedCmd.Flags().StringVar(&feedType, "type", "", "filter by event type")
	feedCmd.Flags().BoolVar(&feedJSON, "json", false, "output raw JSONL")
	feedCmd.Flags().BoolVar(&feedRaw, "raw", false, "read raw event log instead of curated feed")
}
