package cmd

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	resolveWorld string
	resolveAgent string
)

var resolveCmd = &cobra.Command{
	Use:          "resolve",
	Short:        "Signal work completion — push branch, update state, clear tether",
	GroupID:      groupDispatch,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world := resolveWorld
		agent := resolveAgent

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
		if err := config.RequireWorld(world); err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := dispatch.NewSessionManager()
		logger := events.NewLogger(config.Home())

		result, err := dispatch.Resolve(dispatch.ResolveOpts{
			World:     world,
			AgentName: agent,
		}, worldStore, sphereStore, mgr, logger)
		if err != nil {
			return err
		}

		fmt.Printf("Done: %s (%s)\n", result.WritID, result.Title)
		fmt.Printf("  Branch: %s\n", result.BranchName)
		if result.MergeRequestID != "" {
			fmt.Printf("  Merge request: %s (queued)\n", result.MergeRequestID)
		}
		if result.SessionKept {
			fmt.Printf("  Agent %s resolved %q — session kept alive\n", result.AgentName, result.Title)
		} else {
			fmt.Printf("  Agent %s is now idle.\n", result.AgentName)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(resolveCmd)
	resolveCmd.Flags().StringVar(&resolveWorld, "world", "", "world name (defaults to SOL_WORLD env)")
	resolveCmd.Flags().StringVar(&resolveAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
}
