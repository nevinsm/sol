package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var mrCmd = &cobra.Command{
	Use:     "mr",
	Short:   "Merge request plumbing commands",
	GroupID: groupPlumbing,
}

var mrCreateCmd = &cobra.Command{
	Use:          "create --world=W --branch=B --work-item=ID",
	Short:        "Create a merge request for an existing work item",
	Long:         "Plumbing command to manually queue a branch for forge review without going through sol resolve.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		branch, _ := cmd.Flags().GetString("branch")
		workItemID, _ := cmd.Flags().GetString("work-item")
		priority, _ := cmd.Flags().GetInt("priority")

		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		if branch == "" {
			return fmt.Errorf("--branch is required")
		}
		if workItemID == "" {
			return fmt.Errorf("--work-item is required")
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		// Validate work item exists.
		item, err := worldStore.GetWorkItem(workItemID)
		if err != nil {
			return fmt.Errorf("work item %q not found: %w", workItemID, err)
		}

		// Use work item priority if not explicitly set.
		if !cmd.Flags().Changed("priority") {
			priority = item.Priority
		}

		mrID, err := worldStore.CreateMergeRequest(workItemID, branch, priority)
		if err != nil {
			return fmt.Errorf("failed to create merge request: %w", err)
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMergeQueued, "operator", "operator", world, map[string]string{
			"merge_request_id": mrID,
			"work_item_id":     workItemID,
			"branch":           branch,
		})

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(map[string]string{
				"id":           mrID,
				"work_item_id": workItemID,
				"branch":       branch,
				"phase":        "ready",
			})
		}

		fmt.Printf("Created: %s\n", mrID)
		fmt.Printf("  Work item: %s (%s)\n", item.ID, item.Title)
		fmt.Printf("  Branch:    %s\n", branch)
		fmt.Printf("  Priority:  %d\n", priority)
		fmt.Printf("  Phase:     ready\n")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(mrCmd)
	mrCmd.AddCommand(mrCreateCmd)

	mrCreateCmd.Flags().String("world", "", "world name (required)")
	mrCreateCmd.Flags().String("branch", "", "branch to merge (required)")
	mrCreateCmd.Flags().String("work-item", "", "work item ID (required)")
	mrCreateCmd.Flags().Int("priority", 2, "priority (1=high, 2=normal, 3=low)")
	mrCreateCmd.Flags().Bool("json", false, "output as JSON")
}
