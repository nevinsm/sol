package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
	"github.com/nevinsm/gt/internal/witness"
	"github.com/spf13/cobra"
)

var witnessCmd = &cobra.Command{
	Use:   "witness",
	Short: "Manage the per-rig witness health monitor",
}

var witnessRunCmd = &cobra.Command{
	Use:   "run <rig>",
	Short: "Run the witness patrol loop (foreground)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		rigStore, err := store.OpenRig(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		sourceRepo, err := dispatch.DiscoverSourceRepo()
		if err != nil {
			return err
		}

		eventLog := events.NewLogger(config.Home())
		mgr := session.New()
		cfg := witness.DefaultConfig(rig, sourceRepo, config.Home())
		w := witness.New(cfg, townStore, rigStore, mgr, eventLog)

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Witness started for rig %q (patrol interval: %s)\n",
			rig, cfg.PatrolInterval)
		return w.Run(ctx)
	},
}

var witnessStartCmd = &cobra.Command{
	Use:   "start <rig>",
	Short: "Start the witness as a background tmux session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		sessName := dispatch.SessionName(rig, "witness")
		mgr := session.New()

		if mgr.Exists(sessName) {
			return fmt.Errorf("witness already running for rig %q (session %s)", rig, sessName)
		}

		gtBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find gt binary: %w", err)
		}

		env := map[string]string{
			"GT_HOME": config.Home(),
		}

		if err := mgr.Start(sessName, config.Home(),
			fmt.Sprintf("%s witness run %s", gtBin, rig), env, "witness", rig); err != nil {
			return fmt.Errorf("failed to start witness session: %w", err)
		}

		fmt.Printf("Witness started: %s\n", sessName)
		return nil
	},
}

var witnessStopCmd = &cobra.Command{
	Use:   "stop <rig>",
	Short: "Stop the witness",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		sessName := dispatch.SessionName(rig, "witness")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no witness running for rig %q", rig)
		}

		if err := mgr.Stop(sessName, false); err != nil {
			return fmt.Errorf("failed to stop witness: %w", err)
		}

		fmt.Printf("Witness stopped for rig %q\n", rig)
		return nil
	},
}

var witnessAttachCmd = &cobra.Command{
	Use:   "attach <rig>",
	Short: "Attach to the witness tmux session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		sessName := dispatch.SessionName(rig, "witness")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no witness session for rig %q (run 'gt witness start %s' first)", rig, rig)
		}

		return mgr.Attach(sessName)
	},
}

func init() {
	rootCmd.AddCommand(witnessCmd)
	witnessCmd.AddCommand(witnessRunCmd)
	witnessCmd.AddCommand(witnessStartCmd)
	witnessCmd.AddCommand(witnessStopCmd)
	witnessCmd.AddCommand(witnessAttachCmd)
}
