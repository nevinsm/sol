package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	caravanOwner   string
	caravanPhase   int
	caravanFormula string
	caravanVars    []string
)

var caravanCmd = &cobra.Command{
	Use:   "caravan",
	Short: "Manage caravans (grouped work item batches)",
}

// --- sol caravan create ---

var caravanCreateCmd = &cobra.Command{
	Use:          "create <name> [<item-id> ...]",
	Short:        "Create a caravan with optional initial items",
	Args:         cobra.MinimumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		itemIDs := args[1:]

		world, _ := cmd.Flags().GetString("world")
		if world == "" && len(itemIDs) > 0 {
			return fmt.Errorf("--world is required when adding items")
		}
		if world != "" {
			if err := config.RequireWorld(world); err != nil {
				return err
			}
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		owner := caravanOwner
		if owner == "" {
			owner = "operator"
		}

		caravanID, err := sphereStore.CreateCaravan(name, owner)
		if err != nil {
			return err
		}

		phase, _ := cmd.Flags().GetInt("phase")
		for _, itemID := range itemIDs {
			if err := sphereStore.CreateCaravanItem(caravanID, itemID, world, phase); err != nil {
				return err
			}
		}

		logger := events.NewLogger(config.Home())
		logger.Emit(events.EventCaravanCreated, "sol", "operator", "both", map[string]string{
			"caravan_id": caravanID,
			"name":       name,
			"count":      fmt.Sprintf("%d", len(itemIDs)),
		})

		fmt.Printf("Created caravan %s: %q (%d items)\n", caravanID, name, len(itemIDs))
		return nil
	},
}

// --- sol caravan add ---

var caravanAddCmd = &cobra.Command{
	Use:          "add <caravan-id> <item-id> [<item-id> ...]",
	Short:        "Add items to an existing caravan",
	Args:         cobra.MinimumNArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]
		itemIDs := args[1:]

		world, _ := cmd.Flags().GetString("world")
		if world == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(world); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		phase, _ := cmd.Flags().GetInt("phase")
		for _, itemID := range itemIDs {
			if err := sphereStore.CreateCaravanItem(caravanID, itemID, world, phase); err != nil {
				return err
			}
		}

		fmt.Printf("Added %d items to caravan %s\n", len(itemIDs), caravanID)
		return nil
	},
}

// --- sol caravan check ---

var caravanCheckCmd = &cobra.Command{
	Use:          "check <caravan-id>",
	Short:        "Check readiness of caravan items",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return err
		}

		statuses, err := sphereStore.CheckCaravanReadiness(caravanID, gatedWorldOpener)
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			out := struct {
				ID     string                    `json:"id"`
				Name   string                    `json:"name"`
				Status string                    `json:"status"`
				Items  []store.CaravanItemStatus `json:"items"`
			}{
				ID:     caravan.ID,
				Name:   caravan.Name,
				Status: caravan.Status,
				Items:  statuses,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		fmt.Printf("Caravan: %s (%s)\n", caravan.Name, caravan.ID)
		fmt.Printf("Status: %s\n", caravan.Status)
		fmt.Println()

		// Separate ready, awaiting merge, and blocked items.
		var ready, awaitingMerge, blocked []store.CaravanItemStatus
		for _, st := range statuses {
			if st.WorkItemStatus == "closed" {
				// fully merged, skip for now
			} else if st.WorkItemStatus == "done" {
				awaitingMerge = append(awaitingMerge, st)
			} else if st.WorkItemStatus == "open" && st.Ready {
				ready = append(ready, st)
			} else {
				blocked = append(blocked, st)
			}
		}

		if len(ready) > 0 {
			fmt.Println("Ready for dispatch:")
			for _, st := range ready {
				title := itemTitle(st.WorkItemID, st.World)
				fmt.Printf("  %s  %s  (%s)\n", st.WorkItemID, title, st.World)
			}
			fmt.Println()
		}

		if len(awaitingMerge) > 0 {
			fmt.Println("Awaiting merge:")
			for _, st := range awaitingMerge {
				title := itemTitle(st.WorkItemID, st.World)
				fmt.Printf("  %s  %s  (%s)\n", st.WorkItemID, title, st.World)
			}
			fmt.Println()
		}

		if len(blocked) > 0 {
			fmt.Println("Blocked:")
			for _, st := range blocked {
				title := itemTitle(st.WorkItemID, st.World)
				waitingOn := blockedByList(st.WorkItemID, st.World)
				if waitingOn != "" {
					fmt.Printf("  %s  %s  (%s)  <- waiting on %s\n", st.WorkItemID, title, st.World, waitingOn)
				} else {
					fmt.Printf("  %s  %s  (%s)  [%s]\n", st.WorkItemID, title, st.World, st.WorkItemStatus)
				}
			}
		}

		return nil
	},
}

