package cmd

import (
	"github.com/nevinsm/sol/internal/config"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

var rootCmd = &cobra.Command{
	Use:           "sol",
	Short:         "Multi-agent orchestration system",
	Version:       version,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Don't create directories for help or version output.
		if cmd.RunE == nil && cmd.Run == nil {
			return nil
		}
		return config.EnsureDirs()
	},
}

func Execute() error {
	return rootCmd.Execute()
}
