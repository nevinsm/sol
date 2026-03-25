package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/nevinsm/sol/internal/chancellor"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var chancellorCmd = &cobra.Command{
	Use:     "chancellor",
	Short:   "Manage the sphere-scoped planning session",
	GroupID: groupProcesses,
}

// --- sol chancellor start ---

var chancellorStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the chancellor planning session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Hold the agent lock to prevent concurrent start from prefect and user.
		agentLock, err := dispatch.AcquireAgentLock("chancellor")
		if err != nil {
			return fmt.Errorf("failed to start chancellor: %w", err)
		}
		defer agentLock.Release()

		mgr := session.New()

		if err := chancellor.Start(mgr); err != nil {
			return err
		}

		fmt.Println("Started chancellor session")
		fmt.Printf("  Session: %s\n", chancellor.SessionName)
		fmt.Printf("  Attach:  sol chancellor attach\n")
		return nil
	},
}

// --- sol chancellor stop ---

var chancellorStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the chancellor session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Hold the agent lock to prevent a concurrent Prefect respawn from racing
		// with the Exists→stop sequence inside chancellor.Stop (TOCTOU guard).
		agentLock, err := dispatch.AcquireAgentLock("chancellor")
		if err != nil {
			return fmt.Errorf("failed to stop chancellor: %w", err)
		}
		defer agentLock.Release()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()

		if err := chancellor.Stop(mgr, sphereStore); err != nil {
			return err
		}

		fmt.Println("Stopped chancellor session")
		return nil
	},
}

// --- sol chancellor restart ---

var chancellorRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the chancellor (stop then start)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		return restartSession(mgr, chancellor.SessionName, "chancellor",
			"Stopped chancellor session",
			func() error {
				sphereStore, err := store.OpenSphere()
				if err != nil {
					return err
				}
				defer sphereStore.Close()
				return chancellor.Stop(mgr, sphereStore)
			},
			nil, chancellorStartCmd, args)
	},
}

// --- sol chancellor attach ---

var chancellorAttachCmd = &cobra.Command{
	Use:          "attach",
	Short:        "Attach to the chancellor tmux session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if !mgr.Exists(chancellor.SessionName) {
			return fmt.Errorf("no chancellor session running (run 'sol chancellor start' first)")
		}

		return mgr.Attach(chancellor.SessionName)
	},
}

// --- sol chancellor brief ---

var chancellorBriefCmd = briefSubcommand(
	"brief", "Display the chancellor's brief", nil,
	func(_ []string) (string, string, error) {
		return chancellor.BriefPath(), "No brief found for chancellor", nil
	},
)

// --- sol chancellor debrief ---

var chancellorDebriefCmd = debriefSubcommand(
	"debrief", "Archive the chancellor's brief and reset", nil,
	func(_ []string) (string, string, string, string, error) {
		return chancellor.BriefPath(), chancellor.BriefDir(),
			"No brief found for chancellor", "Chancellor ready for fresh engagement", nil
	},
)

// --- sol chancellor status ---

var chancellorStatusJSON bool

type chancellorStatusSummary struct {
	Running     bool   `json:"running"`
	SessionName string `json:"session_name"`
	BriefAge    string `json:"brief_age,omitempty"`
}

var chancellorStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show chancellor status",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		running := mgr.Exists(chancellor.SessionName)

		summary := chancellorStatusSummary{
			Running:     running,
			SessionName: chancellor.SessionName,
		}

		// Check brief age.
		briefPath := chancellor.BriefPath()
		if info, err := os.Stat(briefPath); err == nil {
			summary.BriefAge = time.Since(info.ModTime()).Truncate(time.Second).String()
		}

		if chancellorStatusJSON {
			return printJSON(summary)
		}

		printChancellorStatus(summary)
		return nil
	},
}

func printChancellorStatus(s chancellorStatusSummary) {
	fmt.Printf("Chancellor\n\n")

	if s.Running {
		fmt.Printf("  Process:  running (%s)\n", s.SessionName)
	} else {
		fmt.Printf("  Process:  stopped\n")
	}

	if s.BriefAge != "" {
		fmt.Printf("  Brief:    %s old\n", s.BriefAge)
	}
}

func init() {
	rootCmd.AddCommand(chancellorCmd)
	chancellorCmd.AddCommand(chancellorStartCmd, chancellorStopCmd, chancellorRestartCmd,
		chancellorAttachCmd, chancellorBriefCmd, chancellorDebriefCmd, chancellorStatusCmd)

	chancellorStatusCmd.Flags().BoolVar(&chancellorStatusJSON, "json", false, "output as JSON")
}