// --- sol caravan status ---

var caravanStatusCmd = &cobra.Command{
	Use:          "status [<caravan-id>]",
	Short:        "Show caravan status",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		// Detailed status for a specific caravan.
		if len(args) == 1 {
			caravanID := args[0]
			caravan, err := sphereStore.GetCaravan(caravanID)
			if err != nil {
				return err
			}

			statuses, err := sphereStore.CheckCaravanReadiness(caravanID, gatedWorldOpener)
			if err != nil {
				return err
			}

			if jsonOut {
				out := struct {
					ID     string                    `json:"id"`
					Name   string                    `json:"name"`
					Status string                    `json:"status"`
					Items  []store.CaravanItemStatus `json:"items"`
				}{
					ID:     caravan.ID,
					Name:   caravan.Name,
					Status: caravan.Status,
					Items:  statuses,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			fmt.Printf("Caravan: %s (%s)\n", caravan.Name, caravan.ID)
			fmt.Printf("Status: %s\n", caravan.Status)
			fmt.Println()

			// Check if phases are used.
			hasPhases := false
			for _, st := range statuses {
				if st.Phase > 0 {
					hasPhases = true
					break
				}
			}

			for _, st := range statuses {
				title := itemTitle(st.WorkItemID, st.World)
				marker := "[ ]"
				suffix := ""
				switch {
				case st.WorkItemStatus == "closed":
					marker = "[x]"
				case st.WorkItemStatus == "done":
					marker = "[~]"
					suffix = " (awaiting merge)"
				case st.WorkItemStatus == "open" && st.Ready:
					marker = "[>]"
					suffix = " (ready)"
				default:
					waitingOn := blockedByList(st.WorkItemID, st.World)
					if waitingOn != "" {
						suffix = " <- waiting on " + waitingOn
					} else {
						suffix = fmt.Sprintf(" [%s]", st.WorkItemStatus)
					}
				}
				phasePrefix := ""
				if hasPhases {
					phasePrefix = fmt.Sprintf("[p%d] ", st.Phase)
				}
				fmt.Printf("  %s %s%s  %s  (%s)%s\n", marker, phasePrefix, st.WorkItemID, title, st.World, suffix)
			}
			return nil
		}

		// List all open caravans.
		caravans, err := sphereStore.ListCaravans("open")
		if err != nil {
			return err
		}

		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(caravans)
		}

		if len(caravans) == 0 {
			fmt.Println("No open caravans.")
			return nil
		}

		fmt.Println("Open caravans:")
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, c := range caravans {
			items, err := sphereStore.ListCaravanItems(c.ID)
			if err != nil {
				return err
			}

			// Count statuses.
			var closedCount, mergingCount, readyCount, blockedCount int
			statuses, err := sphereStore.CheckCaravanReadiness(c.ID, gatedWorldOpener)
			if err != nil {
				// If we can't check readiness, just show item count.
				fmt.Fprintf(tw, "  %s\t%s\t%d items\n", c.ID, c.Name, len(items))
				continue
			}
			for _, st := range statuses {
				switch {
				case st.WorkItemStatus == "closed":
					closedCount++
				case st.WorkItemStatus == "done":
					mergingCount++
				case st.WorkItemStatus == "open" && st.Ready:
					readyCount++
				default:
					blockedCount++
				}
			}
			fmt.Fprintf(tw, "  %s\t%s\t%d items\t(%d closed, %d merging, %d ready, %d blocked)\n",
				c.ID, c.Name, len(items), closedCount, mergingCount, readyCount, blockedCount)
		}
		tw.Flush()
		return nil
	},
}

// --- sol caravan launch ---

