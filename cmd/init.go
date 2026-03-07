package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/doctor"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/setup"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	initName       string
	initSourceRepo string
	initSkipChecks bool
	initGuided     bool
)

var initCmd = &cobra.Command{
	Use:     "init",
	Short:   "Initialize sol for first-time use",
	GroupID: groupSetup,
	Long: `Set up SOL_HOME directory structure and create your first world.

Three modes:
  Flag mode:        sol init --name=myworld [--source-repo=<url-or-path>]
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

	// Guided mode: --guided flag → Claude session.
	if initGuided {
		return runGuidedInit()
	}

	// Interactive mode: stdin is a TTY → prompt for input.
	if isTerminal() {
		return runInteractiveInit()
	}

	// Non-interactive, no flags → error.
	return fmt.Errorf("--name flag is required when stdin is not a terminal\n" +
		"Usage: sol init --name=<world> [--source-repo=<path>]")
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

func runInteractiveInit() error {
	var (
		worldName  string
		sourceRepo string
		skipChecks bool
		runChecks  bool
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
				Description("Git URL or local path to your project's repo (optional)").
				Placeholder("git@github.com:org/repo.git").
				Value(&sourceRepo),

			huh.NewConfirm().
				Title("Run prerequisite checks?").
				Description("Verify tmux, git, claude, and SQLite before setup").
				Affirmative("Run checks").
				Negative("Skip").
				Value(&runChecks),
		),
	)

	if err := form.Run(); err != nil {
		return fmt.Errorf("setup cancelled: %w", err)
	}

	skipChecks = !runChecks

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

func runGuidedInit() error {
	// DEGRADE: check prerequisites for guided mode.
	// Guided mode needs tmux (for the session) and claude (for the AI).
	tmuxCheck := doctor.CheckTmux()
	claudeCheck := doctor.CheckClaude()

	if !tmuxCheck.Passed || !claudeCheck.Passed {
		fmt.Println("Guided mode requires tmux and claude CLI.")
		if !tmuxCheck.Passed {
			fmt.Printf("  ✗ %s\n    → %s\n", tmuxCheck.Message, tmuxCheck.Fix)
		}
		if !claudeCheck.Passed {
			fmt.Printf("  ✗ %s\n    → %s\n", claudeCheck.Message, claudeCheck.Fix)
		}
		fmt.Println("\nFalling back to interactive mode...")
		fmt.Println()
		return runInteractiveInit()
	}

	// Determine sol binary path.
	solBin, err := os.Executable()
	if err != nil {
		solBin = "sol" // fallback
	}

	// Create a temporary directory for the guided session.
	tmpDir, err := os.MkdirTemp("", "sol-guided-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write CLAUDE.md into the temp directory.
	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	ctx := protocol.GuidedInitClaudeMDContext{
		SOLHome:   config.Home(),
		SolBinary: solBin,
	}
	content := protocol.GenerateGuidedInitClaudeMD(ctx)
	claudeMDPath := filepath.Join(claudeDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write CLAUDE.md: %w", err)
	}

	// Start an ephemeral tmux session with Claude.
	sessionName := "sol-guided-init"

	// Kill any existing guided init session.
	mgr := session.New()
	if mgr.Exists(sessionName) {
		mgr.Stop(sessionName, true)
	}

	// Start Claude in the temp directory.
	claudeCmd := fmt.Sprintf("cd %s && claude", tmpDir)
	if err := mgr.Start(sessionName, tmpDir, claudeCmd, nil, "guided-init", ""); err != nil {
		return fmt.Errorf("failed to start guided session: %w", err)
	}

	fmt.Println("Starting guided setup with Claude...")
	fmt.Printf("Session: %s\n", sessionName)
	fmt.Println()
	fmt.Println("The Claude session will guide you through setup.")
	fmt.Println("When finished, the session will end automatically.")
	fmt.Println()

	// Attach to the session using exec.Command (not mgr.Attach which uses
	// syscall.Exec and would prevent cleanup). This blocks until detach or exit.
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found in PATH: %w", err)
	}
	attach := exec.Command(tmuxPath, "attach-session", "-t", sessionName)
	attach.Stdin = os.Stdin
	attach.Stdout = os.Stdout
	attach.Stderr = os.Stderr
	_ = attach.Run() // ignore error — user may have detached

	// Clean up the session if it's still running.
	if mgr.Exists(sessionName) {
		mgr.Stop(sessionName, true)
	}

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
	fmt.Printf("  sol writ create --world=%s --title=\"First task\"\n", result.WorldName)
	fmt.Printf("  sol cast <writ-id> --world=%s\n", result.WorldName)
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&initName, "name", "", "world name (required in flag mode)")
	initCmd.Flags().StringVar(&initSourceRepo, "source-repo", "", "git URL or local path to source repository")
	initCmd.Flags().BoolVar(&initSkipChecks, "skip-checks", false, "skip prerequisite checks")
	initCmd.Flags().BoolVar(&initGuided, "guided", false, "Claude-powered guided setup")
}
