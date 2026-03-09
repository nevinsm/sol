package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/spf13/cobra"
)

var (
	handoffsWorld string
	handoffsAgent string
	handoffsLast  int
	handoffsJSON  bool
)

var agentHandoffsCmd = &cobra.Command{
	Use:          "handoffs",
	Short:        "Show recent handoff events",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(handoffsWorld)
		if err != nil {
			return err
		}

		reader := events.NewReader(config.Home(), false)
		opts := events.ReadOpts{
			Type: events.EventHandoff,
		}
		if handoffsAgent != "" {
			opts.Actor = handoffsAgent
		}
		if handoffsLast > 0 {
			opts.Limit = handoffsLast
		} else {
			opts.Limit = 20
		}

		allEvents, err := reader.Read(opts)
		if err != nil {
			return fmt.Errorf("failed to read events: %w", err)
		}

		// Filter by world from payload.
		var filtered []events.Event
		for _, ev := range allEvents {
			payload, ok := payloadMap(ev.Payload)
			if !ok {
				continue
			}
			if w, _ := payload["world"].(string); w == world {
				filtered = append(filtered, ev)
			}
		}

		if handoffsJSON {
			if filtered == nil {
				filtered = []events.Event{}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(filtered)
		}

		if len(filtered) == 0 {
			fmt.Println("No handoff events found.")
			return nil
		}

		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "AGENT\tREASON\tSESSION AGE\tWRIT\tTIME\n")

		for _, ev := range filtered {
			payload, _ := payloadMap(ev.Payload)
			agent := stringVal(payload, "agent", ev.Actor)
			reason := stringVal(payload, "reason", "-")
			sessionAge := stringVal(payload, "session_age", "-")
			writ := stringVal(payload, "writ_id", "-")
			timeAgo := formatTimeAgo(ev.Timestamp)

			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", agent, reason, sessionAge, writ, timeAgo)
		}
		tw.Flush()
		return nil
	},
}

// payloadMap extracts the payload as a map[string]any.
func payloadMap(payload any) (map[string]any, bool) {
	switch p := payload.(type) {
	case map[string]any:
		return p, true
	case map[string]string:
		m := make(map[string]any, len(p))
		for k, v := range p {
			m[k] = v
		}
		return m, true
	default:
		return nil, false
	}
}

// stringVal extracts a string value from a map with a fallback.
func stringVal(m map[string]any, key, fallback string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

// formatTimeAgo returns a human-readable relative time string.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d min ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func init() {
	agentCmd.AddCommand(agentHandoffsCmd)
	agentHandoffsCmd.Flags().StringVar(&handoffsWorld, "world", "", "world name")
	agentHandoffsCmd.Flags().StringVar(&handoffsAgent, "agent", "", "filter by agent name")
	agentHandoffsCmd.Flags().IntVar(&handoffsLast, "last", 20, "number of recent events to show")
	agentHandoffsCmd.Flags().BoolVar(&handoffsJSON, "json", false, "output as JSON")
}