var caravanLaunchCmd = &cobra.Command{
	Use:          "launch <caravan-id>",
	Short:        "Dispatch ready items in a caravan",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]

		world, _ := cmd.Flags().GetString("world")
		if world == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(world); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		statuses, err := sphereStore.CheckCaravanReadiness(caravanID, gatedWorldOpener)
		if err != nil {
			return err
		}

		// Find ready items in the specified world.
		var readyItems []store.CaravanItemStatus
		var blockedItems []store.CaravanItemStatus
		for _, st := range statuses {
			if st.World != world {
				continue
			}
			if st.WorkItemStatus == "open" && st.Ready {
				readyItems = append(readyItems, st)
			} else if st.WorkItemStatus != "done" && st.WorkItemStatus != "closed" {
				blockedItems = append(blockedItems, st)
			}
		}

		if len(readyItems) == 0 {
			fmt.Println("No ready items to dispatch in this world.")
			if len(blockedItems) > 0 {
				fmt.Printf("%d items still blocked.\n", len(blockedItems))
			}
			return nil
		}

		// Config-first source repo discovery.
		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return err
		}
		sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
		if err != nil {
			return err
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		mgr := dispatch.NewSessionManager()
		logger := events.NewLogger(config.Home())

		// Parse --var flags into a map.
		vars, err := parseVarFlags(caravanVars)
		if err != nil {
			return err
		}

		dispatched := 0
		for _, st := range readyItems {
			castOpts := dispatch.CastOpts{
				WorkItemID:  st.WorkItemID,
				World:       world,
				SourceRepo:  sourceRepo,
				WorldConfig: &worldCfg,
			}
			if caravanFormula != "" {
				castOpts.Formula = caravanFormula
				castOpts.Variables = vars
			}
			result, err := dispatch.Cast(castOpts, worldStore, sphereStore, mgr, logger)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to dispatch %s: %v\n", st.WorkItemID, err)
				continue
			}
			fmt.Printf("Dispatched %s -> %s (%s)\n", result.WorkItemID, result.AgentName, result.SessionName)
			dispatched++
		}

		logger.Emit(events.EventCaravanLaunched, "sol", "operator", "both", map[string]string{
			"caravan_id": caravanID,
			"world":      world,
			"dispatched": fmt.Sprintf("%d", dispatched),
		})

		fmt.Printf("\nDispatched %d items.\n", dispatched)
		if len(blockedItems) > 0 {
			fmt.Printf("%d items still blocked.\n", len(blockedItems))
		}

		// Try to auto-close.
		closed, err := sphereStore.TryCloseCaravan(caravanID, gatedWorldOpener)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to check caravan closure: %v\n", err)
		} else if closed {
			caravan, _ := sphereStore.GetCaravan(caravanID)
			carName := caravanID
			if caravan != nil {
				carName = caravan.Name
			}
			logger.Emit(events.EventCaravanClosed, "sol", "operator", "both", map[string]string{
				"caravan_id": caravanID,
				"name":       carName,
			})
			fmt.Println("Caravan auto-closed (all items complete).")
		}

		return nil
	},
}

// helpers

func itemTitle(workItemID, world string) string {
	worldStore, err := gatedWorldOpener(world)
	if err != nil {
		return "(unknown)"
	}
	defer worldStore.Close()
	item, err := worldStore.GetWorkItem(workItemID)
	if err != nil {
		return "(unknown)"
	}
	return item.Title
}

func blockedByList(workItemID, world string) string {
	worldStore, err := gatedWorldOpener(world)
	if err != nil {
		return ""
	}
	defer worldStore.Close()
	deps, err := worldStore.GetDependencies(workItemID)
	if err != nil || len(deps) == 0 {
		return ""
	}

	// Filter to unsatisfied deps — only "closed" (merged) satisfies.
	var unsatisfied []string
	for _, depID := range deps {
		item, err := worldStore.GetWorkItem(depID)
		if err != nil {
			unsatisfied = append(unsatisfied, depID)
			continue
		}
		if item.Status != "closed" {
			unsatisfied = append(unsatisfied, depID)
		}
	}
	return strings.Join(unsatisfied, ", ")
}

func init() {
	rootCmd.AddCommand(caravanCmd)
	caravanCmd.AddCommand(caravanCreateCmd)
	caravanCmd.AddCommand(caravanAddCmd)
	caravanCmd.AddCommand(caravanCheckCmd)
	caravanCmd.AddCommand(caravanStatusCmd)
	caravanCmd.AddCommand(caravanLaunchCmd)

	// create flags
	caravanCreateCmd.Flags().String("world", "", "world name")
	caravanCreateCmd.Flags().StringVar(&caravanOwner, "owner", "", "caravan owner (default: operator)")
	caravanCreateCmd.Flags().Int("phase", 0, "phase for items (default 0)")

	// add flags
	caravanAddCmd.Flags().String("world", "", "world name")
	caravanAddCmd.Flags().Int("phase", 0, "phase for items (default 0)")
	caravanAddCmd.MarkFlagRequired("world")

	// check flags
	caravanCheckCmd.Flags().Bool("json", false, "output as JSON")

	// status flags
	caravanStatusCmd.Flags().Bool("json", false, "output as JSON")

	// launch flags
	caravanLaunchCmd.Flags().String("world", "", "world name")
	caravanLaunchCmd.Flags().StringVar(&caravanFormula, "formula", "", "workflow formula for dispatched items")
	caravanLaunchCmd.Flags().StringSliceVar(&caravanVars, "var", nil, "variable assignment (key=val)")
	caravanLaunchCmd.MarkFlagRequired("world")
}
