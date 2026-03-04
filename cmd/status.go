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

var (
	statusJSON  bool
	statusWorld string
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sphere or world status",
	Long: `Show system status.

Without --world, shows a sphere-level overview of all worlds and processes.
With --world, shows detailed status for that specific world.

Exit codes (world mode only):
  0 = healthy
  1 = unhealthy
  2 = degraded`,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE:          runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	world := statusWorld
	if world == "" {
		world = os.Getenv("SOL_WORLD")
	}
	if world == "" {
		return runSphereStatus()
	}
	if err := config.RequireWorld(world); err != nil {
		return err
	}
	return runWorldStatus(world)
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

	// Exit with health code.
	if code := result.Health(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVar(&statusJSON, "json", false, "output as JSON")
	statusCmd.Flags().StringVar(&statusWorld, "world", "", "world name")
}
