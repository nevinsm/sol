package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/session"
	"github.com/spf13/cobra"
)

const curatorSessionName = "gt-curator"

var curatorCmd = &cobra.Command{
	Use:   "curator",
	Short: "Manage the event feed curator",
}

var curatorRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the curator (foreground)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := events.DefaultCuratorConfig(config.Home())
		logger := events.NewLogger(config.Home())
		curator := events.NewCurator(cfg, events.WithLogger(logger))

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Curator started (raw: .events.jsonl → feed: .feed.jsonl)\n")
		err := curator.Run(ctx)
		fmt.Fprintf(os.Stderr, "Curator stopped (offset: %d)\n", curator.Offset())
		return err
	},
}

var curatorStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the curator as a background tmux session",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if mgr.Exists(curatorSessionName) {
			return fmt.Errorf("curator already running (session %s)", curatorSessionName)
		}

		gtBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find gt binary: %w", err)
		}

		env := map[string]string{
			"GT_HOME": config.Home(),
		}

		if err := mgr.Start(curatorSessionName, config.Home(),
			gtBin+" curator run", env, "curator", ""); err != nil {
			return fmt.Errorf("failed to start curator session: %w", err)
		}

		fmt.Printf("Curator started: %s\n", curatorSessionName)
		return nil
	},
}

var curatorStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the curator background session",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if !mgr.Exists(curatorSessionName) {
			return fmt.Errorf("no curator running (session %s not found)", curatorSessionName)
		}

		if err := mgr.Stop(curatorSessionName, false); err != nil {
			return fmt.Errorf("failed to stop curator: %w", err)
		}

		fmt.Printf("Curator stopped: %s\n", curatorSessionName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(curatorCmd)
	curatorCmd.AddCommand(curatorRunCmd)
	curatorCmd.AddCommand(curatorStartCmd)
	curatorCmd.AddCommand(curatorStopCmd)
}
