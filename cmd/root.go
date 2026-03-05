package cmd

import (
	"github.com/nevinsm/sol/internal/config"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

// Command group IDs for sol help output.
const (
	groupDispatch      = "dispatch"
	groupWorkItems     = "work-items"
	groupAgents        = "agents"
	groupProcesses     = "processes"
	groupCommunication = "communication"
	groupSetup         = "setup"
	groupPlumbing      = "plumbing"
)

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: groupDispatch, Title: "Dispatch:"},
		&cobra.Group{ID: groupWorkItems, Title: "Work Items:"},
		&cobra.Group{ID: groupAgents, Title: "Agents & Sessions:"},
		&cobra.Group{ID: groupProcesses, Title: "Processes:"},
		&cobra.Group{ID: groupCommunication, Title: "Communication:"},
		&cobra.Group{ID: groupSetup, Title: "Setup & Diagnostics:"},
		&cobra.Group{ID: groupPlumbing, Title: "Plumbing:"},
	)
}

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
		// doctor, init, and guard subcommands must work before SOL_HOME exists.
		switch cmd.Name() {
		case "doctor", "init", "dangerous-command", "workflow-bypass":
			return nil
		}
		return config.EnsureDirs()
	},
}

func Execute() error {
	return rootCmd.Execute()
}
