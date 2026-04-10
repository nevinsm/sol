package cmd

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
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
	Use:   "list",
	Short: "List escalations",
	Long: `List escalations in a table.

By default, only open and acknowledged escalations are shown (resolved ones
are hidden). Use --all to include resolved escalations, or --status to filter
by a specific status.

Flags:
  --all            Include resolved escalations (shows open, acknowledged, resolved).
  --status STATUS  Show only escalations with the given status
                   (open, acknowledged, resolved). Takes precedence over --all.
  --json           Emit a JSON array with flat, structured fields.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		escs, err := loadEscalations(sphereStore, escalationListStatus, escalationListAll)
		if err != nil {
			return err
		}

		if escalationListJSON {
			return printJSON(escalationsToJSON(escs))
		}

		return renderEscalationTable(os.Stdout, escs, escalationListStatus, escalationListAll, time.Now())
	},
}

// loadEscalations fetches escalations from the store honouring the status
// filter and --all flag precedence.
func loadEscalations(s *store.SphereStore, statusFilter string, all bool) ([]store.Escalation, error) {
	switch {
	case statusFilter != "":
		// Explicit status filter takes precedence.
		return s.ListEscalations(statusFilter)
	case all:
		// --all: show everything including resolved.
		return s.ListEscalations("")
	default:
		// Default: only open/acknowledged (exclude resolved).
		return s.ListOpenEscalations()
	}
}

// escalationJSON is the flat, consumer-friendly JSON shape for --json output.
// All fields are strings; empty strings represent missing values so consumers
// can check presence without string parsing of a rendered cell.
type escalationJSON struct {
	ID             string `json:"id"`
	Severity       string `json:"severity"`
	Status         string `json:"status"`
	Source         string `json:"source"`
	SourceRef      string `json:"source_ref"`
	Description    string `json:"description"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	LastNotifiedAt string `json:"last_notified_at,omitempty"`
}

func escalationsToJSON(escs []store.Escalation) []escalationJSON {
	out := make([]escalationJSON, len(escs))
	for i, e := range escs {
		j := escalationJSON{
			ID:          e.ID,
			Severity:    e.Severity,
			Status:      e.Status,
			Source:      e.Source,
			SourceRef:   e.SourceRef,
			Description: e.Description,
			CreatedAt:   e.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   e.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if e.LastNotifiedAt != nil {
			j.LastNotifiedAt = e.LastNotifiedAt.UTC().Format(time.RFC3339)
		}
		out[i] = j
	}
	return out
}

// renderEscalationTable writes a header-row + tab-aligned table of escalations
// followed by a count footer. now is injected so tests can pin time.
func renderEscalationTable(w io.Writer, escs []store.Escalation, statusFilter string, all bool, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSEVERITY\tSTATUS\tSOURCE\tREFERENCE\tAGE\tMESSAGE")
	for _, e := range escs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			e.ID,
			e.Severity,
			e.Status,
			orEmpty(e.Source),
			orEmpty(e.SourceRef),
			cliformat.FormatTimestampOrRelative(e.CreatedAt, now),
			orEmpty(e.Description),
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, escalationFooter(len(escs), statusFilter, all))
	return nil
}

// orEmpty returns cliformat.EmptyMarker for empty strings, otherwise s.
func orEmpty(s string) string {
	if s == "" {
		return cliformat.EmptyMarker
	}
	return s
}

// escalationFooter renders the count line reflecting the active filter:
//
//	"3 open"       when default (no filter, no --all)
//	"3 resolved"   when --status=resolved
//	"3 escalations" when --all
func escalationFooter(n int, statusFilter string, all bool) string {
	switch {
	case statusFilter != "":
		return fmt.Sprintf("%d %s", n, statusFilter)
	case all:
		return cliformat.FormatCount(n, "escalation", "escalations")
	default:
		return fmt.Sprintf("%d open", n)
	}
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
