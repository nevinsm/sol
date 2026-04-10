package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

// caravanPhaseStats counts writs in a single phase by lifecycle bucket.
// Buckets are mutually exclusive: each item lands in exactly one of
// merged / inProgress / ready / blocked.
type caravanPhaseStats struct {
	Total      int `json:"total"`
	Merged     int `json:"merged"`
	InProgress int `json:"in_progress"`
	Ready      int `json:"ready"`
	Blocked    int `json:"blocked"`
}

// formatCaravanWorlds returns the WORLDS column value for a caravan: a
// comma-separated list of distinct worlds spanned by items, sorted for
// stable output. Truncates to the first three names with a "+N" suffix
// if more worlds are present. Returns cliformat.EmptyMarker for an empty
// caravan.
func formatCaravanWorlds(items []store.CaravanItem) string {
	if len(items) == 0 {
		return cliformat.EmptyMarker
	}
	seen := map[string]struct{}{}
	var worlds []string
	for _, it := range items {
		if _, ok := seen[it.World]; ok {
			continue
		}
		seen[it.World] = struct{}{}
		worlds = append(worlds, it.World)
	}
	sort.Strings(worlds)
	if len(worlds) <= 3 {
		return strings.Join(worlds, ",")
	}
	return fmt.Sprintf("%s+%d", strings.Join(worlds[:3], ","), len(worlds)-3)
}

// computeCaravanPhaseProgress groups caravan item statuses by phase and
// counts each phase by lifecycle bucket. The returned map is keyed by
// phase number. If statuses is shorter than items (e.g. readiness check
// failed), items missing a status are counted as blocked so the totals
// still match len(items).
func computeCaravanPhaseProgress(items []store.CaravanItem, statuses []store.CaravanItemStatus) map[int]*caravanPhaseStats {
	progress := map[int]*caravanPhaseStats{}
	statusByWrit := map[string]store.CaravanItemStatus{}
	for _, st := range statuses {
		statusByWrit[st.WritID] = st
	}
	for _, it := range items {
		ps, ok := progress[it.Phase]
		if !ok {
			ps = &caravanPhaseStats{}
			progress[it.Phase] = ps
		}
		ps.Total++
		st, hasStatus := statusByWrit[it.WritID]
		switch {
		case !hasStatus:
			ps.Blocked++
		case st.WritStatus == "closed":
			ps.Merged++
		case st.IsDispatched():
			ps.InProgress++
		case st.Ready:
			ps.Ready++
		default:
			ps.Blocked++
		}
	}
	return progress
}

// formatCaravanProgress renders a phase-progress map as a compact column
// value. Phases with zero items are skipped. When only one phase has any
// items the "pN:" prefix is dropped, leaving "merged/total". An entirely
// empty caravan renders as "0/0".
func formatCaravanProgress(progress map[int]*caravanPhaseStats) string {
	if len(progress) == 0 {
		return "0/0"
	}
	phases := make([]int, 0, len(progress))
	for p, ps := range progress {
		if ps.Total == 0 {
			continue
		}
		phases = append(phases, p)
	}
	if len(phases) == 0 {
		return "0/0"
	}
	sort.Ints(phases)
	if len(phases) == 1 {
		ps := progress[phases[0]]
		return fmt.Sprintf("%d/%d", ps.Merged, ps.Total)
	}
	parts := make([]string, 0, len(phases))
	for _, p := range phases {
		ps := progress[p]
		parts = append(parts, fmt.Sprintf("p%d:%d/%d", p, ps.Merged, ps.Total))
	}
	return strings.Join(parts, " ")
}

var (
	caravanOwner         string
	caravanPhase         int
	caravanGuidelines    string
	caravanVars          []string
	caravanDeleteConfirm bool
)

