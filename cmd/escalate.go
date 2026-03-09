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
	Long: `Create an escalation record and route it for operator attention.

Auto-detects source from SOL_WORLD/SOL_AGENT environment variables when
called from within an agent session. Also auto-detects the active writ
from the agent's tether to set --source-ref.

Severity defaults to "medium". Routing behavior (event log, webhook) depends
on the configured escalation router and SOL_ESCALATION_WEBHOOK.`,
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
				writID, err := tether.Read(world, agent, "outpost")
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

		if err := router.Route(cmd.Context(), *esc); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: notification error: %v\n", err)
		}

		// Record initial notification time for aging checks.
		if err := sphereStore.UpdateEscalationLastNotified(id); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to set last_notified_at: %v\n", err)
		}

		fmt.Printf("Escalation created: %s [%s]\n", id, escalateSeverity)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(escalateCmd)
	escalateCmd.Flags().StringVar(&escalateSeverity, "severity", "medium", "Severity level (low, medium, high, critical)")
	escalateCmd.Flags().StringVar(&escalateSource, "source", "operator", "Source of the escalation")
	escalateCmd.Flags().StringVar(&escalateSourceRef, "source-ref", "", "Structured reference (e.g., mr:mr-abc123, writ:sol-xyz)")
}
