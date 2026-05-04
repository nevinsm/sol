package cmd

import (
	"fmt"
	"os"

	cliforge "github.com/nevinsm/sol/internal/cliapi/forge"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var mrCmd = &cobra.Command{
	Use:     "mr",
	Short:   "Merge request plumbing commands",
	GroupID: groupPlumbing,
	Hidden:  true,
}

var mrCreateCmd = &cobra.Command{
	Use:          "create --world=W --branch=B --writ=ID",
	Short:        "Create a merge request for an existing writ",
	Hidden:       true,
	Long:         "Plumbing command to manually queue a branch for forge review without going through sol resolve.",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		branch, _ := cmd.Flags().GetString("branch")
		writID, _ := cmd.Flags().GetString("writ")
		priority, _ := cmd.Flags().GetInt("priority")

		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		// Validate writ exists.
		item, err := worldStore.GetWrit(writID)
		if err != nil {
			return fmt.Errorf("writ %q not found: %w", writID, err)
		}

		// Use writ priority if not explicitly set.
		if !cmd.Flags().Changed("priority") {
			priority = item.Priority
		}

		mrID, err := worldStore.CreateMergeRequest(writID, branch, priority)
		if err != nil {
			return fmt.Errorf("failed to create merge request: %w", err)
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMergeQueued, "sol", config.Autarch, "both", map[string]string{
			"merge_request_id": mrID,
			"writ_id":          writID,
			"branch":           branch,
			"world":            world,
		})

		// Nudge forge that a new MR is ready (best-effort).
		forgeSession := config.SessionName(world, "forge")
		forgeBody := fmt.Sprintf(`{"writ_id":%q,"merge_request_id":%q,"branch":%q,"title":%q}`,
			writID, mrID, branch, item.Title)
		if err := nudge.Deliver(forgeSession, nudge.Message{
			Sender:   config.Autarch,
			Type:     "MR_READY",
			Subject:  fmt.Sprintf("MR %s ready for merge", mrID),
			Body:     forgeBody,
			Priority: "normal",
		}); err != nil {
			fmt.Fprintf(os.Stderr, "mr create: failed to nudge forge: %v\n", err)
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			mr, err := worldStore.GetMergeRequest(mrID)
			if err != nil {
				return fmt.Errorf("failed to read created merge request: %w", err)
			}
			return printJSON(cliforge.FromStoreMR(*mr))
		}

		fmt.Printf("Created: %s\n", mrID)
		fmt.Printf("  Writ: %s (%s)\n", item.ID, item.Title)
		fmt.Printf("  Branch:    %s\n", branch)
		fmt.Printf("  Priority:  %d\n", priority)
		fmt.Printf("  Phase:     ready\n")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(mrCmd)
	mrCmd.AddCommand(mrCreateCmd)

	mrCreateCmd.Flags().String("world", "", "world name")
	mrCreateCmd.Flags().String("branch", "", "branch to merge (required)")
	mrCreateCmd.Flags().String("writ", "", "writ ID (required)")
	mrCreateCmd.Flags().Int("priority", 2, "priority (1=high, 2=normal, 3=low)")
	mrCreateCmd.Flags().Bool("json", false, "output as JSON")

	_ = mrCreateCmd.MarkFlagRequired("branch")
	_ = mrCreateCmd.MarkFlagRequired("writ")
}