var caravanCmd = &cobra.Command{
	Use:     "caravan",
	Short:   "Manage caravans (grouped writ batches)",
	GroupID: groupWrits,
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

		worldFlag, _ := cmd.Flags().GetString("world")
		world := worldFlag
		if world != "" || len(itemIDs) > 0 {
			var err error
			world, err = config.ResolveWorld(worldFlag)
			if err != nil {
				return err
			}
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		owner := caravanOwner
		if owner == "" {
			owner = config.Autarch
		}

		caravanID, err := sphereStore.CreateCaravan(name, owner)
		if err != nil {
			return fmt.Errorf("failed to create caravan: %w", err)
		}

		phase, _ := cmd.Flags().GetInt("phase")
		for _, itemID := range itemIDs {
			if err := sphereStore.CreateCaravanItem(caravanID, itemID, world, phase); err != nil {
				return fmt.Errorf("failed to add item %s to caravan: %w", itemID, err)
			}
		}

		logger := events.NewLogger(config.Home())
		logger.Emit(events.EventCaravanCreated, "sol", config.Autarch, "both", map[string]string{
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
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}
		itemIDs := args[1:]
		for _, id := range itemIDs {
			if err := config.ValidateWritID(id); err != nil {
				return err
			}
		}

		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		// Guard against adding to closed caravans.
		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}
		if caravan.Status == "closed" {
			return fmt.Errorf("caravan %s is closed — reopen it first with: sol caravan reopen %s", caravanID, caravanID)
		}

		phase, _ := cmd.Flags().GetInt("phase")
		for _, itemID := range itemIDs {
			if err := sphereStore.CreateCaravanItem(caravanID, itemID, world, phase); err != nil {
				return fmt.Errorf("failed to add item %s to caravan: %w", itemID, err)
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
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}

		statuses, err := sphereStore.CheckCaravanReadiness(caravanID, gatedWorldOpener)
		if err != nil {
			return fmt.Errorf("failed to check caravan readiness: %w", err)
		}

		// Check caravan-level dependencies.
		unsatisfiedCaravanDeps, _ := sphereStore.UnsatisfiedCaravanDependencies(caravanID)

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			out := struct {
				ID        string                    `json:"id"`
				Name      string                    `json:"name"`
				Status    string                    `json:"status"`
				BlockedBy []string                  `json:"blocked_by_caravans,omitempty"`
				Items     []store.CaravanItemStatus `json:"items"`
			}{
				ID:        caravan.ID,
				Name:      caravan.Name,
				Status:    string(caravan.Status),
				BlockedBy: unsatisfiedCaravanDeps,
				Items:     statuses,
			}
			return printJSON(out)
		}

		fmt.Printf("Caravan: %s (%s)\n", caravan.Name, caravan.ID)
		fmt.Printf("Status: %s\n", caravan.Status)
		if len(unsatisfiedCaravanDeps) > 0 {
			fmt.Printf("Blocked by caravans: %s\n", caravanDepNames(sphereStore, unsatisfiedCaravanDeps))
		}
		fmt.Println()

		// Separate ready, in progress, awaiting merge, and blocked items.
		var ready, inProgress, awaitingMerge, blocked []store.CaravanItemStatus
		for _, st := range statuses {
			if st.WritStatus == "closed" {
				// fully merged, skip for now
			} else if st.WritStatus == "done" {
				awaitingMerge = append(awaitingMerge, st)
			} else if st.IsDispatched() {
				inProgress = append(inProgress, st)
			} else if st.WritStatus == "open" && st.Ready {
				ready = append(ready, st)
			} else {
				blocked = append(blocked, st)
			}
		}

		if len(ready) > 0 {
			fmt.Println("Ready for dispatch:")
			for _, st := range ready {
				title := itemTitle(st.WritID, st.World)
				fmt.Printf("  %s  %s  (%s)\n", st.WritID, title, st.World)
			}
			fmt.Println()
		}

		if len(inProgress) > 0 {
			fmt.Println("In progress:")
			for _, st := range inProgress {
				title := itemTitle(st.WritID, st.World)
				agent := ""
				if st.Assignee != "" {
					agent = fmt.Sprintf("  [%s]", agentShortName(st.Assignee))
				}
				fmt.Printf("  %s  %s  (%s)%s\n", st.WritID, title, st.World, agent)
			}
			fmt.Println()
		}

		if len(awaitingMerge) > 0 {
			fmt.Println("Awaiting merge:")
			for _, st := range awaitingMerge {
				title := itemTitle(st.WritID, st.World)
				fmt.Printf("  %s  %s  (%s)\n", st.WritID, title, st.World)
			}
			fmt.Println()
		}

		if len(blocked) > 0 {
			fmt.Println("Blocked:")
			for _, st := range blocked {
				title := itemTitle(st.WritID, st.World)
				waitingOn := blockedByList(st.WritID, st.World)
				if waitingOn != "" {
					fmt.Printf("  %s  %s  (%s)  <- waiting on %s\n", st.WritID, title, st.World, waitingOn)
				} else {
					fmt.Printf("  %s  %s  (%s)  [%s]\n", st.WritID, title, st.World, st.WritStatus)
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
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		// Detailed status for a specific caravan.
		if len(args) == 1 {
			caravanID := args[0]
			if err := config.ValidateCaravanID(caravanID); err != nil {
				return err
			}
			caravan, err := sphereStore.GetCaravan(caravanID)
			if err != nil {
				return fmt.Errorf("failed to get caravan: %w", err)
			}

			statuses, err := sphereStore.CheckCaravanReadiness(caravanID, gatedWorldOpener)
			if err != nil {
				return fmt.Errorf("failed to check caravan readiness: %w", err)
			}

			// Check caravan-level dependencies.
			unsatisfiedCaravanDeps, _ := sphereStore.UnsatisfiedCaravanDependencies(caravanID)

			if jsonOut {
				out := struct {
					ID        string                    `json:"id"`
					Name      string                    `json:"name"`
					Status    string                    `json:"status"`
					BlockedBy []string                  `json:"blocked_by_caravans,omitempty"`
					Items     []store.CaravanItemStatus `json:"items"`
				}{
					ID:        caravan.ID,
					Name:      caravan.Name,
					Status:    string(caravan.Status),
					BlockedBy: unsatisfiedCaravanDeps,
					Items:     statuses,
				}
				return printJSON(out)
			}

			fmt.Printf("Caravan: %s (%s)\n", caravan.Name, caravan.ID)
			fmt.Printf("Status: %s\n", caravan.Status)
			if len(unsatisfiedCaravanDeps) > 0 {
				fmt.Printf("Blocked by caravans: %s\n", caravanDepNames(sphereStore, unsatisfiedCaravanDeps))
			}
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
				title := itemTitle(st.WritID, st.World)
				marker := "[ ]"
				suffix := ""
				switch {
				case st.WritStatus == "closed":
					marker = "[x]"
				case st.WritStatus == "done":
					marker = "[~]"
					suffix = " (awaiting merge)"
				case st.IsDispatched():
					marker = "[w]"
					suffix = " (in progress)"
					if st.Assignee != "" {
						suffix = fmt.Sprintf(" (in progress: %s)", agentShortName(st.Assignee))
					}
				case st.WritStatus == "open" && st.Ready:
					marker = "[>]"
					suffix = " (ready)"
				default:
					waitingOn := blockedByList(st.WritID, st.World)
					if waitingOn != "" {
						suffix = " <- waiting on " + waitingOn
					} else {
						suffix = fmt.Sprintf(" [%s]", st.WritStatus)
					}
				}
				phasePrefix := ""
				if hasPhases {
					phasePrefix = fmt.Sprintf("[p%d] ", st.Phase)
				}
				fmt.Printf("  %s %s%s  %s  (%s)%s\n", marker, phasePrefix, st.WritID, title, st.World, suffix)
			}
			return nil
		}

		// List all active caravans (drydock + open).
		allCaravans, err := sphereStore.ListCaravans("")
		if err != nil {
			return fmt.Errorf("failed to list caravans: %w", err)
		}
		var caravans []store.Caravan
		for _, c := range allCaravans {
			if c.Status == "drydock" || c.Status == "open" || c.Status == "ready" {
				caravans = append(caravans, c)
			}
		}

		if jsonOut {
			return printJSON(caravans)
		}

		if len(caravans) == 0 {
			fmt.Println("No active caravans.")
			return nil
		}

		fmt.Println("Active caravans:")
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, c := range caravans {
			items, err := sphereStore.ListCaravanItems(c.ID)
			if err != nil {
				return fmt.Errorf("failed to list caravan items: %w", err)
			}

			if c.Status == "drydock" {
				fmt.Fprintf(tw, "  %s\t%s\t%d items\t(drydock)\n", c.ID, c.Name, len(items))
				continue
			}

			// Count statuses.
			var closedCount, mergingCount, readyCount, dispatchedCount, blockedCount int
			statuses, err := sphereStore.CheckCaravanReadiness(c.ID, gatedWorldOpener)
			if err != nil {
				// If we can't check readiness, just show item count.
				fmt.Fprintf(tw, "  %s\t%s\t%d items\n", c.ID, c.Name, len(items))
				continue
			}
			for _, st := range statuses {
				switch {
				case st.WritStatus == "closed":
					closedCount++
				case st.WritStatus == "done":
					mergingCount++
				case st.IsDispatched():
					dispatchedCount++
				case st.WritStatus == "open" && st.Ready:
					readyCount++
				default:
					blockedCount++
				}
			}
			summary := fmt.Sprintf("%d closed", closedCount)
			if mergingCount > 0 {
				summary += fmt.Sprintf(", %d merging", mergingCount)
			}
			if dispatchedCount > 0 {
				summary += fmt.Sprintf(", %d in progress", dispatchedCount)
			}
			if readyCount > 0 {
				summary += fmt.Sprintf(", %d ready", readyCount)
			}
			if blockedCount > 0 {
				summary += fmt.Sprintf(", %d blocked", blockedCount)
			}
			fmt.Fprintf(tw, "  %s\t%s\t%d items\t(%s)\n",
				c.ID, c.Name, len(items), summary)
		}
		tw.Flush()
		return nil
	},
}

// --- sol caravan list ---

var caravanListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List caravans with optional status filtering",
	Long:         "List all caravans. Shows active (non-closed) caravans by default. Use --all for all caravans or --status to filter.",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		showAll, _ := cmd.Flags().GetBool("all")
		statusFilter, _ := cmd.Flags().GetString("status")

		if showAll && statusFilter != "" {
			return fmt.Errorf("--all and --status are mutually exclusive")
		}

		if statusFilter != "" {
			validStatuses := []string{"drydock", "sailing", "arrived", "closed"}
			valid := false
			for _, s := range validStatuses {
				if statusFilter == s {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("invalid status %q: valid values are %s", statusFilter, strings.Join(validStatuses, ", "))
			}
		}

		// Default: active (non-closed). --all: all. --status: specific.
		filter := ""
		excludeClosed := true
		if showAll {
			excludeClosed = false
		} else if statusFilter != "" {
			filter = statusFilter
			excludeClosed = false
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		caravans, err := sphereStore.ListCaravans(store.CaravanStatus(filter))
		if err != nil {
			return fmt.Errorf("failed to list caravans: %w", err)
		}

		if excludeClosed {
			var active []store.Caravan
			for _, c := range caravans {
				if c.Status != store.CaravanClosed {
					active = append(active, c)
				}
			}
			caravans = active
		}

		// Pre-compute per-caravan items, statuses, worlds and phase progress
		// once so the JSON and table branches share a single data source.
		type caravanRow struct {
			caravan  store.Caravan
			items    []store.CaravanItem
			statuses []store.CaravanItemStatus
			worlds   string
			progress map[int]*caravanPhaseStats
			merged   int
			total    int
		}
		rows := make([]caravanRow, 0, len(caravans))
		for _, c := range caravans {
			items, _ := sphereStore.ListCaravanItems(c.ID)
			var statuses []store.CaravanItemStatus
			if c.Status == store.CaravanClosed {
				// All items in a closed caravan are merged by definition;
				// synthesize statuses so phase progress reflects that
				// without paying for a readiness check.
				statuses = make([]store.CaravanItemStatus, 0, len(items))
				for _, it := range items {
					statuses = append(statuses, store.CaravanItemStatus{
						WritID:     it.WritID,
						World:      it.World,
						Phase:      it.Phase,
						WritStatus: "closed",
					})
				}
			} else {
				if s, err := sphereStore.CheckCaravanReadiness(c.ID, gatedWorldOpener); err == nil {
					statuses = s
				}
			}
			progress := computeCaravanPhaseProgress(items, statuses)
			merged := 0
			for _, ps := range progress {
				merged += ps.Merged
			}
			rows = append(rows, caravanRow{
				caravan:  c,
				items:    items,
				statuses: statuses,
				worlds:   formatCaravanWorlds(items),
				progress: progress,
				merged:   merged,
				total:    len(items),
			})
		}

		if jsonOut {
			type caravanListEntry struct {
				ID            string                       `json:"id"`
				Name          string                       `json:"name"`
				Status        string                       `json:"status"`
				Owner         string                       `json:"owner"`
				Items         int                          `json:"items"`
				Merged        int                          `json:"merged"`
				Worlds        []string                     `json:"worlds"`
				PhaseProgress map[string]caravanPhaseStats `json:"phase_progress"`
				CreatedAt     string                       `json:"created_at"`
				ClosedAt      *string                      `json:"closed_at,omitempty"`
			}
			entries := make([]caravanListEntry, 0, len(rows))
			for _, r := range rows {
				// Distinct, sorted worlds for the JSON array.
				seen := map[string]struct{}{}
				worldList := []string{}
				for _, it := range r.items {
					if _, ok := seen[it.World]; ok {
						continue
					}
					seen[it.World] = struct{}{}
					worldList = append(worldList, it.World)
				}
				sort.Strings(worldList)

				phaseMap := map[string]caravanPhaseStats{}
				for phase, ps := range r.progress {
					phaseMap[fmt.Sprintf("%d", phase)] = *ps
				}

				entry := caravanListEntry{
					ID:            r.caravan.ID,
					Name:          r.caravan.Name,
					Status:        string(r.caravan.Status),
					Owner:         r.caravan.Owner,
					Items:         r.total,
					Merged:        r.merged,
					Worlds:        worldList,
					PhaseProgress: phaseMap,
					CreatedAt:     cliformat.FormatTimestamp(r.caravan.CreatedAt),
				}
				if r.caravan.ClosedAt != nil {
					s := cliformat.FormatTimestamp(*r.caravan.ClosedAt)
					entry.ClosedAt = &s
				}
				entries = append(entries, entry)
			}
			return printJSON(entries)
		}

		if len(caravans) == 0 {
			if excludeClosed {
				fmt.Println("No active caravans.")
			} else if filter != "" {
				fmt.Printf("No caravans (status: %s).\n", filter)
			} else {
				fmt.Println("No caravans.")
			}
			return nil
		}

		now := time.Now()
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tNAME\tSTATUS\tWORLDS\tPROGRESS\tCREATED\n")
		for _, r := range rows {
			name := r.caravan.Name
			if name == "" {
				name = cliformat.EmptyMarker
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				r.caravan.ID,
				name,
				r.caravan.Status,
				r.worlds,
				formatCaravanProgress(r.progress),
				cliformat.FormatTimestampOrRelative(r.caravan.CreatedAt, now),
			)
		}
		tw.Flush()
		fmt.Printf("\n%s\n", cliformat.FormatCount(len(rows), "caravan", "caravans"))
		return nil
	},
}

// --- sol caravan commission ---

var caravanCommissionCmd = &cobra.Command{
	Use:          "commission <caravan-id>",
	Short:        "Commission a caravan (drydock → open)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}

		if caravan.Status != "drydock" {
			return fmt.Errorf("caravan %s is %q, not drydock — only drydock caravans can be commissioned", caravanID, caravan.Status)
		}

		if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
			return fmt.Errorf("failed to commission caravan: %w", err)
		}

		fmt.Printf("Commissioned caravan %s: %q (drydock → open)\n", caravanID, caravan.Name)
		return nil
	},
}

