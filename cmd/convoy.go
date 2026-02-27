package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var (
	convoyRig     string
	convoyOwner   string
	convoyJSON    bool
	convoyFormula string
	convoyVars    []string
)

var convoyCmd = &cobra.Command{
	Use:   "convoy",
	Short: "Manage convoys (grouped work item batches)",
}

// --- gt convoy create ---

var convoyCreateCmd = &cobra.Command{
	Use:   "create <name> [<item-id> ...]",
	Short: "Create a convoy with optional initial items",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		itemIDs := args[1:]

		if convoyRig == "" && len(itemIDs) > 0 {
			return fmt.Errorf("--rig is required when adding items")
		}

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		owner := convoyOwner
		if owner == "" {
			owner = "operator"
		}

		convoyID, err := townStore.CreateConvoy(name, owner)
		if err != nil {
			return err
		}

		for _, itemID := range itemIDs {
			if err := townStore.AddConvoyItem(convoyID, itemID, convoyRig); err != nil {
				return err
			}
		}

		fmt.Printf("Created convoy %s: %q (%d items)\n", convoyID, name, len(itemIDs))
		return nil
	},
}

// --- gt convoy add ---

var convoyAddCmd = &cobra.Command{
	Use:   "add <convoy-id> <item-id> [<item-id> ...]",
	Short: "Add items to an existing convoy",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		convoyID := args[0]
		itemIDs := args[1:]

		if convoyRig == "" {
			return fmt.Errorf("--rig is required")
		}

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		for _, itemID := range itemIDs {
			if err := townStore.AddConvoyItem(convoyID, itemID, convoyRig); err != nil {
				return err
			}
		}

		fmt.Printf("Added %d items to convoy %s\n", len(itemIDs), convoyID)
		return nil
	},
}

// --- gt convoy check ---

