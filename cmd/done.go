package cmd

import (
	"fmt"
	"os"

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var (
	doneRig   string
	doneAgent string
)

var doneCmd = &cobra.Command{
	Use:   "done",
	Short: "Signal work completion — push branch, update state, clear hook",
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := doneRig
		agent := doneAgent

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

		result, err := dispatch.Done(dispatch.DoneOpts{
			Rig:       rig,
			AgentName: agent,
		}, rigStore, townStore, mgr)
		if err != nil {
			return err
		}

		fmt.Printf("Done: %s (%s)\n", result.WorkItemID, result.Title)
		fmt.Printf("  Branch: %s\n", result.BranchName)
		fmt.Printf("  Merge request: %s (queued)\n", result.MergeRequestID)
		fmt.Printf("  Agent %s is now idle.\n", result.AgentName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doneCmd)
	doneCmd.Flags().StringVar(&doneRig, "rig", "", "rig name (defaults to GT_RIG env)")
	doneCmd.Flags().StringVar(&doneAgent, "agent", "", "agent name (defaults to GT_AGENT env)")
}
