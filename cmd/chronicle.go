package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/session"
	"github.com/spf13/cobra"
)

const chronicleSessionName = "sol-chronicle"

var chronicleStatusJSON bool

var chronicleCmd = &cobra.Command{
	Use:     "chronicle",
	Short:   "Manage the event feed chronicle",
	GroupID: groupProcesses,
}

var chronicleRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the chronicle (foreground)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := events.DefaultChronicleConfig(config.Home())
		logger := events.NewLogger(config.Home())
		chronicle := events.NewChronicle(cfg, events.WithLogger(logger))

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Chronicle started (raw: .events.jsonl -> feed: .feed.jsonl)\n")
		err := chronicle.Run(ctx)
		fmt.Fprintf(os.Stderr, "Chronicle stopped (offset: %d)\n", chronicle.Offset())
		return err
	},
}

var chronicleStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the chronicle as a background tmux session",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if mgr.Exists(chronicleSessionName) {
			return fmt.Errorf("chronicle already running (session %s)", chronicleSessionName)
		}

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		env := map[string]string{
			"SOL_HOME": config.Home(),
		}

		if err := mgr.Start(chronicleSessionName, config.Home(),
			solBin+" chronicle run", env, "chronicle", ""); err != nil {
			return fmt.Errorf("failed to start chronicle session: %w", err)
		}

		fmt.Printf("Chronicle started: %s\n", chronicleSessionName)
		return nil
	},
}

var chronicleStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the chronicle background session",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if !mgr.Exists(chronicleSessionName) {
			return fmt.Errorf("no chronicle running (session %s not found)", chronicleSessionName)
		}

		if err := mgr.Stop(chronicleSessionName, false); err != nil {
			return fmt.Errorf("failed to stop chronicle: %w", err)
		}

		fmt.Printf("Chronicle stopped: %s\n", chronicleSessionName)
		return nil
	},
}

var chronicleRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the chronicle (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		return restartSession(mgr, chronicleSessionName, "chronicle",
			fmt.Sprintf("Chronicle stopped: %s", chronicleSessionName),
			nil, chronicleStartCmd, args)
	},
}

var chronicleStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show chronicle status",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		running := mgr.Exists(chronicleSessionName)

		// Try to read the checkpoint offset.
		var offset int64 = -1
		checkpointPath := filepath.Join(config.Home(), ".chronicle-checkpoint")
		if data, err := os.ReadFile(checkpointPath); err == nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
				offset = v
			}
		}

		if !running {
			if chronicleStatusJSON {
				out := map[string]any{
					"status": "stopped",
				}
				if offset >= 0 {
					out["checkpoint_offset"] = offset
				}
				data, _ := json.Marshal(out)
				fmt.Println(string(data))
				return nil
			}
			fmt.Println("Chronicle is not running.")
			if offset >= 0 {
				fmt.Printf("Last checkpoint offset: %d\n", offset)
			}
			return &exitError{code: 1}
		}

		if chronicleStatusJSON {
			out := map[string]any{
				"status":  "running",
				"session": chronicleSessionName,
			}
			if offset >= 0 {
				out["checkpoint_offset"] = offset
			}
			data, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("Chronicle: running\n")
		fmt.Printf("Session: %s\n", chronicleSessionName)
		if offset >= 0 {
			fmt.Printf("Checkpoint offset: %d\n", offset)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(chronicleCmd)
	chronicleCmd.AddCommand(chronicleRunCmd)
	chronicleCmd.AddCommand(chronicleStartCmd)
	chronicleCmd.AddCommand(chronicleStopCmd)
	chronicleCmd.AddCommand(chronicleRestartCmd)
	chronicleCmd.AddCommand(chronicleStatusCmd)

	chronicleStatusCmd.Flags().BoolVar(&chronicleStatusJSON, "json", false, "output as JSON")
}
