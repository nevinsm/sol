package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var statusJSON bool

var statusCmd = &cobra.Command{
	Use:     "status [world]",
	Short:   "Show sphere or world status",
	GroupID: groupDispatch,
	Long: `Show system status.

Without arguments, shows a sphere-level overview of all worlds and processes.
With a world name, shows detailed status for that specific world.

Exit codes (world --json only):
  0 = healthy
  1 = unhealthy
  2 = degraded`,
	Args:          cobra.MaximumNArgs(1),
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE:          runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runSphereStatus()
	}
	return runWorldStatus(args[0])
}

func runSphereStatus() error {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return err
	}
	defer sphereStore.Close()

	mgr := session.New()

	result := status.GatherSphere(sphereStore, sphereStore, mgr,
		gatedWorldOpener, sphereStore)

	if statusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Print(status.RenderSphere(result))
	return nil
}

func runWorldStatus(world string) error {
	if err := config.RequireWorld(world); err != nil {
		return err
	}

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

	result, err := status.Gather(world, sphereStore, worldStore, worldStore, mgr)
	if err != nil {
		return err
	}

	status.GatherCaravans(result, sphereStore, gatedWorldOpener)

	if statusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
		if code := result.Health(); code != 0 {
			return &exitError{code: code}
		}
		return nil
	}

	fmt.Print(status.RenderWorld(result))
	return nil
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "output as JSON")
}
