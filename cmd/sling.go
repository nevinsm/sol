package cmd

import (
	"fmt"
	"strings"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var (
	slingAgent   string
	slingFormula string
	slingVars    []string
)

var slingCmd = &cobra.Command{
	Use:   "sling <work-item-id> <rig>",
	Short: "Assign a work item to an agent and start its session",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		workItemID := args[0]
		rig := args[1]

		// Discover source repo from current directory.
		sourceRepo, err := dispatch.DiscoverSourceRepo()
		if err != nil {
			return fmt.Errorf("must run gt sling from within a git repository: %w", err)
		}

		rigStore, err := store.OpenRig(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		mgr := dispatch.NewSessionManager()
		logger := events.NewLogger(config.Home())

		// Parse --var flags into a map.
		vars := parseVarFlags(slingVars)

		result, err := dispatch.Sling(dispatch.SlingOpts{
			WorkItemID: workItemID,
			Rig:        rig,
			AgentName:  slingAgent,
			SourceRepo: sourceRepo,
			Formula:    slingFormula,
			Variables:  vars,
		}, rigStore, townStore, mgr, logger)
		if err != nil {
			return err
		}

		fmt.Printf("Slung %s → %s (%s)\n", result.WorkItemID, result.AgentName, result.SessionName)
		fmt.Printf("  Worktree: %s\n", result.WorktreeDir)
		fmt.Printf("  Session:  %s\n", result.SessionName)
		if result.Formula != "" {
			fmt.Printf("  Formula:  %s\n", result.Formula)
		}
		fmt.Printf("  Attach:   gt session attach %s\n", result.SessionName)
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
	rootCmd.AddCommand(slingCmd)
	slingCmd.Flags().StringVar(&slingAgent, "agent", "", "agent name (auto-selects idle agent if omitted)")
	slingCmd.Flags().StringVar(&slingFormula, "formula", "", "workflow formula to instantiate")
	slingCmd.Flags().StringSliceVar(&slingVars, "var", nil, "workflow variable (key=val, repeatable)")
}
