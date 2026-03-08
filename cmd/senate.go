package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/nevinsm/sol/internal/senate"
	"github.com/nevinsm/sol/internal/session"
	"github.com/spf13/cobra"
)

var senateCmd = &cobra.Command{
	Use:     "senate",
	Short:   "Manage the sphere-scoped planning session",
	GroupID: groupProcesses,
}

// --- sol senate start ---

var senateStartCmd = &cobra.Command{
	Use:          "start",
	Short:        "Start the senate planning session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if err := senate.Start(mgr); err != nil {
			return err
		}

		fmt.Println("Started senate session")
		return nil
	},
}

// --- sol senate stop ---

var senateStopCmd = &cobra.Command{
	Use:          "stop",
	Short:        "Stop the senate session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if err := senate.Stop(mgr); err != nil {
			return err
		}

		fmt.Println("Stopped senate session")
		return nil
	},
}

// --- sol senate restart ---

var senateRestartCmd = &cobra.Command{
	Use:          "restart",
	Short:        "Restart the senate (stop then start)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		return restartSession(mgr, senate.SessionName, "senate",
			"Stopped senate session",
			func() error { return senate.Stop(mgr) },
			senateStartCmd, args)
	},
}

// --- sol senate attach ---

var senateAttachCmd = &cobra.Command{
	Use:          "attach",
	Short:        "Attach to the senate tmux session",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()

		if !mgr.Exists(senate.SessionName) {
			return fmt.Errorf("no senate session running (run 'sol senate start' first)")
		}

		return mgr.Attach(senate.SessionName)
	},
}

// --- sol senate brief ---

var senateBriefCmd = &cobra.Command{
	Use:          "brief",
	Short:        "Display the senate's brief",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(senate.BriefPath())
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No brief found for senate")
				return nil
			}
			return fmt.Errorf("failed to read brief: %w", err)
		}

		fmt.Print(string(data))
		return nil
	},
}

// --- sol senate debrief ---

var senateDebriefCmd = &cobra.Command{
	Use:          "debrief",
	Short:        "Archive the senate's brief and reset",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		briefPath := senate.BriefPath()
		if _, err := os.Stat(briefPath); err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No brief found for senate")
				return nil
			}
			return fmt.Errorf("failed to check brief: %w", err)
		}

		archiveFile, err := archiveBrief(senate.BriefDir(), briefPath)
		if err != nil {
			return err
		}

		fmt.Printf("Archived brief to .brief/archive/%s\n", archiveFile)
		fmt.Println("Senate ready for fresh engagement")
		return nil
	},
}

// --- sol senate status ---

var senateStatusJSON bool

type senateStatusSummary struct {
	Running     bool   `json:"running"`
	SessionName string `json:"session_name"`
	BriefAge    string `json:"brief_age,omitempty"`
}

var senateStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show senate status",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.New()
		running := mgr.Exists(senate.SessionName)

		summary := senateStatusSummary{
			Running:     running,
			SessionName: senate.SessionName,
		}

		// Check brief age.
		briefPath := senate.BriefPath()
		if info, err := os.Stat(briefPath); err == nil {
			summary.BriefAge = time.Since(info.ModTime()).Truncate(time.Second).String()
		}

		if senateStatusJSON {
			return printJSON(summary)
		}

		printSenateStatus(summary)
		return nil
	},
}

func printSenateStatus(s senateStatusSummary) {
	fmt.Printf("Senate\n\n")

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
	rootCmd.AddCommand(senateCmd)
	senateCmd.AddCommand(senateStartCmd, senateStopCmd, senateRestartCmd, senateAttachCmd,
		senateBriefCmd, senateDebriefCmd, senateStatusCmd)

	senateStatusCmd.Flags().BoolVar(&senateStatusJSON, "json", false, "output as JSON")
}
