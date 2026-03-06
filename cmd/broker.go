package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/broker"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/spf13/cobra"
)

var (
	brokerInterval      string
	brokerRefreshMargin string
	brokerStatusJSON    bool
)

var tokenBrokerCmd = &cobra.Command{
	Use:     "token-broker",
	Short:   "Manage the token broker for centralized OAuth refresh",
	GroupID: groupProcesses,
}

var tokenBrokerRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the token broker loop (foreground)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		interval, err := time.ParseDuration(brokerInterval)
		if err != nil {
			return fmt.Errorf("invalid --interval: %w", err)
		}
		margin, err := time.ParseDuration(brokerRefreshMargin)
		if err != nil {
			return fmt.Errorf("invalid --refresh-margin: %w", err)
		}

		cfg := broker.Config{
			PatrolInterval: interval,
			RefreshMargin:  margin,
		}

		eventLog := events.NewLogger(config.Home())
		b := broker.New(cfg, eventLog)

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		return b.Run(ctx)
	},
}

var tokenBrokerStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show token broker status from heartbeat",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		hb, err := broker.ReadHeartbeat()
		if err != nil {
			return err
		}
		if hb == nil {
			fmt.Println("Token broker is not running.")
			return &exitError{code: 1}
		}

		if brokerStatusJSON {
			out := map[string]any{
				"status":       hb.Status,
				"timestamp":    hb.Timestamp.Format(time.RFC3339),
				"patrol_count": hb.PatrolCount,
				"accounts":     hb.Accounts,
				"agent_dirs":   hb.AgentDirs,
				"refreshed":    hb.Refreshed,
				"errors":       hb.Errors,
				"stale":        hb.IsStale(10 * time.Minute),
			}
			if hb.LastRefresh != "" {
				out["last_refresh"] = hb.LastRefresh
			}
			data, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		ago := time.Since(hb.Timestamp).Round(time.Second)
		fmt.Printf("Token broker: %s\n", hb.Status)
		fmt.Printf("Last patrol: %s ago (patrol #%d)\n", ago, hb.PatrolCount)
		fmt.Printf("Accounts: %d\n", hb.Accounts)
		fmt.Printf("Agent dirs managed: %d\n", hb.AgentDirs)
		fmt.Printf("Tokens refreshed this patrol: %d\n", hb.Refreshed)
		if hb.Errors > 0 {
			fmt.Printf("Errors: %d\n", hb.Errors)
		}
		if hb.LastRefresh != "" {
			fmt.Printf("Last refresh: %s\n", hb.LastRefresh)
		}

		if hb.IsStale(10 * time.Minute) {
			fmt.Println("\nWarning: heartbeat is stale — token broker may not be running")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tokenBrokerCmd)
	tokenBrokerCmd.AddCommand(tokenBrokerRunCmd)
	tokenBrokerCmd.AddCommand(tokenBrokerStatusCmd)

	tokenBrokerRunCmd.Flags().StringVar(&brokerInterval, "interval", "5m", "patrol interval")
	tokenBrokerRunCmd.Flags().StringVar(&brokerRefreshMargin, "refresh-margin", "30m", "refresh tokens this long before expiry")

	tokenBrokerStatusCmd.Flags().BoolVar(&brokerStatusJSON, "json", false, "output as JSON")
}
