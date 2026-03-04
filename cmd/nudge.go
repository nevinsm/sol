package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/spf13/cobra"
)

var (
	nudgeDrainWorld string
	nudgeDrainAgent string
)

var nudgeCmd = &cobra.Command{
	Use:   "nudge",
	Short: "Nudge queue operations",
}

var nudgeDrainCmd = &cobra.Command{
	Use:          "drain",
	Short:        "Drain pending nudge messages for an agent session",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(nudgeDrainWorld)
		if err != nil {
			return err
		}
		session := config.SessionName(world, nudgeDrainAgent)

		// Drain pending messages.
		messages, err := nudge.Drain(session)
		if err != nil {
			return fmt.Errorf("failed to drain nudge queue: %w", err)
		}

		// Run cleanup (requeue orphaned claims, delete expired).
		if err := nudge.Cleanup(session); err != nil {
			return fmt.Errorf("failed to cleanup nudge queue: %w", err)
		}

		// Silent no-op if no messages.
		if len(messages) == 0 {
			return nil
		}

		// Format and print messages as structured block.
		for _, msg := range messages {
			fmt.Printf("[NOTIFICATION] %s: %s", msg.Type, msg.Subject)
			if msg.Body != "" {
				fmt.Printf(" — %s", msg.Body)
			}
			fmt.Println()
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(nudgeCmd)
	nudgeCmd.AddCommand(nudgeDrainCmd)
	nudgeDrainCmd.Flags().StringVar(&nudgeDrainWorld, "world", "", "world name (optional with SOL_WORLD or inside a world directory)")
	nudgeDrainCmd.Flags().StringVar(&nudgeDrainAgent, "agent", "", "agent name")
	nudgeDrainCmd.MarkFlagRequired("agent")
}
