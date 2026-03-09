package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/trace"
	"github.com/spf13/cobra"
)

var (
	traceJSON     bool
	traceTimeline bool
	traceCost     bool
	traceNoEvents bool
	traceWorld    string
)

var writTraceCmd = &cobra.Command{
	Use:          "trace <id>",
	Short:        "Show full trace of a writ",
	Long:         "Shows unified timeline, cost, and escalation data for a writ, aggregating data from world DB, sphere DB, tether files, and event logs.",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		writID := args[0]
		if err := config.ValidateWritID(writID); err != nil {
			return err
		}

		opts := trace.Options{
			World:    traceWorld,
			NoEvents: traceNoEvents,
		}

		td, err := trace.Collect(writID, opts)
		if err != nil {
			return err
		}

		if traceJSON {
			return printJSON(td)
		}

		if traceTimeline {
			fmt.Print(trace.RenderTimeline(td))
			return nil
		}

		if traceCost {
			fmt.Print(trace.RenderCost(td))
			return nil
		}

		fmt.Print(trace.RenderFull(td))
		return nil
	},
}

func init() {
	writCmd.AddCommand(writTraceCmd)
	writTraceCmd.Flags().BoolVar(&traceJSON, "json", false, "machine-readable JSON output")
	writTraceCmd.Flags().BoolVar(&traceTimeline, "timeline", false, "show timeline only")
	writTraceCmd.Flags().BoolVar(&traceCost, "cost", false, "show cost only")
	writTraceCmd.Flags().BoolVar(&traceNoEvents, "no-events", false, "skip event log scan (faster)")
	writTraceCmd.Flags().StringVar(&traceWorld, "world", "", "world name")
}
