package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/cliapi/caravans"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var caravanDepCmd = &cobra.Command{
	Use:   "dep",
	Short: "Manage caravan-level dependencies",
}

// --- sol caravan dep add ---

var caravanDepAddCmd = &cobra.Command{
	Use:          "add <caravan-id> <depends-on-caravan-id>",
	Short:        "Declare that a caravan depends on another caravan being closed",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fromID := args[0]
		toID := args[1]
		if err := config.ValidateCaravanID(fromID); err != nil {
			return err
		}
		if err := config.ValidateCaravanID(toID); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		if err := sphereStore.AddCaravanDependency(fromID, toID); err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printCaravanJSON(sphereStore, fromID)
		}

		// Fetch names for display.
		fromName := caravanName(sphereStore, fromID)
		toName := caravanName(sphereStore, toID)
		fmt.Printf("Added dependency: %s (%s) depends on %s (%s)\n",
			fromID, fromName, toID, toName)
		return nil
	},
}

// --- sol caravan dep remove ---

var caravanDepRemoveCmd = &cobra.Command{
	Use:          "remove <caravan-id> <depends-on-caravan-id>",
	Short:        "Remove a caravan dependency",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		fromID := args[0]
		toID := args[1]
		if err := config.ValidateCaravanID(fromID); err != nil {
			return err
		}
		if err := config.ValidateCaravanID(toID); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		if err := sphereStore.RemoveCaravanDependency(fromID, toID); err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printCaravanJSON(sphereStore, fromID)
		}

		fmt.Printf("Removed dependency: %s no longer depends on %s\n", fromID, toID)
		return nil
	},
}

// --- sol caravan dep list ---

var caravanDepListCmd = &cobra.Command{
	Use:          "list <caravan-id>",
	Short:        "Show caravan-level dependencies",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}
		jsonOut, _ := cmd.Flags().GetBool("json")

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return err
		}

		deps, err := sphereStore.GetCaravanDependencies(caravanID)
		if err != nil {
			return err
		}

		dependents, err := sphereStore.GetCaravanDependents(caravanID)
		if err != nil {
			return err
		}

		if jsonOut {
			out := caravans.DepListResponse{
				ID:   caravan.ID,
				Name: caravan.Name,
			}
			out.DependsOn = make([]caravans.DepInfo, 0, len(deps))
			for _, depID := range deps {
				out.DependsOn = append(out.DependsOn, caravanDepEntry(sphereStore, depID))
			}
			out.DependedBy = make([]caravans.DepInfo, 0, len(dependents))
			for _, depID := range dependents {
				out.DependedBy = append(out.DependedBy, caravanDepEntry(sphereStore, depID))
			}
			return printJSON(out)
		}

		fmt.Printf("Caravan: %s (%s)\n\n", caravan.Name, caravan.ID)

		if len(deps) > 0 {
			fmt.Println("Depends on:")
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			for _, depID := range deps {
				c, err := sphereStore.GetCaravan(depID)
				if err != nil {
					fmt.Fprintf(tw, "  %s\t(unknown)\t\n", depID)
					continue
				}
				fmt.Fprintf(tw, "  %s\t%s\t(%s)\n", c.ID, c.Name, c.Status)
			}
			tw.Flush()
		} else {
			fmt.Println("Depends on: (none)")
		}
		fmt.Println()

		if len(dependents) > 0 {
			fmt.Println("Depended on by:")
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			for _, depID := range dependents {
				c, err := sphereStore.GetCaravan(depID)
				if err != nil {
					fmt.Fprintf(tw, "  %s\t(unknown)\t\n", depID)
					continue
				}
				fmt.Fprintf(tw, "  %s\t%s\t(%s)\n", c.ID, c.Name, c.Status)
			}
			tw.Flush()
		} else {
			fmt.Println("Depended on by: (none)")
		}

		return nil
	},
}

func caravanDepEntry(s *store.SphereStore, id string) caravans.DepInfo {
	c, err := s.GetCaravan(id)
	if err != nil {
		return caravans.DepInfo{ID: id, Name: "(unknown)", Status: "unknown"}
	}
	return caravans.DepInfo{ID: c.ID, Name: c.Name, Status: string(c.Status)}
}

func caravanName(s *store.SphereStore, id string) string {
	c, err := s.GetCaravan(id)
	if err != nil {
		return "(unknown)"
	}
	return c.Name
}

// printCaravanJSON fetches a caravan with item statuses and prints the cliapi Caravan as JSON.
func printCaravanJSON(sphereStore *store.SphereStore, caravanID string) error {
	caravan, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		return fmt.Errorf("failed to get caravan: %w", err)
	}

	statuses, err := sphereStore.CheckCaravanReadiness(caravanID, gatedWorldOpener)
	if err != nil {
		// Fall back to empty statuses if readiness check fails (e.g., world not found).
		statuses = []store.CaravanItemStatus{}
	}

	out := caravans.FromStoreCaravan(*caravan, statuses)
	return printJSON(out)
}

func init() {
	caravanCmd.AddCommand(caravanDepCmd)
	caravanDepCmd.AddCommand(caravanDepAddCmd)
	caravanDepCmd.AddCommand(caravanDepRemoveCmd)
	caravanDepCmd.AddCommand(caravanDepListCmd)

	caravanDepAddCmd.Flags().Bool("json", false, "output as JSON")
	caravanDepRemoveCmd.Flags().Bool("json", false, "output as JSON")
	caravanDepListCmd.Flags().Bool("json", false, "output as JSON")
}
