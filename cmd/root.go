package cmd

import (
	"os"

	"github.com/nevinsm/sol/internal/config"
	// Side-effect import: registers sol's built-in migrations via init()
	// in each migration file under internal/migrate/migrations. The
	// registry must be populated before any command runs so that
	// `sol migrate list`, `sol doctor`, and `sol up` see the full set.
	_ "github.com/nevinsm/sol/internal/migrate/migrations"
	"github.com/spf13/cobra"
)

var version = "dev"

// Command group IDs for sol help output.
const (
	groupDispatch      = "dispatch"
	groupWrits         = "writs"
	groupAgents        = "agents"
	groupProcesses     = "processes"
	groupCommunication = "communication"
	groupSetup         = "setup"
	groupPlumbing      = "plumbing"
)

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: groupDispatch, Title: "Dispatch:"},
		&cobra.Group{ID: groupWrits, Title: "Writs:"},
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
		// Bypass: some commands must work before SOL_HOME exists.
		//
		// Two patterns exist for bypassing this check:
		//   1. Name-based switch here (for commands that cannot set their own
		//      PersistentPreRunE because they share the root command group).
		//   2. Self-contained override: commands like "docs" and "skill" set
		//      their own PersistentPreRunE to no-op (see docs.go, skill.go).
		//
		// Prefer the self-contained override (pattern 2) for new commands.
		// The name-based switch below is fragile — any future subcommand
		// whose Name() collides with an entry here would silently skip
		// the world-required check.
		switch cmd.Name() {
		case "doctor", "init", "dangerous-command", "workflow-bypass":
			return nil
		}
		return config.EnsureDirs()
	},
}

func Execute() error {
	// Orchestration is non-interactive by definition: a git credential
	// prompt has no terminal to answer it. Without this, an HTTPS remote
	// with no stored credential lets git block on "Username for
	// 'https://...':" in the operator's terminal, and hangs forever inside
	// a headless daemon or a tmux agent session. Setting it process-wide
	// here (before any subcommand runs) means every child git process sol
	// spawns inherits it — directly, via daemons that re-exec sol, and via
	// exec.Command calls throughout internal/worldsync and internal/forge —
	// so a missing credential fails fast as an error instead of prompting.
	// Agent tmux sessions are a separate case: tmux does not inherit this
	// process's environment into new sessions, so internal/startup sets it
	// explicitly in the session env it builds.
	os.Setenv("GIT_TERMINAL_PROMPT", "0")
	return rootCmd.Execute()
}
