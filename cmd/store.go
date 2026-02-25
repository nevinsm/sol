package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var storeCmd = &cobra.Command{
	Use:   "store",
	Short: "Work item store operations",
}

// Shared flags
var dbFlag string

func init() {
	rootCmd.AddCommand(storeCmd)

	storeCmd.AddCommand(storeCreateCmd)
	storeCmd.AddCommand(storeGetCmd)
	storeCmd.AddCommand(storeListCmd)
	storeCmd.AddCommand(storeUpdateCmd)
	storeCmd.AddCommand(storeCloseCmd)
	storeCmd.AddCommand(storeQueryCmd)
}

// --- gt store create ---

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
		db := cmd.Flag("db").Value.String()
		if db == "" {
			return fmt.Errorf("--db is required")
		}
		if createTitle == "" {
			return fmt.Errorf("--title is required")
		}
		s, err := store.OpenRig(db)
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
	storeCreateCmd.Flags().StringVar(&dbFlag, "db", "", "rig database name")
	storeCreateCmd.Flags().StringVar(&createTitle, "title", "", "work item title")
	storeCreateCmd.Flags().StringVar(&createDescription, "description", "", "work item description")
	storeCreateCmd.Flags().IntVar(&createPriority, "priority", 2, "priority (1=high, 2=normal, 3=low)")
	storeCreateCmd.Flags().StringArrayVar(&createLabels, "label", nil, "label (can be repeated)")
}

// --- gt store get ---

var getJSON bool

var storeGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a work item by ID",
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
	storeGetCmd.Flags().StringVar(&dbFlag, "db", "", "rig database name")
	storeGetCmd.Flags().BoolVar(&getJSON, "json", false, "output as JSON")
}

// --- gt store list ---

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
		db := cmd.Flag("db").Value.String()
		if db == "" {
			return fmt.Errorf("--db is required")
		}
		s, err := store.OpenRig(db)
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
	storeListCmd.Flags().StringVar(&dbFlag, "db", "", "rig database name")
	storeListCmd.Flags().StringVar(&listStatus, "status", "", "filter by status")
	storeListCmd.Flags().StringVar(&listLabel, "label", "", "filter by label")
	storeListCmd.Flags().StringVar(&listAssignee, "assignee", "", "filter by assignee")
	storeListCmd.Flags().BoolVar(&listJSON, "json", false, "output as JSON")
}

// --- gt store update ---

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
		db := cmd.Flag("db").Value.String()
		if db == "" {
			return fmt.Errorf("--db is required")
		}
		updates := store.WorkItemUpdates{
			Status:   updateStatus,
			Assignee: updateAssignee,
			Priority: updatePriority,
		}
		s, err := store.OpenRig(db)
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
	storeUpdateCmd.Flags().StringVar(&dbFlag, "db", "", "rig database name")
	storeUpdateCmd.Flags().StringVar(&updateStatus, "status", "", "new status")
	storeUpdateCmd.Flags().StringVar(&updateAssignee, "assignee", "", "new assignee (- to clear)")
	storeUpdateCmd.Flags().IntVar(&updatePriority, "priority", 0, "new priority")
}

// --- gt store close ---

var storeCloseCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Close a work item",
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

		if err := s.CloseWorkItem(args[0]); err != nil {
			return err
		}
		fmt.Printf("Closed %s\n", args[0])
		return nil
	},
}

func init() {
	storeCloseCmd.Flags().StringVar(&dbFlag, "db", "", "rig database name")
}

// --- gt store query ---

var (
	querySQL  string
	queryJSON bool
)

var storeQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Run a read-only SQL query",
	RunE: func(cmd *cobra.Command, args []string) error {
		db := cmd.Flag("db").Value.String()
		if db == "" {
			return fmt.Errorf("--db is required")
		}
		if querySQL == "" {
			return fmt.Errorf("--sql is required")
		}
		// Only allow SELECT queries.
		trimmed := strings.TrimSpace(strings.ToUpper(querySQL))
		if !strings.HasPrefix(trimmed, "SELECT") {
			return fmt.Errorf("only SELECT queries are allowed")
		}

		s, err := store.OpenRig(db)
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
	storeQueryCmd.Flags().StringVar(&dbFlag, "db", "", "rig database name")
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

func printQueryTable(rows interface{ Next() bool; Scan(dest ...interface{}) error }, cols []string) error {
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
	tw.Flush()
	return nil
}

func printQueryJSON(rows interface{ Next() bool; Scan(dest ...interface{}) error }, cols []string) error {
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
	return printJSON(results)
}
