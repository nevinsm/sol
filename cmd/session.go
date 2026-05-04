package cmd

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	clisessions "github.com/nevinsm/sol/internal/cliapi/sessions"
	"github.com/nevinsm/sol/internal/cliformat"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:     "session",
	Short:   "Manage tmux sessions for agents",
	GroupID: groupAgents,
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

		// Sessions are always world-scoped — resolve via the standard
		// precedence (flag > SOL_WORLD > cwd detection) and require a
		// non-empty result.
		resolved, err := config.ResolveWorld(startWorld)
		if err != nil {
			return err
		}
		startWorld = resolved

		env, err := parseVarFlags(startEnv)
		if err != nil {
			return err
		}

		mgr := session.New()
		if err := mgr.Start(name, startWorkdir, startCmd, env, startRole, startWorld); err != nil {
			return fmt.Errorf("failed to start session: %w", err)
		}
		fmt.Printf("Session %s started\n", name)
		return nil
	},
}

func init() {
	sessionStartCmd.Flags().StringVar(&startWorkdir, "workdir", ".", "working directory")
	sessionStartCmd.Flags().StringVar(&startCmd, "cmd", "", "command to run")
	sessionStartCmd.Flags().StringArrayVar(&startEnv, "env", nil, "environment variable KEY=VAL (can be repeated)")
	sessionStartCmd.Flags().StringVar(&startRole, "role", "outpost", "session role")
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
			return fmt.Errorf("failed to stop session: %w", err)
		}
		fmt.Printf("Session %s stopped\n", args[0])
		return nil
	},
}

func init() {
	sessionStopCmd.Flags().BoolVar(&stopForce, "force", false, "force kill without graceful shutdown")
}

// --- sol session list ---

var (
	sessionListJSON bool
	sessionListRole string
)

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sessions",
	Long: `List all tmux sessions registered with sol across the sphere.

This view is sphere-wide and mixes daemons (forge-merge, sentinel) with
agent sessions (outpost, envoy). Use --role to filter to a specific role.

Timestamps are rendered in canonical sol format: relative ("5m ago",
"3h ago") for recent times and full RFC3339 UTC for anything older
than 24 hours. Empty cells render as "-".`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		sessions, err := mgr.List()
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}

		filtered := filterSessionsByRole(sessions, sessionListRole)

		if sessionListJSON {
			return printJSON(clisessions.FromSessionInfoSlice(filtered))
		}

		renderSessionList(os.Stdout, filtered, time.Now())
		return nil
	},
}

func init() {
	sessionListCmd.Flags().BoolVar(&sessionListJSON, "json", false, "output as JSON")
	sessionListCmd.Flags().StringVar(&sessionListRole, "role", "", "filter by role (outpost, envoy, forge-merge, sentinel)")
}

// filterSessionsByRole returns the subset of sessions whose Role matches
// role. An empty role string returns the input unchanged.
func filterSessionsByRole(sessions []session.SessionInfo, role string) []session.SessionInfo {
	if role == "" {
		return sessions
	}
	out := make([]session.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if s.Role == role {
			out = append(out, s)
		}
	}
	return out
}

// renderSessionList writes the human-readable session list table to w.
// now is passed explicitly so tests can pin the timestamp comparison
// boundary used by cliformat.FormatTimestampOrRelative.
func renderSessionList(w io.Writer, sessions []session.SessionInfo, now time.Time) {
	if len(sessions) == 0 {
		fmt.Fprintln(w, "No sessions found.")
		return
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "NAME\tROLE\tWORLD\tALIVE\tSTARTED\n")
	for _, s := range sessions {
		alive := "no"
		if s.Alive {
			alive = "yes"
		}
		role := s.Role
		if role == "" {
			role = cliformat.EmptyMarker
		}
		world := s.World
		if world == "" {
			world = cliformat.EmptyMarker
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			s.Name,
			role,
			world,
			alive,
			cliformat.FormatTimestampOrRelative(s.StartedAt, now),
		)
	}
	tw.Flush()
	fmt.Fprintln(w, cliformat.FormatCount(len(sessions), "session", "sessions"))
}

// --- sol session health ---

var healthMaxInactivity time.Duration

var sessionHealthCmd = &cobra.Command{
	Use:   "health <name>",
	Short: "Check session health",
	Long: `Check session health and report status via exit code.

Exit codes:
  0  healthy    — session alive with recent activity
  1  dead       — tmux session does not exist
  2  degraded   — session exists but agent process exited or no output change within --max-inactivity`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		status, err := mgr.Health(args[0], healthMaxInactivity)
		if err != nil {
			return fmt.Errorf("failed to check session health: %w", err)
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
			return fmt.Errorf("failed to capture session output: %w", err)
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
		mgr := session.New()
		if err := mgr.Inject(args[0], injectMessage, !injectNoSubmit); err != nil {
			return fmt.Errorf("failed to inject message into session: %w", err)
		}
		fmt.Printf("Injected message into session %s\n", args[0])
		return nil
	},
}

func init() {
	sessionInjectCmd.Flags().StringVar(&injectMessage, "message", "", "text to inject")
	sessionInjectCmd.Flags().BoolVar(&injectNoSubmit, "no-submit", false, "stage text without pressing Enter")

	_ = sessionInjectCmd.MarkFlagRequired("message")
}