// --- sol caravan drydock ---

var caravanDrydockCmd = &cobra.Command{
	Use:          "drydock <caravan-id>",
	Short:        "Return a caravan to drydock (open → drydock)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}

		if caravan.Status != "open" {
			return fmt.Errorf("caravan %s is %q, not open — only open caravans can be returned to drydock", caravanID, caravan.Status)
		}

		if err := sphereStore.UpdateCaravanStatus(caravanID, "drydock"); err != nil {
			return fmt.Errorf("failed to drydock caravan: %w", err)
		}

		fmt.Printf("Returned caravan %s: %q to drydock (open → drydock)\n", caravanID, caravan.Name)
		return nil
	},
}

// --- sol caravan reopen ---

var caravanReopenCmd = &cobra.Command{
	Use:   "reopen <caravan-id>",
	Short: "Reopen a closed caravan (closed → drydock)",
	Long: `Move a closed caravan back to drydock status for modification. Only closed
caravans can be reopened. After reopening, commission the caravan to make it
dispatchable again.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}

		if caravan.Status != "closed" {
			return fmt.Errorf("caravan %s is %q, not closed — only closed caravans can be reopened", caravanID, caravan.Status)
		}

		if err := sphereStore.UpdateCaravanStatus(caravanID, "drydock"); err != nil {
			return fmt.Errorf("failed to reopen caravan: %w", err)
		}

		fmt.Printf("Reopened caravan %s → drydock\n", caravanID)
		return nil
	},
}

// --- sol caravan launch ---

var caravanLaunchCmd = &cobra.Command{
	Use:   "launch <caravan-id>",
	Short: "Dispatch ready items in a caravan",
	Long: `Check readiness of all items in the caravan and dispatch those that are
ready (open, unblocked) in the specified world. Items blocked by dependencies
or in earlier phases are skipped.

Drydock caravans must be commissioned first. Auto-closes the caravan if all
items complete after dispatch. Use --guidelines to select a specific guidelines
template for dispatched writs.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}

		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		// Reject launch for drydock caravans.
		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}
		if caravan.Status == "drydock" {
			return fmt.Errorf("caravan %s (%q) is in drydock — commission it first with: sol caravan commission %s", caravanID, caravan.Name, caravanID)
		}

		statuses, err := sphereStore.CheckCaravanReadiness(caravanID, gatedWorldOpener)
		if err != nil {
			return fmt.Errorf("failed to check caravan readiness: %w", err)
		}

		// Find ready items in the specified world.
		var readyItems []store.CaravanItemStatus
		var blockedItems []store.CaravanItemStatus
		for _, st := range statuses {
			if st.World != world {
				continue
			}
			if st.WritStatus == "open" && st.Ready {
				readyItems = append(readyItems, st)
			} else if st.WritStatus != "done" && st.WritStatus != "closed" {
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
			return fmt.Errorf("failed to load world config: %w", err)
		}
		sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
		if err != nil {
			return fmt.Errorf("failed to resolve source repo: %w", err)
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
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
				WritID:  st.WritID,
				World:       world,
				SourceRepo:  sourceRepo,
				WorldConfig: &worldCfg,
			}
			if caravanGuidelines != "" {
				castOpts.Guidelines = caravanGuidelines
				castOpts.Variables = vars
			}
			result, err := dispatch.Cast(cmd.Context(), castOpts, worldStore, sphereStore, mgr, logger)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to dispatch %s: %v\n", st.WritID, err)
				continue
			}
			fmt.Printf("Dispatched %s -> %s (%s)\n", result.WritID, result.AgentName, result.SessionName)
			dispatched++
		}

		logger.Emit(events.EventCaravanLaunched, "sol", config.Autarch, "both", map[string]string{
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
			logger.Emit(events.EventCaravanClosed, "sol", config.Autarch, "both", map[string]string{
				"caravan_id": caravanID,
				"name":       carName,
			})
			fmt.Println("Caravan auto-closed (all items complete).")
		}

		return nil
	},
}

