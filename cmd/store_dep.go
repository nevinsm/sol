package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var depJSON bool

var storeDepCmd = &cobra.Command{
	Use:   "dep",
	Short: "Manage work item dependencies",
}

// --- gt store dep add ---

var storeDepAddCmd = &cobra.Command{
	Use:   "add <from-id> <to-id>",
	Short: "Add a dependency (from depends on to)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		db := cmd.Flag("db").Value.String()
		if db == "" {
			return fmt.Errorf("--db is required")
		}

		s, err := store.OpenRig(db)
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.AddDependency(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("Added dependency: %s depends on %s\n", args[0], args[1])
		return nil
	},
}

// --- gt store dep remove ---

var storeDepRemoveCmd = &cobra.Command{
	Use:   "remove <from-id> <to-id>",
	Short: "Remove a dependency",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		db := cmd.Flag("db").Value.String()
		if db == "" {
			return fmt.Errorf("--db is required")
		}

		s, err := store.OpenRig(db)
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.RemoveDependency(args[0], args[1]); err != nil {
			return err
		}
		fmt.Printf("Removed dependency: %s no longer depends on %s\n", args[0], args[1])
		return nil
	},
}

// --- gt store dep list ---

var storeDepListCmd = &cobra.Command{
	Use:   "list <item-id>",
	Short: "List dependencies for a work item",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db := cmd.Flag("db").Value.String()
		if db == "" {
			return fmt.Errorf("--db is required")
		}

		s, err := store.OpenRig(db)
		if err != nil {
			return err
		}
		defer s.Close()

		itemID := args[0]

		deps, err := s.GetDependencies(itemID)
		if err != nil {
			return err
		}

		dependents, err := s.GetDependents(itemID)
		if err != nil {
			return err
		}

		if depJSON {
			out := struct {
				WorkItemID string   `json:"work_item_id"`
				DependsOn  []string `json:"depends_on"`
				DependedBy []string `json:"depended_by"`
			}{
				WorkItemID: itemID,
				DependsOn:  deps,
				DependedBy: dependents,
			}
			if out.DependsOn == nil {
				out.DependsOn = []string{}
			}
			if out.DependedBy == nil {
				out.DependedBy = []string{}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		fmt.Printf("Work item: %s\n", itemID)
		fmt.Println()

		if len(deps) == 0 {
			fmt.Println("Depends on: (none)")
		} else {
			fmt.Println("Depends on:")
			for _, depID := range deps {
				title := "(unknown)"
				status := ""
				item, err := s.GetWorkItem(depID)
				if err == nil {
					title = item.Title
					status = fmt.Sprintf("(%s)", item.Status)
				}
				fmt.Printf("  %s  %s  %s\n", depID, title, status)
			}
		}

		fmt.Println()

		if len(dependents) == 0 {
			fmt.Println("Depended on by: (none)")
		} else {
			fmt.Println("Depended on by:")
			for _, depID := range dependents {
				title := "(unknown)"
				status := ""
				item, err := s.GetWorkItem(depID)
				if err == nil {
					title = item.Title
					status = fmt.Sprintf("(%s)", item.Status)
				}
				fmt.Printf("  %s  %s  %s\n", depID, title, status)
			}
		}

		return nil
	},
}

func init() {
	storeCmd.AddCommand(storeDepCmd)
	storeDepCmd.AddCommand(storeDepAddCmd)
	storeDepCmd.AddCommand(storeDepRemoveCmd)
	storeDepCmd.AddCommand(storeDepListCmd)

	// Shared --db flag for dep subcommands.
	storeDepAddCmd.Flags().StringVar(&dbFlag, "db", "", "rig database name")
	storeDepRemoveCmd.Flags().StringVar(&dbFlag, "db", "", "rig database name")
	storeDepListCmd.Flags().StringVar(&dbFlag, "db", "", "rig database name")
	storeDepListCmd.Flags().BoolVar(&depJSON, "json", false, "output as JSON")
}
