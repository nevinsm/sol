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
		return config.EnsureDirs()
	},
}

func Execute() error {
	return rootCmd.Execute()
}