// --- sol caravan remove ---

var caravanRemoveCmd = &cobra.Command{
	Use:          "remove <caravan-id> <item-id>",
	Short:        "Remove an item from a caravan",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}
		itemID := args[1]
		if err := config.ValidateWritID(itemID); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		// Verify caravan exists.
		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}

		// Guard against removing from closed caravans.
		if caravan.Status == "closed" {
			return fmt.Errorf("caravan %s is closed — reopen it first with: sol caravan reopen %s", caravanID, caravanID)
		}

		if err := sphereStore.RemoveCaravanItem(caravanID, itemID); err != nil {
			return fmt.Errorf("failed to remove item %s from caravan: %w", itemID, err)
		}

		fmt.Printf("Removed %s from caravan %s (%q)\n", itemID, caravanID, caravan.Name)
		return nil
	},
}

// --- sol caravan delete ---

var caravanDeleteCmd = &cobra.Command{
	Use:   "delete <caravan-id>",
	Short: "Delete a drydocked or closed caravan entirely",
	Long: `Delete a drydocked or closed caravan entirely.

Requires --confirm to proceed; without it, prints what would be deleted and exits.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		caravanID := args[0]
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}

		if caravan.Status != "drydock" && caravan.Status != "closed" {
			return fmt.Errorf("caravan %s is %q — only drydocked or closed caravans can be deleted", caravanID, caravan.Status)
		}

		items, err := sphereStore.ListCaravanItems(caravanID)
		if err != nil {
			return fmt.Errorf("failed to list caravan items: %w", err)
		}

		if !caravanDeleteConfirm {
			fmt.Printf("This will permanently delete caravan %s:\n", caravanID)
			fmt.Printf("  - Name:   %s\n", caravan.Name)
			fmt.Printf("  - Status: %s\n", caravan.Status)
			fmt.Printf("  - Items:  %d\n", len(items))
			fmt.Println()
			fmt.Println("Run with --confirm to proceed.")
			return &exitError{code: 1}
		}

		if err := sphereStore.DeleteCaravan(caravanID); err != nil {
			return fmt.Errorf("failed to delete caravan: %w", err)
		}

		fmt.Printf("Deleted caravan %s: %q\n", caravanID, caravan.Name)
		return nil
	},
}

// --- sol caravan set-phase ---

var caravanSetPhaseCmd = &cobra.Command{
	Use:   "set-phase <caravan-id> [<item-id>] <phase>",
	Short: "Update the phase of items in a caravan",
	Long:  "Update the phase of a single item, or use --all to update all items in the caravan.",
	Args: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		if all {
			if len(args) != 2 {
				return fmt.Errorf("usage: sol caravan set-phase <caravan-id> --all <phase>")
			}
		} else {
			if len(args) != 3 {
				return fmt.Errorf("usage: sol caravan set-phase <caravan-id> <item-id> <phase>")
			}
		}
		return nil
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		all, _ := cmd.Flags().GetBool("all")
		caravanID := args[0]
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}
		if !all {
			// args[1] is a writ ID when not using --all.
			if err := config.ValidateWritID(args[1]); err != nil {
				return err
			}
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		// Validate caravan exists.
		if _, err := sphereStore.GetCaravan(caravanID); err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}

		if all {
			phase, err := parsePhaseArg(args[1])
			if err != nil {
				return err
			}
			n, err := sphereStore.UpdateAllCaravanItemPhases(caravanID, phase)
			if err != nil {
				return fmt.Errorf("failed to update caravan item phases: %w", err)
			}
			fmt.Printf("Updated %d items to phase %d in caravan %s\n", n, phase, caravanID)
			return nil
		}

		itemID := args[1]
		phase, err := parsePhaseArg(args[2])
		if err != nil {
			return err
		}

		if err := sphereStore.UpdateCaravanItemPhase(caravanID, itemID, phase); err != nil {
			return fmt.Errorf("failed to update caravan item phase: %w", err)
		}
		fmt.Printf("Updated %s to phase %d in caravan %s\n", itemID, phase, caravanID)
		return nil
	},
}

func parsePhaseArg(s string) (int, error) {
	var phase int
	if _, err := fmt.Sscanf(s, "%d", &phase); err != nil {
		return 0, fmt.Errorf("invalid phase %q: must be an integer", s)
	}
	if phase < 0 {
		return 0, fmt.Errorf("invalid phase %d: must be non-negative", phase)
	}
	return phase, nil
}

// --- sol caravan close ---

var caravanCloseCmd = &cobra.Command{
	Use:          "close [<caravan-id>]",
	Short:        "Close a completed caravan",
	Long: `Close a caravan by ID, or use --auto to close all caravans where every item is merged.

Requires --confirm to proceed; without it, prints a preview of the caravan and exits.
Use --force to close even if not all items are merged (requires --confirm).`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		autoClose, _ := cmd.Flags().GetBool("auto")
		force, _ := cmd.Flags().GetBool("force")
		confirm, _ := cmd.Flags().GetBool("confirm")

		if len(args) == 0 && !autoClose {
			return fmt.Errorf("provide a <caravan-id> or use --auto")
		}
		if len(args) == 1 && autoClose {
			return fmt.Errorf("--auto scans all caravans; do not pass a caravan ID")
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return fmt.Errorf("failed to open sphere store: %w", err)
		}
		defer sphereStore.Close()

		logger := events.NewLogger(config.Home())

		if autoClose {
			caravans, err := sphereStore.ListCaravans("open")
			if err != nil {
				return fmt.Errorf("failed to list open caravans: %w", err)
			}
			closed := 0
			for _, c := range caravans {
				ok, err := sphereStore.TryCloseCaravan(c.ID, gatedWorldOpener)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to check caravan %s (%s): %v\n", c.ID, c.Name, err)
					continue
				}
				if ok {
					logger.Emit(events.EventCaravanClosed, "sol", config.Autarch, "both", map[string]string{
						"caravan_id": c.ID,
						"name":       c.Name,
					})
					fmt.Printf("Closed caravan %s: %q\n", c.ID, c.Name)
					closed++
				}
			}
			if closed == 0 {
				fmt.Println("No caravans ready to close (all items must be merged).")
			} else {
				fmt.Printf("\nClosed %d caravan(s).\n", closed)
			}
			return nil
		}

		// Close a specific caravan.
		caravanID := args[0]
		if err := config.ValidateCaravanID(caravanID); err != nil {
			return err
		}

		caravan, err := sphereStore.GetCaravan(caravanID)
		if err != nil {
			return fmt.Errorf("failed to get caravan: %w", err)
		}

		if caravan.Status == "closed" {
			fmt.Printf("Caravan %s (%q) is already closed.\n", caravanID, caravan.Name)
			return nil
		}

		// Dry-run preview when --confirm is not set.
		if !confirm {
			fmt.Printf("This will close caravan %s: %q\n", caravanID, caravan.Name)
			fmt.Printf("  Status: %s\n", caravan.Status)
			statuses, err := sphereStore.CheckCaravanReadiness(caravanID, gatedWorldOpener)
			if err == nil {
				var merged, unmerged int
				for _, st := range statuses {
					if st.WritStatus == "closed" {
						merged++
					} else {
						unmerged++
					}
				}
				fmt.Printf("  Items:  %d merged, %d unmerged\n", merged, unmerged)
				if unmerged > 0 {
					fmt.Println("  Unmerged items:")
					for _, st := range statuses {
						if st.WritStatus != "closed" {
							fmt.Printf("    %s (%s: %s)\n", st.WritID, st.World, st.WritStatus)
						}
					}
				}
			}
			fmt.Println()
			fmt.Println("Run with --confirm to proceed.")
			return &exitError{code: 1}
		}

		if !force {
			closed, err := sphereStore.TryCloseCaravan(caravanID, gatedWorldOpener)
			if err != nil {
				return fmt.Errorf("failed to close caravan: %w", err)
			}
			if !closed {
				// Show which items are not yet merged.
				statuses, err := sphereStore.CheckCaravanReadiness(caravanID, gatedWorldOpener)
				if err != nil {
					return fmt.Errorf("not all items are merged (use --force to close anyway)")
				}
				var unmerged []string
				for _, st := range statuses {
					if st.WritStatus != "closed" {
						unmerged = append(unmerged, fmt.Sprintf("%s (%s: %s)", st.WritID, st.World, st.WritStatus))
					}
				}
				return fmt.Errorf("not all items are merged; unmerged: %s (use --force to close anyway)",
					strings.Join(unmerged, ", "))
			}
		} else {
			if err := sphereStore.UpdateCaravanStatus(caravanID, "closed"); err != nil {
				return fmt.Errorf("failed to close caravan: %w", err)
			}
		}

		logger.Emit(events.EventCaravanClosed, "sol", config.Autarch, "both", map[string]string{
			"caravan_id": caravanID,
			"name":       caravan.Name,
		})

		fmt.Printf("Closed caravan %s: %q\n", caravanID, caravan.Name)
		return nil
	},
}

// helpers

// agentShortName extracts the agent name from a "world/agent-name" assignee string.
func agentShortName(assignee string) string {
	if i := strings.LastIndex(assignee, "/"); i >= 0 {
		return assignee[i+1:]
	}
	return assignee
}

func itemTitle(writID, world string) string {
	worldStore, err := gatedWorldOpener(world)
	if err != nil {
		return "(unknown)"
	}
	defer worldStore.Close()
	item, err := worldStore.GetWrit(writID)
	if err != nil {
		return "(unknown)"
	}
	return item.Title
}

func caravanDepNames(sphereStore *store.SphereStore, ids []string) string {
	var parts []string
	for _, id := range ids {
		c, err := sphereStore.GetCaravan(id)
		if err != nil {
			parts = append(parts, id)
		} else {
			parts = append(parts, fmt.Sprintf("%s (%s)", c.Name, id))
		}
	}
	return strings.Join(parts, ", ")
}

func blockedByList(writID, world string) string {
	worldStore, err := gatedWorldOpener(world)
	if err != nil {
		return ""
	}
	defer worldStore.Close()
	deps, err := worldStore.GetDependencies(writID)
	if err != nil || len(deps) == 0 {
		return ""
	}

	// Filter to unsatisfied deps — only "closed" (merged) satisfies.
	var unsatisfied []string
	for _, depID := range deps {
		item, err := worldStore.GetWrit(depID)
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
	caravanCmd.AddCommand(caravanListCmd)
	caravanCmd.AddCommand(caravanStatusCmd)
	caravanCmd.AddCommand(caravanLaunchCmd)
	caravanCmd.AddCommand(caravanCloseCmd)
	caravanCmd.AddCommand(caravanSetPhaseCmd)
	caravanCmd.AddCommand(caravanRemoveCmd)
	caravanCmd.AddCommand(caravanDeleteCmd)
	caravanCmd.AddCommand(caravanCommissionCmd)
	caravanCmd.AddCommand(caravanDrydockCmd)
	caravanCmd.AddCommand(caravanReopenCmd)

	// delete flags
	caravanDeleteCmd.Flags().BoolVar(&caravanDeleteConfirm, "confirm", false, "confirm deletion (without this flag, prints what would be deleted)")

	// set-phase flags
	caravanSetPhaseCmd.Flags().Bool("all", false, "update all items in the caravan")

	// close flags
	caravanCloseCmd.Flags().Bool("confirm", false, "confirm closure")
	caravanCloseCmd.Flags().Bool("force", false, "close even if not all items are merged")
	caravanCloseCmd.Flags().Bool("auto", false, "scan all open caravans and close any where all items are merged")

	// create flags
	caravanCreateCmd.Flags().String("world", "", "world name")
	caravanCreateCmd.Flags().StringVar(&caravanOwner, "owner", "", "caravan owner (default: autarch)")
	caravanCreateCmd.Flags().Int("phase", 0, "phase for items (default 0)")

	// add flags
	caravanAddCmd.Flags().String("world", "", "world name")
	caravanAddCmd.Flags().Int("phase", 0, "phase for items (default 0)")

	// check flags
	caravanCheckCmd.Flags().Bool("json", false, "output as JSON")

	// list flags
	caravanListCmd.Flags().Bool("json", false, "output as JSON")
	caravanListCmd.Flags().Bool("all", false, "include closed caravans")
	caravanListCmd.Flags().String("status", "", "filter by status (open, ready, closed)")

	// status flags
	caravanStatusCmd.Flags().Bool("json", false, "output as JSON")

	// launch flags
	caravanLaunchCmd.Flags().String("world", "", "world name")
	caravanLaunchCmd.Flags().StringVar(&caravanGuidelines, "guidelines", "", "guidelines template for dispatched items")
	caravanLaunchCmd.Flags().StringSliceVar(&caravanVars, "var", nil, "variable assignment (key=val)")
}
