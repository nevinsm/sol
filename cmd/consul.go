package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/escalation"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	consulInterval     string
	consulStaleTimeout string
	consulWebhook      string
	consulStatusJSON   bool
)

var consulCmd = &cobra.Command{
	Use:   "consul",
	Short: "Manage the sphere-level consul patrol process",
}

var consulRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the consul patrol loop (foreground)",
	RunE: func(cmd *cobra.Command, args []string) error {
		interval, err := time.ParseDuration(consulInterval)
		if err != nil {
			return fmt.Errorf("invalid --interval: %w", err)
		}
		staleTimeout, err := time.ParseDuration(consulStaleTimeout)
		if err != nil {
			return fmt.Errorf("invalid --stale-timeout: %w", err)
		}

		webhook := consulWebhook
		if webhook == "" {
			webhook = os.Getenv("GT_ESCALATION_WEBHOOK")
		}

		cfg := consul.Config{
			PatrolInterval:     interval,
			StaleTetherTimeout: staleTimeout,
			GTHome:             config.Home(),
			EscalationWebhook:  webhook,
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		eventLog := events.NewLogger(config.Home())
		mgr := session.New()
		router := escalation.DefaultRouter(eventLog, sphereStore, webhook)

		d := consul.New(cfg, sphereStore, mgr, router, eventLog)

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Consul starting (patrol every %s, stale timeout %s)\n",
			cfg.PatrolInterval, cfg.StaleTetherTimeout)
		return d.Run(ctx)
	},
}

var consulStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show consul status from heartbeat",
	RunE: func(cmd *cobra.Command, args []string) error {
		hb, err := consul.ReadHeartbeat(config.Home())
		if err != nil {
			return err
		}
		if hb == nil {
			fmt.Println("Consul is not running.")
			os.Exit(1)
		}

		if consulStatusJSON {
			stale := hb.IsStale(10 * time.Minute)
			out := map[string]any{
				"status":        hb.Status,
				"timestamp":     hb.Timestamp.Format(time.RFC3339),
				"patrol_count":  hb.PatrolCount,
				"stale_tethers": hb.StaleTethers,
				"caravan_feeds": hb.CaravanFeeds,
				"escalations":   hb.Escalations,
				"stale":         stale,
			}
			data, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		ago := time.Since(hb.Timestamp).Round(time.Second)
		fmt.Printf("Consul: %s\n", hb.Status)
		fmt.Printf("Last patrol: %s ago (patrol #%d)\n", ago, hb.PatrolCount)
		fmt.Printf("Stale tethers recovered: %d\n", hb.StaleTethers)
		fmt.Printf("Caravan feeds: %d\n", hb.CaravanFeeds)
		fmt.Printf("Open escalations: %d\n", hb.Escalations)

		if hb.IsStale(10 * time.Minute) {
			fmt.Println("\nWarning: heartbeat is stale — consul may not be running")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(consulCmd)
	consulCmd.AddCommand(consulRunCmd)
	consulCmd.AddCommand(consulStatusCmd)

	consulRunCmd.Flags().StringVar(&consulInterval, "interval", "5m", "patrol interval")
	consulRunCmd.Flags().StringVar(&consulStaleTimeout, "stale-timeout", "1h", "stale hook timeout")
	consulRunCmd.Flags().StringVar(&consulWebhook, "webhook", "", "escalation webhook URL")

	consulStatusCmd.Flags().BoolVar(&consulStatusJSON, "json", false, "output as JSON")
}
