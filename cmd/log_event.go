package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/spf13/cobra"
)

var (
	logEventType       string
	logEventActor      string
	logEventSource     string
	logEventVisibility string
	logEventPayload    string
)

var logEventCmd = &cobra.Command{
	Use:          "log-event",
	Short:        "Log a custom event to the event feed (plumbing)",
	GroupID:      groupPlumbing,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		var payload any
		if logEventPayload != "" {
			if err := json.Unmarshal([]byte(logEventPayload), &payload); err != nil {
				return fmt.Errorf("invalid --payload JSON: %w", err)
			}
		}

		logger := events.NewLogger(config.Home())
		logger.Emit(logEventType, logEventSource, logEventActor, logEventVisibility, payload)

		fmt.Printf("Logged: %s by %s\n", logEventType, logEventActor)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logEventCmd)
	logEventCmd.Flags().StringVar(&logEventType, "type", "", "event type (required)")
	logEventCmd.Flags().StringVar(&logEventActor, "actor", "", "who triggered the event (required)")
	logEventCmd.Flags().StringVar(&logEventSource, "source", "sol", "event source")
	logEventCmd.Flags().StringVar(&logEventVisibility, "visibility", "both", "event visibility (feed, audit, or both)")
	logEventCmd.Flags().StringVar(&logEventPayload, "payload", "{}", "JSON payload")
	logEventCmd.MarkFlagRequired("type")
	logEventCmd.MarkFlagRequired("actor")
}
