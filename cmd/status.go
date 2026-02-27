package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var statusJSON bool

var statusCmd = &cobra.Command{
	Use:   "status <world>",
	Short: "Show world status",
	Args:  cobra.ExactArgs(1),
	// SilenceErrors and SilenceUsage so exit code reflects health, not cobra.
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world := args[0]

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		mgr := session.New()

		// worldStore satisfies both WorldStore and MergeQueueStore.
		result, err := status.Gather(world, sphereStore, worldStore, worldStore, mgr)
		if err != nil {
			return err
		}

		// Gather caravan info (non-fatal if unavailable).
		status.GatherCaravans(result, sphereStore, store.OpenWorld)

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

func printStatus(rs *status.WorldStatus) {
	fmt.Printf("World: %s\n", rs.World)

	if rs.Prefect.Running {
		fmt.Printf("Prefect: running (pid %d)\n", rs.Prefect.PID)
	} else {
		fmt.Println("Prefect: not running")
	}

	if rs.Forge.Running {
		fmt.Printf("Forge: running (%s)\n", rs.Forge.SessionName)
	} else {
		fmt.Println("Forge: not running")
	}

	if rs.Chronicle.Running {
		fmt.Printf("Chronicle: running (%s)\n", rs.Chronicle.SessionName)
	} else {
		fmt.Println("Chronicle: not running")
	}

	if rs.Sentinel.Running {
		fmt.Printf("Sentinel: running (%s)\n", rs.Sentinel.SessionName)
	} else {
		fmt.Println("Sentinel: not running")
	}

	fmt.Println()

	if len(rs.Agents) == 0 {
		fmt.Println("No agents registered.")
	} else {
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
	}

	// Caravans.
	if len(rs.Caravans) > 0 {
		fmt.Println("Caravans:")
		for _, c := range rs.Caravans {
			blocked := c.TotalItems - c.DoneItems - c.ReadyItems
			fmt.Printf("  %s  %s  %d items (%d done, %d ready, %d blocked)\n",
				c.ID, c.Name, c.TotalItems, c.DoneItems, c.ReadyItems, blocked)
		}
		fmt.Println()
	}

	// Merge queue line.
	mq := rs.MergeQueue
	if mq.Total == 0 {
		fmt.Println("Merge Queue: empty")
	} else {
		fmt.Printf("Merge Queue: %d ready, %d in progress, %d failed\n",
			mq.Ready, mq.Claimed, mq.Failed)
	}

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