var convoyCheckCmd = &cobra.Command{
	Use:   "check <convoy-id>",
	Short: "Check readiness of convoy items",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		convoyID := args[0]

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		convoy, err := townStore.GetConvoy(convoyID)
		if err != nil {
			return err
		}

		statuses, err := townStore.CheckConvoyReadiness(convoyID, store.OpenRig)
		if err != nil {
			return err
		}

		if convoyJSON {
			out := struct {
				ID       string                    `json:"id"`
				Name     string                    `json:"name"`
				Status   string                    `json:"status"`
				Items    []store.ConvoyItemStatus   `json:"items"`
			}{
				ID:     convoy.ID,
				Name:   convoy.Name,
				Status: convoy.Status,
				Items:  statuses,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		fmt.Printf("Convoy: %s (%s)\n", convoy.Name, convoy.ID)
		fmt.Printf("Status: %s\n", convoy.Status)
		fmt.Println()

		// Separate ready and blocked items.
		var ready, blocked []store.ConvoyItemStatus
		for _, st := range statuses {
			if st.WorkItemStatus == "open" && st.Ready {
				ready = append(ready, st)
			} else if st.WorkItemStatus == "done" || st.WorkItemStatus == "closed" {
				// completed, skip for now
			} else {
				blocked = append(blocked, st)
			}
		}

		if len(ready) > 0 {
			fmt.Println("Ready for dispatch:")
			for _, st := range ready {
				title := itemTitle(st.WorkItemID, st.Rig)
				fmt.Printf("  %s  %s  (%s)\n", st.WorkItemID, title, st.Rig)
			}
			fmt.Println()
		}

		if len(blocked) > 0 {
			fmt.Println("Blocked:")
			for _, st := range blocked {
				title := itemTitle(st.WorkItemID, st.Rig)
				waitingOn := blockedByList(st.WorkItemID, st.Rig)
				if waitingOn != "" {
					fmt.Printf("  %s  %s  (%s)  <- waiting on %s\n", st.WorkItemID, title, st.Rig, waitingOn)
				} else {
					fmt.Printf("  %s  %s  (%s)  [%s]\n", st.WorkItemID, title, st.Rig, st.WorkItemStatus)
				}
			}
		}

		return nil
	},
}

// --- gt convoy status ---

var convoyStatusCmd = &cobra.Command{
	Use:   "status [<convoy-id>]",
	Short: "Show convoy status",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		// Detailed status for a specific convoy.
		if len(args) == 1 {
			convoyID := args[0]
			convoy, err := townStore.GetConvoy(convoyID)
			if err != nil {
				return err
			}

			statuses, err := townStore.CheckConvoyReadiness(convoyID, store.OpenRig)
			if err != nil {
				return err
			}

			if convoyJSON {
				out := struct {
					ID       string                    `json:"id"`
					Name     string                    `json:"name"`
					Status   string                    `json:"status"`
					Items    []store.ConvoyItemStatus   `json:"items"`
				}{
					ID:     convoy.ID,
					Name:   convoy.Name,
					Status: convoy.Status,
					Items:  statuses,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			fmt.Printf("Convoy: %s (%s)\n", convoy.Name, convoy.ID)
			fmt.Printf("Status: %s\n", convoy.Status)
			fmt.Println()

			for _, st := range statuses {
				title := itemTitle(st.WorkItemID, st.Rig)
				marker := "[ ]"
				suffix := ""
				switch {
				case st.WorkItemStatus == "done" || st.WorkItemStatus == "closed":
					marker = "[x]"
				case st.WorkItemStatus == "open" && st.Ready:
					marker = "[>]"
					suffix = " (ready)"
				default:
					waitingOn := blockedByList(st.WorkItemID, st.Rig)
					if waitingOn != "" {
						suffix = " <- waiting on " + waitingOn
					} else {
						suffix = fmt.Sprintf(" [%s]", st.WorkItemStatus)
					}
				}
				fmt.Printf("  %s %s  %s  (%s)%s\n", marker, st.WorkItemID, title, st.Rig, suffix)
			}
			return nil
		}

		// List all open convoys.
		convoys, err := townStore.ListConvoys("open")
		if err != nil {
			return err
		}

		if convoyJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(convoys)
		}

		if len(convoys) == 0 {
			fmt.Println("No open convoys.")
			return nil
		}

		fmt.Println("Open convoys:")
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, c := range convoys {
			items, err := townStore.ListConvoyItems(c.ID)
			if err != nil {
				return err
			}

			// Count statuses.
			var done, readyCount, blockedCount int
			statuses, err := townStore.CheckConvoyReadiness(c.ID, store.OpenRig)
			if err != nil {
				// If we can't check readiness, just show item count.
				fmt.Fprintf(tw, "  %s\t%s\t%d items\n", c.ID, c.Name, len(items))
				continue
			}
			for _, st := range statuses {
				switch {
				case st.WorkItemStatus == "done" || st.WorkItemStatus == "closed":
					done++
				case st.WorkItemStatus == "open" && st.Ready:
					readyCount++
				default:
					blockedCount++
				}
			}
			fmt.Fprintf(tw, "  %s\t%s\t%d items\t(%d done, %d ready, %d blocked)\n",
				c.ID, c.Name, len(items), done, readyCount, blockedCount)
		}
		tw.Flush()
		return nil
	},
}

// --- gt convoy launch ---

var convoyLaunchCmd = &cobra.Command{
	Use:   "launch <convoy-id>",
	Short: "Dispatch ready items in a convoy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		convoyID := args[0]

		if convoyRig == "" {
			return fmt.Errorf("--rig is required")
		}

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		statuses, err := townStore.CheckConvoyReadiness(convoyID, store.OpenRig)
		if err != nil {
			return err
		}

		// Find ready items in the specified rig.
		var readyItems []store.ConvoyItemStatus
		var blockedItems []store.ConvoyItemStatus
		for _, st := range statuses {
			if st.Rig != convoyRig {
				continue
			}
			if st.WorkItemStatus == "open" && st.Ready {
				readyItems = append(readyItems, st)
			} else if st.WorkItemStatus != "done" && st.WorkItemStatus != "closed" {
				blockedItems = append(blockedItems, st)
			}
		}

		if len(readyItems) == 0 {
			fmt.Println("No ready items to dispatch in this rig.")
			if len(blockedItems) > 0 {
				fmt.Printf("%d items still blocked.\n", len(blockedItems))
			}
			return nil
		}

		// Discover source repo.
		sourceRepo, err := dispatch.DiscoverSourceRepo()
		if err != nil {
			return fmt.Errorf("must run gt convoy launch from within a git repository: %w", err)
		}

		rigStore, err := store.OpenRig(convoyRig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		mgr := dispatch.NewSessionManager()
		logger := events.NewLogger(config.Home())

		dispatched := 0
		for _, st := range readyItems {
			result, err := dispatch.Sling(dispatch.SlingOpts{
				WorkItemID: st.WorkItemID,
				Rig:        convoyRig,
				SourceRepo: sourceRepo,
			}, rigStore, townStore, mgr, logger)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to dispatch %s: %v\n", st.WorkItemID, err)
				continue
			}
			fmt.Printf("Dispatched %s -> %s (%s)\n", result.WorkItemID, result.AgentName, result.SessionName)
			dispatched++
		}

		fmt.Printf("\nDispatched %d items.\n", dispatched)
		if len(blockedItems) > 0 {
			fmt.Printf("%d items still blocked.\n", len(blockedItems))
		}

		// Try to auto-close.
		closed, err := townStore.TryCloseConvoy(convoyID, store.OpenRig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to check convoy closure: %v\n", err)
		} else if closed {
			fmt.Println("Convoy auto-closed (all items complete).")
		}

		return nil
	},
}

