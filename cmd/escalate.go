package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/escalation"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	escalateSeverity string
	escalateSource   string
)

var escalateCmd = &cobra.Command{
	Use:          "escalate <description>",
	Short:        "Create an escalation",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		description := args[0]

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		id, err := sphereStore.CreateEscalation(escalateSeverity, escalateSource, description)
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

		if err := router.Route(context.Background(), *esc); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: notification error: %v\n", err)
		}

		fmt.Printf("Escalation created: %s [%s]\n", id, escalateSeverity)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(escalateCmd)
	escalateCmd.Flags().StringVar(&escalateSeverity, "severity", "medium", "Severity level (low, medium, high, critical)")
	escalateCmd.Flags().StringVar(&escalateSource, "source", "operator", "Source of the escalation")
}
