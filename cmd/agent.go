package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

// --- gt agent create ---

var (
	agentCreateRig  string
	agentCreateRole string
)

var agentCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if agentCreateRig == "" {
			return fmt.Errorf("--rig is required")
		}

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		id, err := townStore.CreateAgent(name, agentCreateRig, agentCreateRole)
		if err != nil {
			return err
		}
		fmt.Printf("Created agent %s\n", id)
		return nil
	},
}

// --- gt agent list ---

var (
	agentListRig  string
	agentListJSON bool
)

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		if agentListRig == "" {
			return fmt.Errorf("--rig is required")
		}

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		agents, err := townStore.ListAgents(agentListRig, "")
		if err != nil {
			return err
		}

		if agentListJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(agents)
		}

		if len(agents) == 0 {
			fmt.Println("No agents found.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tNAME\tRIG\tROLE\tSTATE\tHOOK ITEM\n")
		for _, a := range agents {
			hookItem := a.HookItem
			if hookItem == "" {
				hookItem = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Name, a.Rig, a.Role, a.State, hookItem)
		}
		tw.Flush()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)

	agentCmd.AddCommand(agentCreateCmd)
	agentCreateCmd.Flags().StringVar(&agentCreateRig, "rig", "", "rig name")
	agentCreateCmd.Flags().StringVar(&agentCreateRole, "role", "polecat", "agent role")

	agentCmd.AddCommand(agentListCmd)
	agentListCmd.Flags().StringVar(&agentListRig, "rig", "", "rig name")
	agentListCmd.Flags().BoolVar(&agentListJSON, "json", false, "output as JSON")
}
