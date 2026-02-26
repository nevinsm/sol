package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/refinery"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var refineryCmd = &cobra.Command{
	Use:   "refinery",
	Short: "Manage the merge pipeline refinery",
}

var refineryRunCmd = &cobra.Command{
	Use:   "run <rig>",
	Short: "Run the refinery for a rig (foreground)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]

		if err := config.EnsureDirs(); err != nil {
			return fmt.Errorf("failed to ensure directories: %w", err)
		}

		logPath := filepath.Join(config.RuntimeDir(), fmt.Sprintf("refinery-%s.log", rig))
		logger, logFile, err := refinery.NewLogger(logPath)
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}
		if logFile != nil {
			defer logFile.Close()
		}

		rigStore, err := store.OpenRig(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		sourceRepo, err := dispatch.DiscoverSourceRepo()
		if err != nil {
			return err
		}

		cfg := refinery.DefaultConfig()

		// Load quality gates from config file.
		gatesPath := filepath.Join(config.RigDir(rig), "refinery", "quality-gates.txt")
		gates, err := refinery.LoadQualityGates(gatesPath, cfg.QualityGates)
		if err != nil {
			return fmt.Errorf("failed to load quality gates: %w", err)
		}
		cfg.QualityGates = gates

		ref := refinery.New(rig, sourceRepo, rigStore, townStore, cfg, logger)

		// Signal handling.
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		fmt.Fprintf(os.Stderr, "Refinery started for rig %q (pid %d)\n", rig, os.Getpid())
		fmt.Fprintf(os.Stderr, "Log: %s\n", logPath)
		return ref.Run(ctx)
	},
}

var refineryStartCmd = &cobra.Command{
	Use:   "start <rig>",
	Short: "Start the refinery in a tmux session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		sessName := dispatch.SessionName(rig, "refinery")
		mgr := session.New()

		// Check if already running.
		if mgr.Exists(sessName) {
			return fmt.Errorf("refinery already running for rig %q (session %s)", rig, sessName)
		}

		// Discover source repo for working directory.
		sourceRepo, err := dispatch.DiscoverSourceRepo()
		if err != nil {
			return err
		}

		// Start session running the refinery.
		err = mgr.Start(sessName, sourceRepo,
			fmt.Sprintf("gt refinery run %s", rig),
			map[string]string{
				"GT_HOME": config.Home(),
				"GT_RIG":  rig,
			},
			"refinery", rig)
		if err != nil {
			return fmt.Errorf("failed to start refinery session: %w", err)
		}

		fmt.Printf("Refinery started for rig %q\n", rig)
		fmt.Printf("  Session: %s\n", sessName)
		fmt.Printf("  Attach:  gt refinery attach %s\n", rig)
		return nil
	},
}

var refineryStopCmd = &cobra.Command{
	Use:   "stop <rig>",
	Short: "Stop the refinery",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		sessName := dispatch.SessionName(rig, "refinery")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no refinery running for rig %q", rig)
		}

		if err := mgr.Stop(sessName, false); err != nil {
			return fmt.Errorf("failed to stop refinery: %w", err)
		}

		fmt.Printf("Refinery stopped for rig %q\n", rig)
		return nil
	},
}

var refineryAttachCmd = &cobra.Command{
	Use:   "attach <rig>",
	Short: "Attach to the refinery tmux session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]
		sessName := dispatch.SessionName(rig, "refinery")
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no refinery session for rig %q (run 'gt refinery start %s' first)", rig, rig)
		}

		return mgr.Attach(sessName)
	},
}

var refineryQueueJSON bool

var refineryQueueCmd = &cobra.Command{
	Use:   "queue <rig>",
	Short: "Show the merge request queue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]

		rigStore, err := store.OpenRig(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		// List all merge requests (all phases).
		mrs, err := rigStore.ListMergeRequests("")
		if err != nil {
			return err
		}

		if refineryQueueJSON {
			return printJSON(mrs)
		}

		printQueue(rig, mrs)
		return nil
	},
}

func printQueue(rig string, mrs []store.MergeRequest) {
	if len(mrs) == 0 {
		fmt.Printf("Merge Queue: %s (empty)\n", rig)
		return
	}

	fmt.Printf("Merge Queue: %s (%d items)\n\n", rig, len(mrs))

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\tWORK ITEM\tBRANCH\tPHASE\tATTEMPTS\n")
	for _, mr := range mrs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n",
			mr.ID, mr.WorkItemID, mr.Branch, mr.Phase, mr.Attempts)
	}
	tw.Flush()

	// Summary counts.
	counts := map[string]int{}
	for _, mr := range mrs {
		counts[mr.Phase]++
	}
	fmt.Printf("\nSummary: %d ready, %d in progress, %d merged\n",
		counts["ready"], counts["claimed"], counts["merged"])
}

func init() {
	rootCmd.AddCommand(refineryCmd)
	refineryCmd.AddCommand(refineryRunCmd)
	refineryCmd.AddCommand(refineryStartCmd)
	refineryCmd.AddCommand(refineryStopCmd)
	refineryCmd.AddCommand(refineryQueueCmd)
	refineryCmd.AddCommand(refineryAttachCmd)
	refineryQueueCmd.Flags().BoolVar(&refineryQueueJSON, "json", false, "output as JSON")
}