// helpers

func itemTitle(workItemID, rig string) string {
	rigStore, err := store.OpenRig(rig)
	if err != nil {
		return "(unknown)"
	}
	defer rigStore.Close()
	item, err := rigStore.GetWorkItem(workItemID)
	if err != nil {
		return "(unknown)"
	}
	return item.Title
}

func blockedByList(workItemID, rig string) string {
	rigStore, err := store.OpenRig(rig)
	if err != nil {
		return ""
	}
	defer rigStore.Close()
	deps, err := rigStore.GetDependencies(workItemID)
	if err != nil || len(deps) == 0 {
		return ""
	}

	// Filter to unsatisfied deps.
	var unsatisfied []string
	for _, depID := range deps {
		item, err := rigStore.GetWorkItem(depID)
		if err != nil {
			unsatisfied = append(unsatisfied, depID)
			continue
		}
		if item.Status != "done" && item.Status != "closed" {
			unsatisfied = append(unsatisfied, depID)
		}
	}
	return strings.Join(unsatisfied, ", ")
}

func init() {
	rootCmd.AddCommand(convoyCmd)
	convoyCmd.AddCommand(convoyCreateCmd)
	convoyCmd.AddCommand(convoyAddCmd)
	convoyCmd.AddCommand(convoyCheckCmd)
	convoyCmd.AddCommand(convoyStatusCmd)
	convoyCmd.AddCommand(convoyLaunchCmd)

	// create flags
	convoyCreateCmd.Flags().StringVar(&convoyRig, "rig", "", "rig for the listed items")
	convoyCreateCmd.Flags().StringVar(&convoyOwner, "owner", "", "convoy owner (default: operator)")

	// add flags
	convoyAddCmd.Flags().StringVar(&convoyRig, "rig", "", "rig for the items")
	convoyAddCmd.MarkFlagRequired("rig")

	// check flags
	convoyCheckCmd.Flags().BoolVar(&convoyJSON, "json", false, "output as JSON")

	// status flags
	convoyStatusCmd.Flags().BoolVar(&convoyJSON, "json", false, "output as JSON")

	// launch flags
	convoyLaunchCmd.Flags().StringVar(&convoyRig, "rig", "", "rig to dispatch from")
	convoyLaunchCmd.Flags().StringVar(&convoyFormula, "formula", "", "workflow formula for dispatched items")
	convoyLaunchCmd.Flags().StringSliceVar(&convoyVars, "var", nil, "variable assignment (key=val)")
	convoyLaunchCmd.MarkFlagRequired("rig")
}
