package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/service"
	"github.com/spf13/cobra"
)

var serviceCmd = &cobra.Command{
	Use:     "service",
	Short:   "Manage system service units for sol sphere daemons",
	GroupID: groupProcesses,
}

const serviceStatusLong = `Show status of sol sphere daemon units.

This command queries the platform service manager (systemd on Linux, launchd
on macOS) and prints per-component state. It is suitable for use in monitoring
and health-check scripts.

Exit codes:
  0   All sol sphere daemons are running.
  1   The status command itself failed (could not query the service manager,
      or another unexpected error).
  2   One or more daemons are degraded: stopped, failed, or unknown to the
      service manager. The command itself ran successfully.`

var serviceInstallCmd = &cobra.Command{
	Use:          "install",
	Short:        "Generate and install system service units (enable but don't start)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		solBin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to detect sol binary path: %w", err)
		}

		solHome := config.Home()

		if err := service.Install(solBin, solHome); err != nil {
			return err
		}

		if !service.LingerEnabled() {
			fmt.Fprintln(os.Stderr, "\nWarning: loginctl enable-linger is not set for your user.")
			fmt.Fprintln(os.Stderr, "Without linger, services will stop when you log out.")
			fmt.Fprintln(os.Stderr, "Run: loginctl enable-linger")
		}

		fmt.Fprintln(os.Stderr, "\nService units installed and enabled. Start with: sol service start")
		return nil
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:          "uninstall",
	Short:        "Stop, disable, and remove system service units",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return service.Uninstall()
	},
}

var serviceStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start all sol sphere daemon units",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return service.Start()
	},
}

var serviceStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop all sol sphere daemon units",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return service.Stop()
	},
}

var serviceRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart all sol sphere daemon units",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return service.Restart()
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show status of sol sphere daemon units",
	Long:         serviceStatusLong,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		err := service.Status()
		if err == nil {
			return nil
		}
		// Map "degraded" into exit code 2 so monitoring scripts can
		// distinguish stopped/failed daemons from a tool crash. Print
		// the underlying message before swapping in the exitError so
		// the user still sees a useful diagnostic.
		if errors.Is(err, service.ErrServiceDegraded) {
			fmt.Fprintln(os.Stderr, err)
			return &exitError{code: 2}
		}
		return err
	},
}

func init() {
	rootCmd.AddCommand(serviceCmd)
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStartCmd)
	serviceCmd.AddCommand(serviceStopCmd)
	serviceCmd.AddCommand(serviceRestartCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
}
