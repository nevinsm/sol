package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var writCmd = &cobra.Command{
	Use:   "writ",
	Short: "Manage writs",
	GroupID: groupWrits,
}

func init() {
	rootCmd.AddCommand(writCmd)

	writCmd.AddCommand(writCreateCmd)
	writCmd.AddCommand(writStatusCmd)
	writCmd.AddCommand(writGetAliasCmd)
	writCmd.AddCommand(writListCmd)
	writCmd.AddCommand(writUpdateCmd)
	writCmd.AddCommand(writCloseCmd)
	writCmd.AddCommand(writQueryCmd)
	writCmd.AddCommand(writReadyCmd)
	writCmd.AddCommand(writActivateCmd)
	writCmd.AddCommand(writCleanCmd)
}

// --- sol writ create ---

var (
	createTitle       string
	createDescription string
	createPriority    int
	createLabels      []string
	createKind        string
	createMetadata    string
)

var writCreateCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a writ",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		sleeping, err := config.IsSleeping(world)
		if err != nil {
			return fmt.Errorf("failed to check sleep status for world %q: %w", world, err)
		}
		if sleeping {
			return fmt.Errorf("world %q is sleeping: writ creation blocked (wake it with 'sol world wake %s')", world, world)
		}

		if cmd.Flags().Changed("priority") {
			if createPriority < 1 || createPriority > 3 {
				return fmt.Errorf("invalid priority %d: must be between 1 and 3", createPriority)
			}
		}

		opts := store.CreateWritOpts{
			Title:       createTitle,
			Description: createDescription,
			CreatedBy:   config.Autarch,
			Priority:    createPriority,
			Labels:      createLabels,
			Kind:        createKind,
		}

		if createMetadata != "" {
			var meta map[string]any
			if err := json.Unmarshal([]byte(createMetadata), &meta); err != nil {
				return fmt.Errorf("invalid --metadata JSON: %w", err)
			}
			opts.Metadata = meta
		}

		s, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer s.Close()

		id, err := s.CreateWritWithOpts(opts)
		if err != nil {
			return fmt.Errorf("failed to create writ: %w", err)
		}
		fmt.Println(id)
		return nil
	},
}

func init() {
	writCreateCmd.Flags().String("world", "", "world name")
	writCreateCmd.Flags().StringVar(&createTitle, "title", "", "writ title")
	_ = writCreateCmd.MarkFlagRequired("title")
	writCreateCmd.Flags().StringVar(&createDescription, "description", "", "writ description")
	writCreateCmd.Flags().IntVar(&createPriority, "priority", 2, "priority (1=high, 2=normal, 3=low)")
	writCreateCmd.Flags().StringArrayVar(&createLabels, "label", nil, "label (can be repeated)")
	writCreateCmd.Flags().StringVar(&createKind, "kind", "", "writ kind (default: code)")
	writCreateCmd.Flags().StringVar(&createMetadata, "metadata", "", "metadata as JSON object")
}

// --- sol writ status ---

var writStatusJSON bool

var writStatusRunE = func(cmd *cobra.Command, args []string) error {
	if err := config.ValidateWritID(args[0]); err != nil {
		return err
	}
	worldFlag, _ := cmd.Flags().GetString("world")
	world, err := config.ResolveWorld(worldFlag)
	if err != nil {
		return err
	}
	if err := config.RequireWorld(world); err != nil {
		return err
	}
	s, err := store.OpenWorld(world)
	if err != nil {
		return fmt.Errorf("failed to open world store: %w", err)
	}
	defer s.Close()

	item, err := s.GetWrit(args[0])
	if err != nil {
		return fmt.Errorf("failed to get writ: %w", err)
	}

	if writStatusJSON {
		return printJSON(item)
	}
	printWrit(item)
	return nil
}

var writStatusCmd = &cobra.Command{
	Use:          "status <id>",
	Short:        "Show writ status",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE:         writStatusRunE,
}

// Hidden alias for backwards compatibility.
var writGetAliasCmd = &cobra.Command{
	Use:          "get <id>",
	Short:        "Show writ status",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	Hidden:       true,
	RunE:         writStatusRunE,
}

func init() {
	for _, cmd := range []*cobra.Command{writStatusCmd, writGetAliasCmd} {
		cmd.Flags().String("world", "", "world name")
		cmd.Flags().BoolVar(&writStatusJSON, "json", false, "output as JSON")
	}
}

// --- sol writ list ---

var (
	listStatus   string
	listLabel    string
	listAssignee string
	listJSON     bool
	listAll      bool
)

var writListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List writs",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		// --all and --status are mutually exclusive.
		if listAll && listStatus != "" {
			return fmt.Errorf("--all and --status are mutually exclusive")
		}

		// Validate --status value if provided.
		validStatuses := []string{"open", "tethered", "working", "resolve", "done", "closed"}
		if listStatus != "" {
			valid := false
			for _, s := range validStatuses {
				if listStatus == s {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("invalid status %q: valid values are %s", listStatus, strings.Join(validStatuses, ", "))
			}
		}

		s, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer s.Close()

		// Determine filter behavior:
		// - --status=X: filter to that single status
		// - --all: no status filter (show everything including closed)
		// - default: exclude closed writs
		defaultFilter := false
		filters := store.ListFilters{
			Assignee: listAssignee,
			Label:    listLabel,
		}
		if listStatus != "" {
			filters.Status = store.WritStatus(listStatus)
		} else if !listAll {
			defaultFilter = true
			filters.Statuses = []string{"open", "tethered", "working", "resolve", "done"}
		}

		items, err := s.ListWrits(filters)
		if err != nil {
			return fmt.Errorf("failed to list writs: %w", err)
		}

		// Look up caravan membership for all returned writs. Best-effort:
		// if the sphere store is unavailable we render EmptyMarker in the
		// caravan column and omit the field from --json.
		caravanMemberships := lookupCaravanMemberships(items)

		if listJSON {
			return printJSON(buildWritListJSON(items, caravanMemberships))
		}
		if len(items) == 0 {
			if defaultFilter {
				fmt.Println("No open writs found. Use --all to include closed writs.")
			} else {
				fmt.Println("No writs found.")
			}
			return nil
		}
		now := time.Now()
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tSTATUS\tKIND\tPRIORITY\tTITLE\tASSIGNEE\tCARAVAN\tCREATED\tLABELS\n")
		for _, item := range items {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				item.ID,
				item.Status,
				orEmpty(item.Kind),
				priorityLabel(item.Priority),
				item.Title,
				orEmpty(item.Assignee),
				renderCaravanCell(caravanMemberships[item.ID]),
				cliformat.FormatTimestampOrRelative(item.CreatedAt, now),
				renderLabelsCell(item.Labels),
			)
		}
		tw.Flush()
		fmt.Println(cliformat.FormatCount(len(items), "writ", "writs"))
		return nil
	},
}

func init() {
	writListCmd.Flags().String("world", "", "world name (defaults to $SOL_WORLD or detected from current worktree)")
	writListCmd.Flags().StringVar(&listStatus, "status", "", "filter by status")
	writListCmd.Flags().BoolVar(&listAll, "all", false, "show all writs including closed")
	writListCmd.Flags().StringVar(&listLabel, "label", "", "filter by label")
	writListCmd.Flags().StringVar(&listAssignee, "assignee", "", "filter by assignee")
	writListCmd.Flags().BoolVar(&listJSON, "json", false, "output as JSON")
}

// --- sol writ update ---

var (
	updateStatus      string
	updateAssignee    string
	updatePriority    int
	updateTitle       string
	updateDescription string
)

var writUpdateCmd = &cobra.Command{
	Use:          "update <id>",
	Short:        "Update a writ",
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
		if updateStatus != "" {
			if updateStatus == "closed" {
				return fmt.Errorf("cannot set status to %q via 'writ update': use 'sol writ close' instead", updateStatus)
			}
			validStatuses := []string{"open", "tethered", "working", "resolve", "done"}
			valid := false
			for _, s := range validStatuses {
				if updateStatus == s {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("invalid status %q: valid values are %s", updateStatus, strings.Join(validStatuses, ", "))
			}
		}
		if cmd.Flags().Changed("priority") {
			if updatePriority < 1 || updatePriority > 3 {
				return fmt.Errorf("invalid priority %d: must be between 1 and 3", updatePriority)
			}
		}
		updates := store.WritUpdates{
			Status:      store.WritStatus(updateStatus),
			Assignee:    updateAssignee,
			Priority:    updatePriority,
			Title:       updateTitle,
			Description: updateDescription,
		}
		s, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer s.Close()

		if err := s.UpdateWrit(args[0], updates); err != nil {
			return fmt.Errorf("failed to update writ: %w", err)
		}
		fmt.Printf("Updated %s\n", args[0])
		return nil
	},
}

