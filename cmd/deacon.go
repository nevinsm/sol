package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/deacon"
	"github.com/nevinsm/gt/internal/escalation"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var (
	deaconInterval     string
	deaconStaleTimeout string
	deaconWebhook      string
	deaconStatusJSON   bool
)

var deaconCmd = &cobra.Command{
	Use:   "deacon",
	Short: "Manage the town-level deacon patrol process",
}

var deaconRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the deacon patrol loop (foreground)",
	RunE: func(cmd *cobra.Command, args []string) error {
		interval, err := time.ParseDuration(deaconInterval)
		if err != nil {
			return fmt.Errorf("invalid --interval: %w", err)
		}
		staleTimeout, err := time.ParseDuration(deaconStaleTimeout)
		if err != nil {
			return fmt.Errorf("invalid --stale-timeout: %w", err)
		}

		webhook := deaconWebhook
		if webhook == "" {
			webhook = os.Getenv("GT_ESCALATION_WEBHOOK")
		}

		cfg := deacon.Config{
			PatrolInterval:    interval,
			StaleHookTimeout:  staleTimeout,
			GTHome:            config.Home(),
			EscalationWebhook: webhook,
		}

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		eventLog := events.NewLogger(config.Home())
		mgr := session.New()
		router := escalation.DefaultRouter(eventLog, townStore, webhook)

		d := deacon.New(cfg, townStore, mgr, router, eventLog)

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Deacon starting (patrol every %s, stale timeout %s)\n",
			cfg.PatrolInterval, cfg.StaleHookTimeout)
		return d.Run(ctx)
	},
}

var deaconStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show deacon status from heartbeat",
	RunE: func(cmd *cobra.Command, args []string) error {
		hb, err := deacon.ReadHeartbeat(config.Home())
		if err != nil {
			return err
		}
		if hb == nil {
			fmt.Println("Deacon is not running.")
			os.Exit(1)
		}

		if deaconStatusJSON {
			stale := hb.IsStale(10 * time.Minute)
			out := map[string]any{
				"status":       hb.Status,
				"timestamp":    hb.Timestamp.Format(time.RFC3339),
				"patrol_count": hb.PatrolCount,
				"stale_hooks":  hb.StaleHooks,
				"convoy_feeds": hb.ConvoyFeeds,
				"escalations":  hb.Escalations,
				"stale":        stale,
			}
			data, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		ago := time.Since(hb.Timestamp).Round(time.Second)
		fmt.Printf("Deacon: %s\n", hb.Status)
		fmt.Printf("Last patrol: %s ago (patrol #%d)\n", ago, hb.PatrolCount)
		fmt.Printf("Stale hooks recovered: %d\n", hb.StaleHooks)
		fmt.Printf("Convoy feeds: %d\n", hb.ConvoyFeeds)
		fmt.Printf("Open escalations: %d\n", hb.Escalations)

		if hb.IsStale(10 * time.Minute) {
			fmt.Println("\nWarning: heartbeat is stale — deacon may not be running")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deaconCmd)
	deaconCmd.AddCommand(deaconRunCmd)
	deaconCmd.AddCommand(deaconStatusCmd)

	deaconRunCmd.Flags().StringVar(&deaconInterval, "interval", "5m", "patrol interval")
	deaconRunCmd.Flags().StringVar(&deaconStaleTimeout, "stale-timeout", "1h", "stale hook timeout")
	deaconRunCmd.Flags().StringVar(&deaconWebhook, "webhook", "", "escalation webhook URL")

	deaconStatusCmd.Flags().BoolVar(&deaconStatusJSON, "json", false, "output as JSON")
}
