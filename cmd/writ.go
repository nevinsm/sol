package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

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
		if createTitle == "" {
			return fmt.Errorf("--title is required")
		}

		if config.IsSleeping(world) {
			return fmt.Errorf("world %q is sleeping: writ creation blocked (wake it with 'sol world wake %s')", world, world)
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
	world, _ := cmd.Flags().GetString("world")
	if world == "" {
		return fmt.Errorf("--world is required")
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
		s, err := store.OpenWorld(world)
		if err != nil {
			return fmt.Errorf("failed to open world store: %w", err)
		}
		defer s.Close()

		filters := store.ListFilters{
			Status:   store.WritStatus(listStatus),
			Assignee: listAssignee,
			Label:    listLabel,
		}
		items, err := s.ListWrits(filters)
		if err != nil {
			return fmt.Errorf("failed to list writs: %w", err)
		}

		if listJSON {
			return printJSON(items)
		}
		if len(items) == 0 {
			fmt.Println("No writs found.")
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
	writListCmd.Flags().String("world", "", "world name")
	writListCmd.Flags().StringVar(&listStatus, "status", "", "filter by status")
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

var closeReason string

var writCloseCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Close a writ",
	Long: `Close a writ permanently. Supersedes any failed merge requests linked to
the writ and auto-resolves linked escalations.

Use --reason to record why the writ was closed (e.g. completed, superseded,
cancelled). This is a terminal state — closed writs cannot be reopened.`,
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
	fmt.Printf("Created at:  %s\n", w.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated at:  %s\n", w.UpdatedAt.Format("2006-01-02 15:04:05"))
	if w.ClosedAt != nil {
		fmt.Printf("Closed at:   %s\n", w.ClosedAt.Format("2006-01-02 15:04:05"))
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