func init() {
	writUpdateCmd.Flags().String("world", "", "world name")
	writUpdateCmd.Flags().StringVar(&updateStatus, "status", "", "new status")
	writUpdateCmd.Flags().StringVar(&updateAssignee, "assignee", "", "new assignee (- to clear)")
	writUpdateCmd.Flags().IntVar(&updatePriority, "priority", 0, "new priority")
	writUpdateCmd.Flags().StringVar(&updateTitle, "title", "", "new title")
	writUpdateCmd.Flags().StringVar(&updateDescription, "description", "", "new description")
}

// --- sol writ close ---

var (
	closeReason  string
	closeConfirm bool
)

var writCloseCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Close a writ",
	Long: `Close a writ permanently. Supersedes any failed merge requests linked to
the writ and auto-resolves linked escalations.

Use --reason to record why the writ was closed (e.g. completed, superseded,
cancelled). This is a terminal state — closed writs cannot be reopened.

Requires --confirm to proceed; without it, prints what would be closed and exits.`,
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
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer s.Close()

		if !closeConfirm {
			w, err := s.GetWrit(args[0])
			if err != nil {
				return fmt.Errorf("failed to get writ: %w", err)
			}
			fmt.Printf("This will permanently close writ %s:\n", args[0])
			fmt.Printf("  Title: %s\n", w.Title)
			// Show any failed MRs that would be superseded.
			failedMRs, mrErr := s.ListMergeRequestsByWrit(args[0], "failed")
			if mrErr == nil && len(failedMRs) > 0 {
				fmt.Printf("  Failed MRs to supersede: %d\n", len(failedMRs))
				for _, mr := range failedMRs {
					fmt.Printf("    - %s\n", mr.ID)
				}
			}
			fmt.Println()
			fmt.Println("Run with --confirm to proceed.")
			return &exitError{code: 1}
		}

		superseded, err := s.CloseWrit(args[0], closeReason)
		if err != nil {
			return fmt.Errorf("failed to close writ: %w", err)
		}

		// Auto-resolve linked escalations (best-effort).
		sphereStore, err := store.OpenSphere()
		if err == nil {
			defer sphereStore.Close()
			escalations, escErr := sphereStore.ListEscalationsBySourceRef("writ:" + args[0])
			if escErr != nil {
				fmt.Fprintf(os.Stderr, "warning: list escalations for writ %s: %v\n", args[0], escErr)
			}
			for _, esc := range escalations {
				_ = sphereStore.ResolveEscalation(esc.ID)
			}
			// Resolve escalations for MRs superseded by writ closure.
			for _, mrID := range superseded {
				escalations, escErr := sphereStore.ListEscalationsBySourceRef("mr:" + mrID)
				if escErr != nil {
					fmt.Fprintf(os.Stderr, "warning: list escalations for mr %s: %v\n", mrID, escErr)
				}
				for _, esc := range escalations {
					_ = sphereStore.ResolveEscalation(esc.ID)
				}
			}
		}

		if len(superseded) > 0 {
			fmt.Printf("Closed %s (superseded %d failed MR(s))\n", args[0], len(superseded))
		} else {
			fmt.Printf("Closed %s\n", args[0])
		}
		return nil
	},
}

func init() {
	writCloseCmd.Flags().String("world", "", "world name")
	writCloseCmd.Flags().StringVar(&closeReason, "reason", "", "close reason (e.g. completed, superseded, cancelled)")
	writCloseCmd.Flags().BoolVar(&closeConfirm, "confirm", false, "confirm the destructive operation")
}

// --- sol writ query ---

var (
	querySQL  string
	queryJSON bool
)

var writQueryCmd = &cobra.Command{
	Use:          "query",
	Short:        "Run a read-only SQL query",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		if querySQL == "" {
			return fmt.Errorf("--sql is required")
		}
		// Only allow SELECT queries.
		trimmed := strings.TrimSpace(strings.ToUpper(querySQL))
		if !strings.HasPrefix(trimmed, "SELECT") {
			return fmt.Errorf("only SELECT queries are allowed")
		}
		if strings.Contains(querySQL, ";") {
			return fmt.Errorf("multi-statement queries are not allowed")
		}

		s, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer s.Close()

		rows, err := s.DB().Query(querySQL)
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return fmt.Errorf("failed to get columns: %w", err)
		}

		if queryJSON {
			return printQueryJSON(rows, cols)
		}
		return printQueryTable(rows, cols)
	},
}

func init() {
	writQueryCmd.Flags().String("world", "", "world name")
	writQueryCmd.Flags().StringVar(&querySQL, "sql", "", "SQL SELECT query")
	writQueryCmd.Flags().BoolVar(&queryJSON, "json", false, "output as JSON")
}

// --- sol writ ready ---

var readyJSON bool

