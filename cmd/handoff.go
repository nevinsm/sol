package cmd

import (
	"fmt"
	"os"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/handoff"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var (
	handoffRig     string
	handoffAgent   string
	handoffSummary string
)

var handoffCmd = &cobra.Command{
	Use:   "handoff",
	Short: "Hand off to a fresh session with context preservation",
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := handoffRig
		agent := handoffAgent

		// Infer from environment if not provided.
		if rig == "" {
			rig = os.Getenv("GT_RIG")
		}
		if agent == "" {
			agent = os.Getenv("GT_AGENT")
		}

		if rig == "" {
			return fmt.Errorf("--rig is required (or set GT_RIG env var)")
		}
		if agent == "" {
			return fmt.Errorf("--agent is required (or set GT_AGENT env var)")
		}

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		mgr := session.New()
		logger := events.NewLogger(config.Home())

		if err := handoff.Exec(handoff.ExecOpts{
			Rig:       rig,
			AgentName: agent,
			Summary:   handoffSummary,
		}, mgr, townStore, logger); err != nil {
			return err
		}

		fmt.Println("Handoff complete. New session starting.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(handoffCmd)
	handoffCmd.Flags().StringVar(&handoffRig, "rig", "", "rig name (defaults to GT_RIG env)")
	handoffCmd.Flags().StringVar(&handoffAgent, "agent", "", "agent name (defaults to GT_AGENT env)")
	handoffCmd.Flags().StringVar(&handoffSummary, "summary", "", "summary of current progress")
}
