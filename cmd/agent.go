package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

// --- sol agent create ---

var (
	agentCreateWorld string
	agentCreateRole  string
)

var agentCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if agentCreateWorld == "" {
			return fmt.Errorf("--world is required")
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		id, err := sphereStore.CreateAgent(name, agentCreateWorld, agentCreateRole)
		if err != nil {
			return err
		}
		fmt.Printf("Created agent %s\n", id)
		return nil
	},
}

// --- sol agent list ---

var (
	agentListWorld string
	agentListJSON  bool
)

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		if agentListWorld == "" {
			return fmt.Errorf("--world is required")
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		agents, err := sphereStore.ListAgents(agentListWorld, "")
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
		fmt.Fprintf(tw, "ID\tNAME\tWORLD\tROLE\tSTATE\tTETHER ITEM\n")
		for _, a := range agents {
			tetherItem := a.TetherItem
			if tetherItem == "" {
				tetherItem = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Name, a.World, a.Role, a.State, tetherItem)
		}
		tw.Flush()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)

	agentCmd.AddCommand(agentCreateCmd)
	agentCreateCmd.Flags().StringVar(&agentCreateWorld, "world", "", "world name")
	agentCreateCmd.Flags().StringVar(&agentCreateRole, "role", "agent", "agent role")

	agentCmd.AddCommand(agentListCmd)
	agentListCmd.Flags().StringVar(&agentListWorld, "world", "", "world name")
	agentListCmd.Flags().BoolVar(&agentListJSON, "json", false, "output as JSON")
}
