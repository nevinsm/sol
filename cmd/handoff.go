package cmd

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	handoffWorld   string
	handoffAgent   string
	handoffSummary string
)

var handoffCmd = &cobra.Command{
	Use:   "handoff",
	Short: "Hand off to a fresh session with context preservation",
	RunE: func(cmd *cobra.Command, args []string) error {
		world := handoffWorld
		agent := handoffAgent

		// Infer from environment if not provided.
		if world == "" {
			world = os.Getenv("SOL_WORLD")
		}
		if agent == "" {
			agent = os.Getenv("SOL_AGENT")
		}

		if world == "" {
			return fmt.Errorf("--world is required (or set SOL_WORLD env var)")
		}
		if agent == "" {
			return fmt.Errorf("--agent is required (or set SOL_AGENT env var)")
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()
		logger := events.NewLogger(config.Home())

		if err := handoff.Exec(handoff.ExecOpts{
			World:     world,
			AgentName: agent,
			Summary:   handoffSummary,
		}, mgr, sphereStore, logger); err != nil {
			return err
		}

		fmt.Println("Handoff complete. New session starting.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(handoffCmd)
	handoffCmd.Flags().StringVar(&handoffWorld, "world", "", "world name (defaults to SOL_WORLD env)")
	handoffCmd.Flags().StringVar(&handoffAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
	handoffCmd.Flags().StringVar(&handoffSummary, "summary", "", "summary of current progress")
}
