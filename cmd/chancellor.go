package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/nevinsm/sol/internal/chancellor"
	"github.com/nevinsm/sol/internal/session"
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
		mgr := session.New()

		if err := chancellor.Stop(mgr); err != nil {
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
			func() error { return chancellor.Stop(mgr) },
			chancellorStartCmd, args)
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

var chancellorBriefCmd = &cobra.Command{
	Use:          "brief",
	Short:        "Display the chancellor's brief",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(chancellor.BriefPath())
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No brief found for chancellor")
				return nil
			}
			return fmt.Errorf("failed to read brief: %w", err)
		}

		fmt.Print(string(data))
		return nil
	},
}

// --- sol chancellor debrief ---

var chancellorDebriefCmd = &cobra.Command{
	Use:          "debrief",
	Short:        "Archive the chancellor's brief and reset",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		briefPath := chancellor.BriefPath()
		if _, err := os.Stat(briefPath); err != nil {
			if os.IsNotExist(err) {
				fmt.Println("No brief found for chancellor")
				return nil
			}
			return fmt.Errorf("failed to check brief: %w", err)
		}

		archiveFile, err := archiveBrief(chancellor.BriefDir(), briefPath)
		if err != nil {
			return err
		}

		fmt.Printf("Archived brief to .brief/archive/%s\n", archiveFile)
		fmt.Println("Chancellor ready for fresh engagement")
		return nil
	},
}

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
