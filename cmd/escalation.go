package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unicode"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	escalationListStatus string
	escalationListJSON   bool
)

var escalationCmd = &cobra.Command{
	Use:   "escalation",
	Short: "Manage escalations",
}

// --- sol escalation list ---

var escalationListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List escalations",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		escs, err := sphereStore.ListEscalations(escalationListStatus)
		if err != nil {
			return err
		}

		if escalationListJSON {
			type jsonEsc struct {
				ID          string `json:"id"`
				Severity    string `json:"severity"`
				Source      string `json:"source"`
				Description string `json:"description"`
				Status      string `json:"status"`
				CreatedAt   string `json:"created_at"`
			}
			out := make([]jsonEsc, len(escs))
			for i, e := range escs {
				out[i] = jsonEsc{
					ID:          e.ID,
					Severity:    e.Severity,
					Source:      e.Source,
					Description: e.Description,
					Status:      e.Status,
					CreatedAt:   e.CreatedAt.Format(time.RFC3339),
				}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		// Human-readable output.
		statusLabel := "All"
		if escalationListStatus != "" {
			statusLabel = escalationListStatus
			// Capitalize first letter.
			if len(statusLabel) > 0 {
				r := []rune(statusLabel)
				r[0] = unicode.ToUpper(r[0])
				statusLabel = string(r)
			}
		}
		fmt.Printf("%s escalations:\n", statusLabel)

		for _, e := range escs {
			ago := timeAgo(time.Since(e.CreatedAt))
			fmt.Printf("  %-14s [%-8s]  %-16s %s  (%s)\n", e.ID, e.Severity, e.Source, e.Description, ago)
		}

		fmt.Printf("\n%d escalation(s)\n", len(escs))
		return nil
	},
}

// --- sol escalation ack ---

var escalationAckCmd = &cobra.Command{
	Use:   "ack <id>",
	Short: "Acknowledge an escalation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		if err := sphereStore.AckEscalation(id); err != nil {
			return err
		}

		// Emit event (best-effort).
		logger := events.NewLogger(config.Home())
		logger.Emit(events.EventEscalationAcked, "sol", "operator", "both", map[string]string{
			"id": id,
		})

		fmt.Printf("Acknowledged: %s\n", id)
		return nil
	},
}

// --- sol escalation resolve ---

var escalationResolveCmd = &cobra.Command{
	Use:   "resolve <id>",
	Short: "Resolve an escalation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		if err := sphereStore.ResolveEscalation(id); err != nil {
			return err
		}

		// Emit event (best-effort).
		logger := events.NewLogger(config.Home())
		logger.Emit(events.EventEscalationResolved, "sol", "operator", "both", map[string]string{
			"id": id,
		})

		fmt.Printf("Resolved: %s\n", id)
		return nil
	},
}

// timeAgo formats a duration as a human-readable relative time.
func timeAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func init() {
	rootCmd.AddCommand(escalationCmd)
	escalationCmd.AddCommand(escalationListCmd)
	escalationCmd.AddCommand(escalationAckCmd)
	escalationCmd.AddCommand(escalationResolveCmd)

	escalationListCmd.Flags().StringVar(&escalationListStatus, "status", "", "Filter by status (open, acknowledged, resolved)")
	escalationListCmd.Flags().BoolVar(&escalationListJSON, "json", false, "Output as JSON array")
}
