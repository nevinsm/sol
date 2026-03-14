package cmd

import (
	"fmt"
	"time"
	"unicode"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	escalationListStatus string
	escalationListJSON   bool
	escalationListAll    bool
)

var escalationCmd = &cobra.Command{
	Use:     "escalation",
	Short:   "Manage escalations",
	GroupID: groupCommunication,
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

		var escs []store.Escalation
		if escalationListStatus != "" {
			// Explicit status filter takes precedence.
			escs, err = sphereStore.ListEscalations(escalationListStatus)
		} else if escalationListAll {
			// --all: show everything including resolved.
			escs, err = sphereStore.ListEscalations("")
		} else {
			// Default: only open/acknowledged (exclude resolved).
			escs, err = sphereStore.ListOpenEscalations()
		}
		if err != nil {
			return err
		}

		if escalationListJSON {
			type jsonEsc struct {
				ID          string `json:"id"`
				Severity    string `json:"severity"`
				Status      string `json:"status"`
				Source      string `json:"source"`
				SourceRef   string `json:"source_ref"`
				Description string `json:"description"`
				CreatedAt   string `json:"created_at"`
			}
			out := make([]jsonEsc, len(escs))
			for i, e := range escs {
				out[i] = jsonEsc{
					ID:          e.ID,
					Severity:    e.Severity,
					Status:      e.Status,
					Source:      e.Source,
					SourceRef:   e.SourceRef,
					Description: e.Description,
					CreatedAt:   e.CreatedAt.Format(time.RFC3339),
				}
			}
			return printJSON(out)
		}

		// Human-readable output.
		statusLabel := "Open"
		if escalationListAll {
			statusLabel = "All"
		}
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
			ago := status.FormatDuration(time.Since(e.CreatedAt)) + " ago"
			sourceRef := e.SourceRef
			if sourceRef == "" {
				sourceRef = "—"
			}
			fmt.Printf("  %-18s [%-8s]  %-14s %-16s %-16s %s  (%s)\n",
				e.ID, e.Severity, e.Status, e.Source, sourceRef, e.Description, ago)
		}

		fmt.Printf("\n%d escalation(s)\n", len(escs))
		return nil
	},
}

// --- sol escalation ack ---

var escalationAckCmd = &cobra.Command{
	Use:          "ack <id>",
	Short:        "Acknowledge an escalation",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
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
		logger.Emit(events.EventEscalationAcked, "sol", config.Autarch, "both", map[string]string{
			"id": id,
		})

		fmt.Printf("Acknowledged: %s\n", id)
		return nil
	},
}

// --- sol escalation resolve ---

var escalationResolveCmd = &cobra.Command{
	Use:          "resolve <id>",
	Short:        "Resolve an escalation",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
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
		logger.Emit(events.EventEscalationResolved, "sol", config.Autarch, "both", map[string]string{
			"id": id,
		})

		fmt.Printf("Resolved: %s\n", id)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(escalationCmd)
	escalationCmd.AddCommand(escalationListCmd)
	escalationCmd.AddCommand(escalationAckCmd)
	escalationCmd.AddCommand(escalationResolveCmd)

	escalationListCmd.Flags().StringVar(&escalationListStatus, "status", "", "Filter by status (open, acknowledged, resolved)")
	escalationListCmd.Flags().BoolVar(&escalationListAll, "all", false, "Include resolved escalations")
	escalationListCmd.Flags().BoolVar(&escalationListJSON, "json", false, "Output as JSON array")
}
