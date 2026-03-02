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

var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Work item store operations",
}

func init() {
	rootCmd.AddCommand(storeCmd)

	storeCmd.AddCommand(storeCreateCmd)
	storeCmd.AddCommand(storeGetCmd)
	storeCmd.AddCommand(storeListCmd)
	storeCmd.AddCommand(storeUpdateCmd)
	storeCmd.AddCommand(storeCloseCmd)
	storeCmd.AddCommand(storeQueryCmd)
}

// --- sol store create ---

var (
	createTitle       string
	createDescription string
	createPriority    int
	createLabels      []string
)

var storeCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a work item",
	RunE: func(cmd *cobra.Command, args []string) error {
		world, _ := cmd.Flags().GetString("world")
		if world == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(world); err != nil {
			return err
		}
		if createTitle == "" {
			return fmt.Errorf("--title is required")
		}
		s, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer s.Close()

		id, err := s.CreateWorkItem(createTitle, createDescription, "operator", createPriority, createLabels)
		if err != nil {
			return err
		}
		fmt.Println(id)
		return nil
	},
}

func init() {
	storeCreateCmd.Flags().String("world", "", "world name")
	storeCreateCmd.Flags().StringVar(&createTitle, "title", "", "work item title")
	storeCreateCmd.Flags().StringVar(&createDescription, "description", "", "work item description")
	storeCreateCmd.Flags().IntVar(&createPriority, "priority", 2, "priority (1=high, 2=normal, 3=low)")
	storeCreateCmd.Flags().StringArrayVar(&createLabels, "label", nil, "label (can be repeated)")
}

// --- sol store get ---

var getJSON bool

var storeGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a work item by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world, _ := cmd.Flags().GetString("world")
		if world == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(world); err != nil {
			return err
		}
		s, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer s.Close()

		item, err := s.GetWorkItem(args[0])
		if err != nil {
			return err
		}

		if getJSON {
			return printJSON(item)
		}
		printWorkItem(item)
		return nil
	},
}

func init() {
	storeGetCmd.Flags().String("world", "", "world name")
	storeGetCmd.Flags().BoolVar(&getJSON, "json", false, "output as JSON")
}

// --- sol store list ---

var (
	listStatus   string
	listLabel    string
	listAssignee string
	listJSON     bool
)

var storeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List work items",
	RunE: func(cmd *cobra.Command, args []string) error {
		world, _ := cmd.Flags().GetString("world")
		if world == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(world); err != nil {
			return err
		}
		s, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer s.Close()

		filters := store.ListFilters{
			Status:   listStatus,
			Assignee: listAssignee,
			Label:    listLabel,
		}
		items, err := s.ListWorkItems(filters)
		if err != nil {
			return err
		}

		if listJSON {
			return printJSON(items)
		}
		if len(items) == 0 {
			fmt.Println("No work items found.")
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
	storeListCmd.Flags().String("world", "", "world name")
	storeListCmd.Flags().StringVar(&listStatus, "status", "", "filter by status")
	storeListCmd.Flags().StringVar(&listLabel, "label", "", "filter by label")
	storeListCmd.Flags().StringVar(&listAssignee, "assignee", "", "filter by assignee")
	storeListCmd.Flags().BoolVar(&listJSON, "json", false, "output as JSON")
}

// --- sol store update ---

var (
	updateStatus   string
	updateAssignee string
	updatePriority int
)

var storeUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a work item",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world, _ := cmd.Flags().GetString("world")
		if world == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(world); err != nil {
			return err
		}
		updates := store.WorkItemUpdates{
			Status:   updateStatus,
			Assignee: updateAssignee,
			Priority: updatePriority,
		}
		s, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.UpdateWorkItem(args[0], updates); err != nil {
			return err
		}
		fmt.Printf("Updated %s\n", args[0])
		return nil
	},
}

func init() {
	storeUpdateCmd.Flags().String("world", "", "world name")
	storeUpdateCmd.Flags().StringVar(&updateStatus, "status", "", "new status")
	storeUpdateCmd.Flags().StringVar(&updateAssignee, "assignee", "", "new assignee (- to clear)")
	storeUpdateCmd.Flags().IntVar(&updatePriority, "priority", 0, "new priority")
}

// --- sol store close ---

var storeCloseCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Close a work item",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		world, _ := cmd.Flags().GetString("world")
		if world == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(world); err != nil {
			return err
		}
		s, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.CloseWorkItem(args[0]); err != nil {
			return err
		}
		fmt.Printf("Closed %s\n", args[0])
		return nil
	},
}

func init() {
	storeCloseCmd.Flags().String("world", "", "world name")
}

// --- sol store query ---

var (
	querySQL  string
	queryJSON bool
)

var storeQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Run a read-only SQL query",
	RunE: func(cmd *cobra.Command, args []string) error {
		world, _ := cmd.Flags().GetString("world")
		if world == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(world); err != nil {
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
			return err
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
	storeQueryCmd.Flags().String("world", "", "world name")
	storeQueryCmd.Flags().StringVar(&querySQL, "sql", "", "SQL SELECT query")
	storeQueryCmd.Flags().BoolVar(&queryJSON, "json", false, "output as JSON")
}

// --- helpers ---

func printJSON(v interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printWorkItem(w *store.WorkItem) {
	fmt.Printf("ID:          %s\n", w.ID)
	fmt.Printf("Title:       %s\n", w.Title)
	if w.Description != "" {
		fmt.Printf("Description: %s\n", w.Description)
	}
	fmt.Printf("Status:      %s\n", w.Status)
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
	if len(w.Labels) > 0 {
		fmt.Printf("Labels:      %s\n", strings.Join(w.Labels, ", "))
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
