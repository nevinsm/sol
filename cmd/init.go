package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/setup"
	"github.com/spf13/cobra"
)

var (
	initName       string
	initSourceRepo string
	initSkipChecks bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize sol for first-time use",
	Long: `Set up SOL_HOME directory structure and create your first world.

Three modes:
  Flag mode:        sol init --name=myworld [--source-repo=/path]
  Interactive mode: sol init (prompts for input when stdin is a TTY)
  Guided mode:      sol init --guided (Claude-powered setup conversation)

Runs prerequisite checks (sol doctor) by default. Use --skip-checks to bypass.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	// Flag-based mode: if --name is provided, run directly.
	if initName != "" {
		return runFlagInit()
	}

	// If --name is not provided, we need interactive or guided mode.
	// Those are implemented in prompts 05 and 06.
	// For now, return an error asking for --name.
	return fmt.Errorf("--name flag is required (interactive mode coming soon)")
}

func runFlagInit() error {
	params := setup.Params{
		WorldName:  initName,
		SourceRepo: initSourceRepo,
		SkipChecks: initSkipChecks,
	}

	result, err := setup.Run(params)
	if err != nil {
		return err
	}

	printInitSuccess(result)
	return nil
}

func printInitSuccess(result *setup.Result) {
	fmt.Printf("sol initialized successfully!\n\n")
	fmt.Printf("  SOL_HOME:  %s\n", result.SOLHome)
	fmt.Printf("  World:     %s\n", result.WorldName)
	fmt.Printf("  Config:    %s\n", result.ConfigPath)
	fmt.Printf("  Database:  %s\n", result.DBPath)

	sourceDisplay := result.SourceRepo
	if sourceDisplay == "" {
		sourceDisplay = "(none)"
	}
	fmt.Printf("  Source:    %s\n", sourceDisplay)

	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  sol store create --world=%s --title=\"First task\"\n", result.WorldName)
	fmt.Printf("  sol cast <work-item-id> %s\n", result.WorldName)
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&initName, "name", "", "world name (required in flag mode)")
	initCmd.Flags().StringVar(&initSourceRepo, "source-repo", "", "path to source git repository")
	initCmd.Flags().BoolVar(&initSkipChecks, "skip-checks", false, "skip prerequisite checks")
}
