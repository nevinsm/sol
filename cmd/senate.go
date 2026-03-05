package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

		// Create archive directory.
		archiveDir := filepath.Join(senate.BriefDir(), "archive")
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			return fmt.Errorf("failed to create archive directory: %w", err)
		}

		// Generate archive filename with RFC3339 timestamp, colons replaced by dashes.
		ts := time.Now().UTC().Format(time.RFC3339)
		safeTS := strings.ReplaceAll(ts, ":", "-")
		archiveFile := safeTS + ".md"
		archivePath := filepath.Join(archiveDir, archiveFile)

		// Move current brief to archive.
		if err := os.Rename(briefPath, archivePath); err != nil {
			return fmt.Errorf("failed to archive brief: %w", err)
		}

		fmt.Printf("Archived brief to .brief/archive/%s\n", archiveFile)
		fmt.Println("Senate ready for fresh engagement")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(senateCmd)
	senateCmd.AddCommand(senateStartCmd, senateStopCmd, senateAttachCmd,
		senateBriefCmd, senateDebriefCmd)
}