var writReadyCmd = &cobra.Command{
	Use:          "ready",
	Short:        "List writs ready for dispatch",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		s, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer s.Close()

		items, err := s.ReadyWrits()
		if err != nil {
			return fmt.Errorf("failed to list ready writs: %w", err)
		}

		// Apply caravan-level filtering (deps + phase gating) using
		// the sphere store. If the sphere store is unavailable, skip
		// caravan checks and return dependency-ready writs only.
		sphereStore, sphereErr := store.OpenSphere()
		if sphereErr == nil {
			defer sphereStore.Close()

			var filtered []store.Writ
			for _, item := range items {
				blocked, err := sphereStore.IsWritBlockedByCaravan(
					item.ID, world, store.OpenWorld)
				if err != nil {
					// On error, include the writ (conservative: prefer
					// showing it over silently hiding it).
					filtered = append(filtered, item)
					continue
				}
				if !blocked {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}

		if readyJSON {
			return printJSON(items)
		}
		if len(items) == 0 {
			fmt.Println("No ready writs found.")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tSTATUS\tPRI\tTITLE\tASSIGNEE\tLABELS\n")
		for _, item := range items {
			assignee := item.Assignee
			if assignee == "" {
				assignee = "-"
			}
			labels := "-"
			if len(item.Labels) > 0 {
				labels = strings.Join(item.Labels, ", ")
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n", item.ID, item.Status, item.Priority, item.Title, assignee, labels)
		}
		tw.Flush()
		return nil
	},
}

func init() {
	writReadyCmd.Flags().String("world", "", "world name")
	writReadyCmd.Flags().BoolVar(&readyJSON, "json", false, "output as JSON")
}

// --- helpers ---

// priorityLabel renders a numeric priority as its human label. The numeric
// values are preserved in --json output; this helper is only for table views.
func priorityLabel(p int) string {
	switch p {
	case 1:
		return "high"
	case 2:
		return "normal"
	case 3:
		return "low"
	default:
		return fmt.Sprintf("%d", p)
	}
}

// renderLabelsCell joins labels with ", " or returns EmptyMarker.
func renderLabelsCell(labels []string) string {
	if len(labels) == 0 {
		return cliformat.EmptyMarker
	}
	return strings.Join(labels, ", ")
}

// caravanRef is the JSON shape for the caravan a writ belongs to.
type caravanRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// writMembership captures the caravan references (resolved to name where
// possible) a single writ belongs to, in the order ListCaravanItemsForWrit
// returns them.
type writMembership struct {
	Caravans []caravanRef
}

// lookupCaravanMemberships returns a map from writ ID to its caravan
// memberships. Best-effort: if the sphere store is unavailable (the common
// case in isolated tests) the map is empty and callers MUST treat a missing
// entry as "no caravans known" rather than "no caravans".
func lookupCaravanMemberships(writs []store.Writ) map[string]writMembership {
	result := make(map[string]writMembership, len(writs))
	if len(writs) == 0 {
		return result
	}
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return result
	}
	defer sphereStore.Close()

	// Cache caravan lookups so we don't query the same ID multiple times.
	// A cache entry with c == nil is a negative result (caravan row
	// missing or unreadable) — we still reuse it to avoid re-querying.
	caravanCache := make(map[string]*store.Caravan)
	resolve := func(id string) caravanRef {
		if c, ok := caravanCache[id]; ok {
			if c == nil {
				return caravanRef{ID: id}
			}
			return caravanRef{ID: c.ID, Name: c.Name}
		}
		c, err := sphereStore.GetCaravan(id)
		if err != nil || c == nil {
			caravanCache[id] = nil
			return caravanRef{ID: id}
		}
		caravanCache[id] = c
		return caravanRef{ID: c.ID, Name: c.Name}
	}

	for _, w := range writs {
		items, err := sphereStore.GetCaravanItemsForWrit(w.ID)
		if err != nil || len(items) == 0 {
			continue
		}
		m := writMembership{}
		seen := make(map[string]struct{}, len(items))
		for _, it := range items {
			if _, dup := seen[it.CaravanID]; dup {
				continue
			}
			seen[it.CaravanID] = struct{}{}
			m.Caravans = append(m.Caravans, resolve(it.CaravanID))
		}
		if len(m.Caravans) > 0 {
			result[w.ID] = m
		}
	}
	return result
}

// renderCaravanCell renders the caravan-membership cell for the writ list
// table. Shows the first caravan's name (or ID if unnamed) and, if the writ
// is in more than one caravan, a "+N" suffix for the remainder. An empty
// membership renders as EmptyMarker.
func renderCaravanCell(m writMembership) string {
	if len(m.Caravans) == 0 {
		return cliformat.EmptyMarker
	}
	first := m.Caravans[0]
	label := first.Name
	if label == "" {
		label = first.ID
	}
	if len(m.Caravans) > 1 {
		label = fmt.Sprintf("%s +%d", label, len(m.Caravans)-1)
	}
	return label
}

// writListJSON is the JSON shape emitted by `sol writ list --json`. Unlike
// the legacy default-marshal of store.Writ (which exposed PascalCase Go
// field names), this is an explicit snake_case surface so downstream tools
// can depend on stable field names and include the caravan join.
type writListJSON struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	Priority    int            `json:"priority"`
	Kind        string         `json:"kind"`
	Assignee    string         `json:"assignee,omitempty"`
	ParentID    string         `json:"parent_id,omitempty"`
	CreatedBy   string         `json:"created_by"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	ClosedAt    string         `json:"closed_at,omitempty"`
	CloseReason string         `json:"close_reason,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Caravan     *caravanRef    `json:"caravan"`
}

// buildWritListJSON projects store.Writ + caravan membership into the
// writListJSON shape. If a writ belongs to more than one caravan, the first
// is reported (matching table-view behaviour); the "+N" hint is table-only.
func buildWritListJSON(items []store.Writ, memberships map[string]writMembership) []writListJSON {
	out := make([]writListJSON, 0, len(items))
	for _, w := range items {
		row := writListJSON{
			ID:          w.ID,
			Title:       w.Title,
			Description: w.Description,
			Status:      w.Status,
			Priority:    w.Priority,
			Kind:        w.Kind,
			Assignee:    w.Assignee,
			ParentID:    w.ParentID,
			CreatedBy:   w.CreatedBy,
			CreatedAt:   cliformat.FormatTimestamp(w.CreatedAt),
			UpdatedAt:   cliformat.FormatTimestamp(w.UpdatedAt),
			CloseReason: w.CloseReason,
			Labels:      w.Labels,
			Metadata:    w.Metadata,
		}
		if w.ClosedAt != nil {
			row.ClosedAt = cliformat.FormatTimestamp(*w.ClosedAt)
		}
		if m, ok := memberships[w.ID]; ok && len(m.Caravans) > 0 {
			c := m.Caravans[0]
			row.Caravan = &c
		}
		out = append(out, row)
	}
	return out
}

func printWrit(w *store.Writ) {
	fmt.Printf("ID:          %s\n", w.ID)
	fmt.Printf("Title:       %s\n", w.Title)
	if w.Description != "" {
		fmt.Printf("Description: %s\n", w.Description)
	}
	fmt.Printf("Status:      %s\n", w.Status)
	fmt.Printf("Kind:        %s\n", w.Kind)
	fmt.Printf("Priority:    %d\n", w.Priority)
	if w.Assignee != "" {
		fmt.Printf("Assignee:    %s\n", w.Assignee)
	}
	if w.ParentID != "" {
		fmt.Printf("Parent:      %s\n", w.ParentID)
	}
	fmt.Printf("Created by:  %s\n", w.CreatedBy)
	fmt.Printf("Created at:  %s\n", cliformat.FormatTimestamp(w.CreatedAt))
	fmt.Printf("Updated at:  %s\n", cliformat.FormatTimestamp(w.UpdatedAt))
	if w.ClosedAt != nil {
		fmt.Printf("Closed at:   %s\n", cliformat.FormatTimestamp(*w.ClosedAt))
	}
	if w.CloseReason != "" {
		fmt.Printf("Close reason: %s\n", w.CloseReason)
	}
	if len(w.Labels) > 0 {
		fmt.Printf("Labels:      %s\n", strings.Join(w.Labels, ", "))
	}
	if w.Metadata != nil {
		b, err := json.MarshalIndent(w.Metadata, "             ", "  ")
		if err == nil {
			fmt.Printf("Metadata:    %s\n", string(b))
		}
	}
}

type scanRows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Err() error
}

func printQueryTable(rows scanRows, cols []string) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(cols, "\t"))

	values := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		strs := make([]string, len(cols))
		for i, v := range values {
			if v == nil {
				strs[i] = "NULL"
			} else {
				strs[i] = fmt.Sprintf("%v", v)
			}
		}
		fmt.Fprintln(tw, strings.Join(strs, "\t"))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}
	tw.Flush()
	return nil
}

func printQueryJSON(rows scanRows, cols []string) error {
	var results []map[string]interface{}
	values := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		row := make(map[string]interface{})
		for i, col := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = v
			}
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}
	return printJSON(results)
}
