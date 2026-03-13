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
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/spf13/cobra"
)

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
		chron := events.NewChronicle(cfg, events.WithLogger(logger))

		// Write PID file.
		if err := prefect.WriteDaemonPID("chronicle", os.Getpid()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write PID file: %v\n", err)
		}

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Chronicle started (raw: .events.jsonl -> feed: .feed.jsonl)\n")
		err := chron.Run(ctx)
		fmt.Fprintf(os.Stderr, "Chronicle stopped (offset: %d)\n", chron.Offset())

		// Clean up heartbeat and PID on exit.
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
		// Check if already running via PID.
		pid := prefect.ReadDaemonPID("chronicle")
		if pid > 0 && prefect.IsRunning(pid) {
			fmt.Printf("Chronicle already running (pid %d)\n", pid)
			return nil
		}

		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to find sol binary: %w", err)
		}

		logPath := filepath.Join(config.RuntimeDir(), "chronicle.log")
		newPid, err := processutil.StartDaemon(logPath, append(os.Environ(), "SOL_HOME="+config.Home()), solBin, "chronicle", "run")
		if err != nil {
			return fmt.Errorf("failed to start chronicle: %w", err)
		}

		// Write PID file.
		if err := prefect.WriteDaemonPID("chronicle", newPid); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write PID file: %v\n", err)
		}

		// Verify alive after a moment.
		time.Sleep(time.Second)
		if !prefect.IsRunning(newPid) {
			return fmt.Errorf("chronicle exited immediately (check %s)", logPath)
		}

		fmt.Printf("Chronicle started (pid %d)\n", newPid)
		return nil
	},
}

var chronicleStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the chronicle background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid := prefect.ReadDaemonPID("chronicle")
		if pid <= 0 || !prefect.IsRunning(pid) {
			return fmt.Errorf("no chronicle running (pid file not found or process dead)")
		}

		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to stop chronicle (pid %d): %w", pid, err)
		}

		// Wait for process to exit.
		for i := 0; i < 20; i++ {
			time.Sleep(500 * time.Millisecond)
			if !prefect.IsRunning(pid) {
				break
			}
		}

		if prefect.IsRunning(pid) {
			return fmt.Errorf("chronicle (pid %d) did not exit after SIGTERM", pid)
		}

		fmt.Printf("Chronicle stopped (pid %d)\n", pid)
		return nil
	},
}

var chronicleRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the chronicle (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Stop if running (ignore errors — may not be running).
		pid := prefect.ReadDaemonPID("chronicle")
		if pid > 0 && prefect.IsRunning(pid) {
			if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
				for i := 0; i < 20; i++ {
					time.Sleep(500 * time.Millisecond)
					if !prefect.IsRunning(pid) {
						break
					}
				}
			}
			fmt.Println("Chronicle stopped")
		}

		// Start.
		return chronicleStartCmd.RunE(cmd, args)
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
		pid := prefect.ReadDaemonPID("chronicle")
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
				"status": "running",
				"pid":    pid,
			}
			if offset >= 0 {
				out["checkpoint_offset"] = offset
			}
			if hb != nil {
				out["events_processed"] = hb.EventsProcessed
				out["heartbeat_age"] = time.Since(hb.Timestamp).Truncate(time.Second).String()
			}
			data, err := json.Marshal(out)
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
