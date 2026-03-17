package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/config/defaults"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:     "config",
	Short:   "Manage sol configuration",
	GroupID: groupSetup,
}

var configClaudeCmd = &cobra.Command{
	Use:   "claude",
	Short: "Edit sphere-level Claude Code defaults",
	Long: `Launch an interactive Claude Code session for configuring sphere-level defaults.

The defaults directory ($SOL_HOME/.claude-defaults/) is the template for all
agent config directories. Changes made here propagate to all agents on their
next session start.

File ownership:
  settings.json        Sol-owned. Always overwritten from template. Do not edit.
  settings.local.json  User-owned. Your customizations go here.
  plugins/             Managed by /install and /uninstall. Shared sphere-wide.

Plugins installed here are available to all agents across all worlds.
After installing a plugin, verify its enabledPlugins entry exists in
settings.local.json (not just settings.json) to ensure it persists
across sol restarts.

Uses the sphere-level default account for authentication.`,
	SilenceUsage: true,
	RunE:         runConfigClaude,
}

func runConfigClaude(cmd *cobra.Command, args []string) error {
	// Seed defaults if they don't exist.
	defaultsDir := config.ClaudeDefaultsDir()

	if err := config.EnsureClaudeDefaults(); err != nil {
		return fmt.Errorf("failed to seed claude defaults: %w", err)
	}

	// Write CLAUDE.local.md persona so the operator understands file ownership.
	personaPath := filepath.Join(defaultsDir, "CLAUDE.local.md")
	if err := os.WriteFile(personaPath, defaults.ConfigSessionMD, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write config session persona: %v\n", err)
	}

	// Seed onboarding state to skip interactive prompts.
	if err := config.SeedOnboardingState(defaultsDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to seed onboarding state: %v\n", err)
	}

	// Find claude binary.
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	// Launch claude with CLAUDE_CONFIG_DIR set to .claude-defaults/ and
	// CWD also set to .claude-defaults/ so Claude Code discovers CLAUDE.local.md.
	claudeCmd := exec.Command(claudeBin, "--dangerously-skip-permissions")
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr
	claudeCmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+defaultsDir)
	claudeCmd.Dir = defaultsDir

	if err := claudeCmd.Run(); err != nil {
		// Exit errors from interactive processes are expected (user quit).
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &exitError{code: exitErr.ExitCode()}
		}
		return err
	}

	return nil
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configClaudeCmd)
}
