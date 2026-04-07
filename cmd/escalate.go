package cmd

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/escalation"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/spf13/cobra"
)

var (
	escalateSeverity  string
	escalateSource    string
	escalateSourceRef string
)

var escalateCmd = &cobra.Command{
	Use:   "escalate <description>",
	Short: "Create an escalation",
	Long: `Create an escalation record and route it for autarch attention.

Auto-detects source from SOL_WORLD/SOL_AGENT environment variables when
called from within an agent session. Also auto-detects the active writ
from the agent's tether to set --source-ref.

Severity defaults to "medium". Routing behavior (event log, webhook) depends
on the configured escalation router and SOL_ESCALATION_WEBHOOK.

Exit codes:
  0 - Escalation created (routing is best-effort and logged as a warning
      if it fails — the escalation still exists and last_notified_at is
      recorded so the aging loop does not spin)
  1 - Failed to create the escalation or to record last_notified_at`,
	GroupID:      groupCommunication,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		description := args[0]

		// Auto-detect source from SOL_WORLD/SOL_AGENT if --source not explicitly set.
		source := escalateSource
		if !cmd.Flags().Changed("source") {
			world := os.Getenv("SOL_WORLD")
			agent := os.Getenv("SOL_AGENT")
			if world != "" && agent != "" {
				source = world + "/" + agent
			}
		}

		// Determine source_ref: explicit flag > auto-detect from tether.
		sourceRef := escalateSourceRef
		if sourceRef == "" {
			world := os.Getenv("SOL_WORLD")
			agent := os.Getenv("SOL_AGENT")
			if world != "" && agent != "" {
				// Best-effort: read tether to get current writ ID.
				role := os.Getenv("SOL_ROLE")
				if role == "" {
					role = "outpost" // backward-compatible default
				}
				writID, err := tether.Read(world, agent, role)
				if err == nil && writID != "" {
					sourceRef = "writ:" + writID
				}
				// If tether read fails or is empty, proceed without source_ref.
			}
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		var createArgs []string
		if sourceRef != "" {
			createArgs = append(createArgs, sourceRef)
		}

		id, err := sphereStore.CreateEscalation(escalateSeverity, source, description, createArgs...)
		if err != nil {
			return err
		}

		esc, err := sphereStore.GetEscalation(id)
		if err != nil {
			return err
		}

		logger := events.NewLogger(config.Home())
		webhookURL := os.Getenv("SOL_ESCALATION_WEBHOOK")
		router := escalation.DefaultRouter(logger, sphereStore, webhookURL)

		// Record initial notification time BEFORE routing so the aging
		// loop sees an attempt even if the router blocks, panics, or
		// errors. CF-M24: previously this update happened after Route
		// and any failure was swallowed, leaving last_notified_at NULL
		// and causing the aging loop to retry indefinitely.
		if err := sphereStore.UpdateEscalationLastNotified(id); err != nil {
			return fmt.Errorf("failed to set last_notified_at for %s: %w", id, err)
		}

		if err := router.Route(cmd.Context(), *esc); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: notification error: %v\n", err)
		}

		fmt.Printf("Escalation created: %s [%s]\n", id, escalateSeverity)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(escalateCmd)
	escalateCmd.Flags().StringVar(&escalateSeverity, "severity", "medium", "Severity level (low, medium, high, critical)")
	escalateCmd.Flags().StringVar(&escalateSource, "source", config.Autarch, "Source of the escalation")
	escalateCmd.Flags().StringVar(&escalateSourceRef, "source-ref", "", "Structured reference (e.g., mr:mr-abc123, writ:sol-xyz)")
}
