package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/config"
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
	Long: `Launch Claude Code pointed at the sphere-level defaults directory.

The defaults directory ($SOL_HOME/.claude-defaults/) contains settings.json
and statusline.sh that are copied to all agent config dirs on session start.

Changes made in this session propagate to all agents on their next start.
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

	// Provision credentials from the sphere default account.
	resolvedAccount := account.ResolveAccount("", "")
	if resolvedAccount != "" {
		if err := config.ProvisionCredentials(defaultsDir, resolvedAccount); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to provision account %q credentials: %v\n", resolvedAccount, err)
		}
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

	// Launch claude with CLAUDE_CONFIG_DIR set to .claude-defaults/.
	claudeCmd := exec.Command(claudeBin, "--dangerously-skip-permissions")
	claudeCmd.Stdin = os.Stdin
	claudeCmd.Stdout = os.Stdout
	claudeCmd.Stderr = os.Stderr
	claudeCmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+defaultsDir)

	if err := claudeCmd.Run(); err != nil {
		// Exit errors from interactive processes are expected (user quit).
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}

	return nil
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configClaudeCmd)
}
