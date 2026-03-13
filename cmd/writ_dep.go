package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

func validateWritIDs(ids ...string) error {
	for _, id := range ids {
		if err := config.ValidateWritID(id); err != nil {
			return err
		}
	}
	return nil
}

var depJSON bool

var writDepCmd = &cobra.Command{
	Use:   "dep",
	Short: "Manage writ dependencies",
}

// --- sol writ dep add ---

var writDepAddCmd = &cobra.Command{
	Use:          "add <from-id> <to-id>",
	Short:        "Add a dependency (from depends on to)",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateWritIDs(args[0], args[1]); err != nil {
			return err
		}
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		s, err := store.OpenWorld(world)
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

// --- sol writ dep remove ---

var writDepRemoveCmd = &cobra.Command{
	Use:          "remove <from-id> <to-id>",
	Short:        "Remove a dependency",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateWritIDs(args[0], args[1]); err != nil {
			return err
		}
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		s, err := store.OpenWorld(world)
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

// --- sol writ dep list ---

var writDepListCmd = &cobra.Command{
	Use:          "list <item-id>",
	Short:        "List dependencies for a writ",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.ValidateWritID(args[0]); err != nil {
			return err
		}
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		s, err := store.OpenWorld(world)
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
				WritID string   `json:"writ_id"`
				DependsOn  []string `json:"depends_on"`
				DependedBy []string `json:"depended_by"`
			}{
				WritID: itemID,
				DependsOn:  deps,
				DependedBy: dependents,
			}
			if out.DependsOn == nil {
				out.DependsOn = []string{}
			}
			if out.DependedBy == nil {
				out.DependedBy = []string{}
			}
			return printJSON(out)
		}

		fmt.Printf("Writ: %s\n", itemID)
		fmt.Println()

		if len(deps) == 0 {
			fmt.Println("Depends on: (none)")
		} else {
			fmt.Println("Depends on:")
			for _, depID := range deps {
				title := "(unknown)"
				status := ""
				item, err := s.GetWrit(depID)
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
				item, err := s.GetWrit(depID)
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
	writCmd.AddCommand(writDepCmd)
	writDepCmd.AddCommand(writDepAddCmd)
	writDepCmd.AddCommand(writDepRemoveCmd)
	writDepCmd.AddCommand(writDepListCmd)

	// Shared --world flag for dep subcommands.
	writDepAddCmd.Flags().String("world", "", "world name")
	writDepRemoveCmd.Flags().String("world", "", "world name")
	writDepListCmd.Flags().String("world", "", "world name")
	writDepListCmd.Flags().BoolVar(&depJSON, "json", false, "output as JSON")
}
