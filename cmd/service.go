package cmd

import (
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
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return service.Status()
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
