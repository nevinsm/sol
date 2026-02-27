package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/escalation"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var (
	escalateSeverity string
	escalateSource   string
)

var escalateCmd = &cobra.Command{
	Use:   "escalate <description>",
	Short: "Create an escalation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		description := args[0]

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		id, err := townStore.CreateEscalation(escalateSeverity, escalateSource, description)
		if err != nil {
			return err
		}

		esc, err := townStore.GetEscalation(id)
		if err != nil {
			return err
		}

		logger := events.NewLogger(config.Home())
		webhookURL := os.Getenv("GT_ESCALATION_WEBHOOK")
		router := escalation.DefaultRouter(logger, townStore, webhookURL)

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
