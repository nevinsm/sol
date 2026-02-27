package cmd

import (
	"fmt"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	castAgent   string
	castFormula string
	castVars    []string
)

var castCmd = &cobra.Command{
	Use:   "cast <work-item-id> <world>",
	Short: "Assign a work item to an agent and start its session",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		workItemID := args[0]
		world := args[1]

		// Discover source repo from current directory.
		sourceRepo, err := dispatch.DiscoverSourceRepo()
		if err != nil {
			return fmt.Errorf("must run sol cast from within a git repository: %w", err)
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

		// Parse --var flags into a map.
		vars := parseVarFlags(castVars)

		result, err := dispatch.Cast(dispatch.CastOpts{
			WorkItemID: workItemID,
			World:      world,
			AgentName:  castAgent,
			SourceRepo: sourceRepo,
			Formula:    castFormula,
			Variables:  vars,
		}, worldStore, sphereStore, mgr, logger)
		if err != nil {
			return err
		}

		fmt.Printf("Cast %s -> %s (%s)\n", result.WorkItemID, result.AgentName, result.SessionName)
		fmt.Printf("  Worktree: %s\n", result.WorktreeDir)
		fmt.Printf("  Session:  %s\n", result.SessionName)
		if result.Formula != "" {
			fmt.Printf("  Formula:  %s\n", result.Formula)
		}
		fmt.Printf("  Attach:   sol session attach %s\n", result.SessionName)
		return nil
	},
}

// parseVarFlags splits "key=val" strings into a map.
func parseVarFlags(vars []string) map[string]string {
	if len(vars) == 0 {
		return nil
	}
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

func init() {
	rootCmd.AddCommand(castCmd)
	castCmd.Flags().StringVar(&castAgent, "agent", "", "agent name (auto-selects idle agent if omitted)")
	castCmd.Flags().StringVar(&castFormula, "formula", "", "workflow formula to instantiate")
	castCmd.Flags().StringSliceVar(&castVars, "var", nil, "workflow variable (key=val, repeatable)")
}
