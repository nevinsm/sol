package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/status"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var statusJSON bool

var statusCmd = &cobra.Command{
	Use:   "status <rig>",
	Short: "Show rig status",
	Args:  cobra.ExactArgs(1),
	// SilenceErrors and SilenceUsage so exit code reflects health, not cobra.
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		rig := args[0]

		townStore, err := store.OpenTown()
		if err != nil {
			return err
		}
		defer townStore.Close()

		rigStore, err := store.OpenRig(rig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		mgr := session.New()

		result, err := status.Gather(rig, townStore, rigStore, mgr)
		if err != nil {
			return err
		}

		if statusJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(result); err != nil {
				return err
			}
		} else {
			printStatus(result)
		}

		// Exit with health code.
		os.Exit(result.Health())
		return nil
	},
}

func printStatus(rs *status.RigStatus) {
	fmt.Printf("Rig: %s\n", rs.Rig)

	if rs.Supervisor.Running {
		fmt.Printf("Supervisor: running (pid %d)\n", rs.Supervisor.PID)
	} else {
		fmt.Println("Supervisor: not running")
	}

	fmt.Println()

	if len(rs.Agents) == 0 {
		fmt.Println("No agents registered.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "AGENT\tSTATE\tSESSION\tWORK\n")
	for _, a := range rs.Agents {
		sess := "-"
		if a.State == "working" || a.State == "stalled" {
			if a.SessionAlive {
				sess = "alive"
			} else {
				sess = "dead!"
			}
		}

		work := "-"
		if a.HookItem != "" {
			work = fmt.Sprintf("%s: %s", a.HookItem, a.WorkTitle)
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Name, a.State, sess, work)
	}
	tw.Flush()

	fmt.Println()

	// Summary line.
	parts := fmt.Sprintf("%d working, %d idle", rs.Summary.Working, rs.Summary.Idle)
	if rs.Summary.Stalled > 0 {
		parts += fmt.Sprintf(", %d stalled", rs.Summary.Stalled)
	}
	if rs.Summary.Dead > 0 {
		parts += fmt.Sprintf(", %d dead session", rs.Summary.Dead)
		if rs.Summary.Dead > 1 {
			parts += "s"
		}
	}
	fmt.Printf("Summary: %d agents (%s)\n", rs.Summary.Total, parts)
	fmt.Printf("Health: %s\n", rs.HealthString())
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "output as JSON")
}
