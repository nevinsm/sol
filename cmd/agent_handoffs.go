package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	cliagents "github.com/nevinsm/sol/internal/cliapi/agents"
	"github.com/nevinsm/sol/internal/cliformat"
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
	Use:   "handoffs [name]",
	Short: "Show recent handoff events",
	Long: `Show recent handoff events for agents in a world.

Without a name argument, lists handoffs for all agents in the world.
Passing a name filters handoffs to just that agent:

    sol agent handoffs Polaris --world=sol-dev
    sol agent handoffs --world=sol-dev

The --world flag is optional: if omitted, sol auto-detects the world from
the current directory (when inside a sol-managed worktree or world tree).

The --agent flag is deprecated; pass the agent name as a positional
argument instead.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve agent name: positional wins, but fall back to the
		// deprecated --agent flag for one release. If --agent was used,
		// emit a deprecation notice on stderr. We print the notice before
		// any other work so the user sees it even if later steps fail.
		agentName := ""
		if len(args) > 0 {
			agentName = args[0]
		}
		if handoffsAgent != "" {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: --agent is deprecated; pass the agent name as a positional argument (e.g. 'sol agent handoffs <name>')")
			if agentName == "" {
				agentName = handoffsAgent
			}
		}

		world, err := config.ResolveWorld(handoffsWorld)
		if err != nil {
			return err
		}

		reader := events.NewReader(config.Home(), false)
		opts := events.ReadOpts{
			Type: events.EventHandoff,
		}
		if agentName != "" {
			opts.Actor = agentName
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
			return printJSON(cliagents.FromEvents(filtered))
		}

		if len(filtered) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No handoff events found.")
			return nil
		}

		now := time.Now()
		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "AGENT\tREASON\tSESSION AGE\tWRIT\tTIME\n")

		for _, ev := range filtered {
			payload, _ := payloadMap(ev.Payload)
			agent := stringVal(payload, "agent", ev.Actor)
			reason := stringVal(payload, "reason", cliformat.EmptyMarker)
			sessionAge := stringVal(payload, "session_age", cliformat.EmptyMarker)
			writ := stringVal(payload, "writ_id", cliformat.EmptyMarker)
			timeCell := cliformat.FormatTimestampOrRelative(ev.Timestamp, now)

			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", agent, reason, sessionAge, writ, timeCell)
		}
		tw.Flush()

		fmt.Fprintln(cmd.OutOrStdout(), cliformat.FormatCount(len(filtered), "handoff", "handoff(s)"))
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

func init() {
	agentCmd.AddCommand(agentHandoffsCmd)
	agentHandoffsCmd.Flags().StringVar(&handoffsWorld, "world", "", "world name (auto-detected from current directory if omitted)")
	agentHandoffsCmd.Flags().StringVar(&handoffsAgent, "agent", "", "filter by agent name (deprecated: use positional arg)")
	_ = agentHandoffsCmd.Flags().MarkHidden("agent")
	agentHandoffsCmd.Flags().IntVar(&handoffsLast, "last", 20, "number of recent events to show")
	agentHandoffsCmd.Flags().BoolVar(&handoffsJSON, "json", false, "output as JSON")
}
