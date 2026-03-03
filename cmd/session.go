package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage tmux sessions for agents",
}

func init() {
	rootCmd.AddCommand(sessionCmd)

	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionStopCmd)
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionHealthCmd)
	sessionCmd.AddCommand(sessionCaptureCmd)
	sessionCmd.AddCommand(sessionAttachCmd)
	sessionCmd.AddCommand(sessionInjectCmd)
}

// --- sol session start ---

var (
	startWorkdir string
	startCmd     string
	startEnv     []string
	startRole    string
	startWorld   string
)

var sessionStartCmd = &cobra.Command{
	Use:          "start <name>",
	Short:        "Start a tmux session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		if startWorld != "" {
			if err := config.RequireWorld(startWorld); err != nil {
				return err
			}
		}

		env, err := parseVarFlags(startEnv)
		if err != nil {
			return err
		}

		mgr := session.New()
		if err := mgr.Start(name, startWorkdir, startCmd, env, startRole, startWorld); err != nil {
			return err
		}
		fmt.Printf("Session %s started\n", name)
		return nil
	},
}

func init() {
	sessionStartCmd.Flags().StringVar(&startWorkdir, "workdir", ".", "working directory")
	sessionStartCmd.Flags().StringVar(&startCmd, "cmd", "", "command to run")
	sessionStartCmd.Flags().StringArrayVar(&startEnv, "env", nil, "environment variable KEY=VAL (can be repeated)")
	sessionStartCmd.Flags().StringVar(&startRole, "role", "agent", "session role")
	sessionStartCmd.Flags().StringVar(&startWorld, "world", "", "world name")
}

// --- sol session stop ---

var stopForce bool

var sessionStopCmd = &cobra.Command{
	Use:          "stop <name>",
	Short:        "Stop a tmux session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		if err := mgr.Stop(args[0], stopForce); err != nil {
			return err
		}
		fmt.Printf("Session %s stopped\n", args[0])
		return nil
	},
}

func init() {
	sessionStopCmd.Flags().BoolVar(&stopForce, "force", false, "force kill without graceful shutdown")
}

// --- sol session list ---

var sessionListJSON bool

var sessionListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List all sessions",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		sessions, err := mgr.List()
		if err != nil {
			return err
		}

		if sessionListJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(sessions)
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions found.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "NAME\tROLE\tWORLD\tALIVE\tSTARTED\n")
		for _, s := range sessions {
			alive := "no"
			if s.Alive {
				alive = "yes"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.Name, s.Role, s.World, alive, s.StartedAt.Format("2006-01-02 15:04:05"))
		}
		tw.Flush()
		return nil
	},
}

func init() {
	sessionListCmd.Flags().BoolVar(&sessionListJSON, "json", false, "output as JSON")
}

// --- sol session health ---

var healthMaxInactivity time.Duration

var sessionHealthCmd = &cobra.Command{
	Use:          "health <name>",
	Short:        "Check session health",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		status, err := mgr.Health(args[0], healthMaxInactivity)
		if err != nil {
			return err
		}
		fmt.Println(status)
		if code := status.ExitCode(); code != 0 {
			return &exitError{code: code}
		}
		return nil
	},
}

func init() {
	sessionHealthCmd.Flags().DurationVar(&healthMaxInactivity, "max-inactivity", 30*time.Minute, "max inactivity before reporting hung")
}

// --- sol session capture ---

var captureLines int

var sessionCaptureCmd = &cobra.Command{
	Use:          "capture <name>",
	Short:        "Capture pane output",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		output, err := mgr.Capture(args[0], captureLines)
		if err != nil {
			return err
		}
		fmt.Print(output)
		return nil
	},
}

func init() {
	sessionCaptureCmd.Flags().IntVar(&captureLines, "lines", 50, "number of lines to capture")
}

// --- sol session attach ---

var sessionAttachCmd = &cobra.Command{
	Use:          "attach <name>",
	Short:        "Attach to a tmux session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		return mgr.Attach(args[0])
	},
}

// --- sol session inject ---

var injectMessage string
var injectNoSubmit bool

var sessionInjectCmd = &cobra.Command{
	Use:          "inject <name>",
	Short:        "Inject text into a session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if injectMessage == "" {
			return fmt.Errorf("--message is required")
		}
		mgr := session.New()
		if err := mgr.Inject(args[0], injectMessage, !injectNoSubmit); err != nil {
			return err
		}
		fmt.Printf("Injected message into session %s\n", args[0])
		return nil
	},
}

func init() {
	sessionInjectCmd.Flags().StringVar(&injectMessage, "message", "", "text to inject")
	sessionInjectCmd.Flags().BoolVar(&injectNoSubmit, "no-submit", false, "stage text without pressing Enter")
}
