package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var sentinelCmd = &cobra.Command{
	Use:   "sentinel",
	Short: "Manage the per-world sentinel health monitor",
}

var sentinelRunCmd = &cobra.Command{
	Use:          "run <world>",
	Short:        "Run the sentinel patrol loop (foreground)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		// Config-first source repo discovery.
		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return err
		}
		sourceRepo, err := dispatch.ResolveSourceRepo(worldCfg)
		if err != nil {
			return err
		}

		eventLog := events.NewLogger(config.Home())
		mgr := session.New()
		cfg := sentinel.DefaultConfig(world, sourceRepo, config.Home())
		w := sentinel.New(cfg, sphereStore, worldStore, mgr, eventLog)

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Sentinel started for world %q (patrol interval: %s)\n",
			world, cfg.PatrolInterval)
		return w.Run(ctx)
	},
}

var sentinelStartCmd = &cobra.Command{
	Use:          "start <world>",
	Short:        "Start the sentinel as a background tmux session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		sessName := dispatch.SessionName(world, "sentinel")
		mgr := session.New()

		if mgr.Exists(sessName) {
			return fmt.Errorf("sentinel already running for world %q (session %s)", world, sessName)
		}

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		env := map[string]string{
			"SOL_HOME": config.Home(),
		}

		if err := mgr.Start(sessName, config.Home(),
			fmt.Sprintf("%s sentinel run %s", solBin, world), env, "sentinel", world); err != nil {
			return fmt.Errorf("failed to start sentinel session: %w", err)
		}

		fmt.Printf("Sentinel started: %s\n", sessName)
		return nil
	},
}

var sentinelStopCmd = &cobra.Command{
	Use:          "stop <world>",
	Short:        "Stop the sentinel",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		sessName := dispatch.SessionName(world, "sentinel")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no sentinel running for world %q", world)
		}

		if err := mgr.Stop(sessName, false); err != nil {
			return fmt.Errorf("failed to stop sentinel: %w", err)
		}

		fmt.Printf("Sentinel stopped for world %q\n", world)
		return nil
	},
}

var sentinelAttachCmd = &cobra.Command{
	Use:          "attach <world>",
	Short:        "Attach to the sentinel tmux session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		if err := config.RequireWorld(world); err != nil {
			return err
		}

		sessName := dispatch.SessionName(world, "sentinel")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no sentinel session for world %q (run 'sol sentinel start %s' first)", world, world)
		}

		return mgr.Attach(sessName)
	},
}

func init() {
	rootCmd.AddCommand(sentinelCmd)
	sentinelCmd.AddCommand(sentinelRunCmd)
	sentinelCmd.AddCommand(sentinelStartCmd)
	sentinelCmd.AddCommand(sentinelStopCmd)
	sentinelCmd.AddCommand(sentinelAttachCmd)
}
