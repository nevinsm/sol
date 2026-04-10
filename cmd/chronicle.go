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
	"time"

	"github.com/nevinsm/sol/internal/chronicle"
	clichronicle "github.com/nevinsm/sol/internal/cliapi/chronicle"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/daemon"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/spf13/cobra"
)

var chronicleStatusJSON bool

// chronicleLifecycle describes the chronicle daemon to the shared
// internal/daemon package.
var chronicleLifecycle = daemon.Lifecycle{
	Name:    "chronicle",
	PIDPath: func() string { return daemonPIDPath("chronicle") },
	RunArgs: []string{"chronicle", "run"},
	LogPath: func() string { return daemonLogPath("chronicle") },
}

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
		chron := events.NewChronicle(cfg, events.WithLogger(logger))

		// Flock-authoritative pidfile bootstrap.
		release, err := daemon.RunBootstrap(chronicleLifecycle)
		if err != nil {
			return fmt.Errorf("chronicle run: %w", err)
		}
		defer release()

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Chronicle started (raw: .events.jsonl -> feed: .feed.jsonl)\n")
		err = chron.Run(ctx)
		fmt.Fprintf(os.Stderr, "Chronicle stopped (offset: %d)\n", chron.Offset())

		// Clean up heartbeat on exit.
		os.Remove(chronicle.HeartbeatPath())
		return err
	},
}

var chronicleStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the chronicle as a background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
			return fmt.Errorf("failed to create runtime directory: %w", err)
		}
		lc := chronicleLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		res, err := daemon.Start(lc)
		if err != nil {
			return err
		}
		switch res.Status {
		case "running":
			fmt.Printf("Chronicle already running (pid %d)\n", res.PID)
		case "started":
			fmt.Printf("Chronicle started (pid %d)\n", res.PID)
		}
		return nil
	},
}

var chronicleStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the chronicle background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := daemon.Stop(chronicleLifecycle); err != nil {
			return err
		}
		fmt.Println("Chronicle stopped")
		return nil
	},
}

var chronicleRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the chronicle (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		lc := chronicleLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		if err := daemon.Restart(lc); err != nil {
			return err
		}
		fmt.Println("Chronicle restarted")
		return nil
	},
}

var chronicleStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show chronicle status",
	Long: `Show whether the chronicle process is running.

Prints PID, heartbeat metrics, and checkpoint offset. Use --json for machine-readable output.

Exit codes:
  0 - Chronicle is running
  1 - Chronicle is not running`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, _ := processutil.ReadPID(daemonPIDPath("chronicle"))
		running := pid > 0 && prefect.IsRunning(pid)

		// Read heartbeat for metrics.
		hb, _ := chronicle.ReadHeartbeat()

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
				resp := clichronicle.StatusResponse{
					Status: "stopped",
				}
				if offset >= 0 {
					resp.CheckpointOffset = &offset
				}
				data, _ := json.Marshal(resp)
				fmt.Println(string(data))
			} else {
				fmt.Println("Chronicle is not running.")
				if offset >= 0 {
					fmt.Printf("Last checkpoint offset: %d\n", offset)
				}
			}
			return &exitError{code: 1}
		}

		if chronicleStatusJSON {
			resp := clichronicle.StatusResponse{
				Status: "running",
				PID:    &pid,
			}
			if offset >= 0 {
				resp.CheckpointOffset = &offset
			}
			if hb != nil {
				resp.EventsProcessed = &hb.EventsProcessed
				age := time.Since(hb.Timestamp).Truncate(time.Second).String()
				resp.HeartbeatAge = age
			}
			data, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("Chronicle: running\n")
		fmt.Printf("PID: %d\n", pid)
		if hb != nil {
			fmt.Printf("Events processed: %d\n", hb.EventsProcessed)
			fmt.Printf("Heartbeat age: %s\n", time.Since(hb.Timestamp).Truncate(time.Second))
		}
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
