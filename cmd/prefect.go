package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	cliprefect "github.com/nevinsm/sol/internal/cliapi/prefect"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/daemon"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var prefectStatusJSON bool

// prefectPIDPath returns the path to the prefect PID file.
func prefectPIDPath() string {
	return filepath.Join(config.RuntimeDir(), "prefect.pid")
}

// prefectLogPath returns the path to the prefect log file.
func prefectLogPath() string {
	return filepath.Join(config.RuntimeDir(), "prefect.log")
}

// prefectLifecycle describes the prefect daemon to the shared internal/daemon
// package. Note: prefect.Run() still writes its own PID via prefect.WritePID()
// internally — daemon.Start waits briefly and then reads the pidfile written
// by the child, same three-state classification as any other daemon.
var prefectLifecycle = daemon.Lifecycle{
	Name:    "prefect",
	PIDPath: prefectPIDPath,
	RunArgs: []string{"prefect", "run"},
	LogPath: prefectLogPath,
}

var prefectCmd = &cobra.Command{
	Use:     "prefect",
	Short:   "Manage the sol prefect",
	GroupID: groupProcesses,
}

var prefectRunCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run the prefect (foreground)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logPath := prefectLogPath()
		logger, logFile, err := prefect.NewLogger(logPath)
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}
		if logFile != nil {
			defer logFile.Close()
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()
		cfg := prefect.DefaultConfig()

		consulEnabled, _ := cmd.Flags().GetBool("consul")
		sourceRepo, _ := cmd.Flags().GetString("source-repo")
		worlds, _ := cmd.Flags().GetStringSlice("worlds")
		if consulEnabled {
			cfg.ConsulEnabled = true
		}
		if sourceRepo != "" {
			cfg.ConsulSourceRepo = sourceRepo
		}
		if len(worlds) > 0 {
			cfg.Worlds = worlds
		}

		// Resolve sol binary path for starting world services (sentinel/forge).
		if solBin, err := os.Executable(); err == nil {
			cfg.SolBinary = solBin
		}

		eventLog := events.NewLogger(config.Home())
		sup := prefect.New(cfg, sphereStore, mgr, logger, eventLog)

		// Signal handling.
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Prefect started (pid %d)\n", os.Getpid())
		fmt.Fprintf(os.Stderr, "Log: %s\n", logPath)
		// prefect.Run owns its own WritePID/ClearPID lifecycle (the duplicate-
		// instance guard lives there today). Keeping it conservative for this
		// writ — see sol-06e76378be1408bf scope notes.
		return sup.Run(ctx)
	},
}

var prefectStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the prefect as a background process",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
			return fmt.Errorf("failed to create runtime directory: %w", err)
		}
		lc := prefectLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		res, err := daemon.Start(lc)
		if err != nil {
			return err
		}
		switch res.Status {
		case "running":
			fmt.Printf("Prefect already running (pid %d)\n", res.PID)
		case "started":
			fmt.Printf("Prefect started (pid %d)\n", res.PID)
		}
		return nil
	},
}

var prefectStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the running prefect",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := daemon.Stop(prefectLifecycle); err != nil {
			return err
		}
		fmt.Println("Prefect stopped")
		return nil
	},
}

var prefectRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the prefect (stop then start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		lc := prefectLifecycle
		lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		if err := daemon.Restart(lc); err != nil {
			return err
		}
		fmt.Println("Prefect restarted")
		return nil
	},
}

var prefectStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show prefect status",
	Long: `Show whether the prefect process is running.

Prints status, PID, and uptime. Use --json for machine-readable output.

Exit codes:
  0 - Prefect is running
  1 - Prefect is not running`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := prefect.ReadPID()
		if err != nil {
			return err
		}

		running := pid > 0 && prefect.IsRunning(pid)

		if !running {
			if prefectStatusJSON {
				data, _ := json.Marshal(cliprefect.StatusResponse{
					Status: "stopped",
				})
				fmt.Println(string(data))
			} else {
				fmt.Println("Prefect is not running.")
			}
			return &exitError{code: 1}
		}

		// Read PID file mtime as proxy for start time.
		var uptime time.Duration
		if info, err := os.Stat(prefectPIDPath()); err == nil {
			uptime = time.Since(info.ModTime()).Round(time.Second)
		}

		if prefectStatusJSON {
			resp := cliprefect.StatusResponse{
				Status: "running",
				PID:    pid,
			}
			if uptime > 0 {
				resp.UptimeSeconds = int(uptime.Seconds())
			}
			data, err := json.Marshal(resp)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("Prefect: running\n")
		fmt.Printf("PID: %d\n", pid)
		if uptime > 0 {
			fmt.Printf("Uptime: %s\n", uptime)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(prefectCmd)
	prefectCmd.AddCommand(prefectRunCmd)
	prefectCmd.AddCommand(prefectStartCmd)
	prefectCmd.AddCommand(prefectStopCmd)
	prefectCmd.AddCommand(prefectRestartCmd)
	prefectCmd.AddCommand(prefectStatusCmd)

	prefectRunCmd.Flags().Bool("consul", false, "Enable consul monitoring and auto-start")
	prefectRunCmd.Flags().String("source-repo", "", "Source repository path (for consul dispatch)")
	prefectRunCmd.Flags().StringSlice("worlds", nil, "Comma-separated list of worlds to supervise (default: all)")

	prefectStatusCmd.Flags().BoolVar(&prefectStatusJSON, "json", false, "output as JSON")
}
