package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/setup"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func runInit(cmd *cobra.Command, args []string) error {
	// Flag mode: --name provided → run directly.
	if initName != "" {
		return runFlagInit()
	}

	// Guided mode: --guided flag → Claude session (prompt 06).
	// (placeholder — will be added in prompt 06)

	// Interactive mode: stdin is a TTY → prompt for input.
	if isTerminal() {
		return runInteractiveInit()
	}

	// Non-interactive, no flags → error.
	return fmt.Errorf("--name flag is required when stdin is not a terminal\n" +
		"Usage: sol init --name=<world> [--source-repo=<path>]")
}

func runFlagInit() error {
	// Validate source repo if provided.
	if initSourceRepo != "" {
		info, err := os.Stat(initSourceRepo)
		if err != nil {
			return fmt.Errorf("source repo path %q: %w", initSourceRepo, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("source repo path %q is not a directory", initSourceRepo)
		}
	}

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

func runInteractiveInit() error {
	var (
		worldName  string
		sourceRepo string
		skipChecks bool
	)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("sol init").
				Description("Set up sol for first-time use.\n"+
					"This creates SOL_HOME and your first world."),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("World name").
				Description("Name for your first world (e.g., 'myproject')").
				Placeholder("myworld").
				Value(&worldName).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("world name is required")
					}
					return config.ValidateWorldName(s)
				}),

			huh.NewInput().
				Title("Source repository").
				Description("Path to your project's git repo (optional)").
				Placeholder("/path/to/repo").
				Value(&sourceRepo),

			huh.NewConfirm().
				Title("Skip prerequisite checks?").
				Description("Run 'sol doctor' checks before setup").
				Affirmative("Skip checks").
				Negative("Run checks").
				Value(&skipChecks),
		),
	)

	if err := form.Run(); err != nil {
		return fmt.Errorf("setup cancelled: %w", err)
	}

	// Validate source repo path if provided.
	if sourceRepo != "" {
		info, err := os.Stat(sourceRepo)
		if err != nil {
			return fmt.Errorf("source repo path %q: %w", sourceRepo, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("source repo path %q is not a directory", sourceRepo)
		}
	}

	params := setup.Params{
		WorldName:  worldName,
		SourceRepo: sourceRepo,
		SkipChecks: skipChecks,
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
