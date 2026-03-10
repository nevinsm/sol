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

Without arguments, auto-detects world from cwd (or SOL_WORLD).
If a world is detected, shows sphere processes plus world detail combined.
Otherwise, shows a sphere-level overview of all worlds and processes.
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
		// Auto-detect world from SOL_WORLD or cwd.
		if world := detectWorldForStatus(); world != "" {
			return runCombinedStatus(world)
		}
		return runSphereStatus()
	}
	return runWorldStatus(args[0])
}

// detectWorldForStatus checks SOL_WORLD env and cwd for world context.
// Returns empty string if no world detected (silent, no error).
func detectWorldForStatus() string {
	world, err := config.ResolveWorld("")
	if err != nil {
		return ""
	}
	return world
}

func runSphereStatus() error {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return err
	}
	defer sphereStore.Close()

	mgr := session.New()

	result := status.GatherSphere(sphereStore, sphereStore, mgr,
		gatedWorldOpener, sphereStore, sphereStore)

	// Add autarch mail count.
	if count, err := sphereStore.CountPending(config.Autarch); err == nil && count > 0 {
		result.MailCount = count
	}

	if statusJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Print(status.RenderSphere(result))
	return nil
}

func runCombinedStatus(world string) error {
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
	status.GatherTokens(result, worldStore)

	// Load world config for capacity (non-fatal).
	if worldCfg, err := config.LoadWorldConfig(world); err == nil {
		result.Capacity = worldCfg.Agents.Capacity
	}

	consulInfo := status.GatherConsulInfo()

	// Gather escalation summary (non-fatal).
	var escSummary *status.EscalationSummary
	if escs, err := sphereStore.ListOpenEscalations(); err == nil && len(escs) > 0 {
		escSummary = &status.EscalationSummary{
			Total:      len(escs),
			BySeverity: make(map[string]int),
		}
		for _, esc := range escs {
			escSummary.BySeverity[esc.Severity]++
		}
	}

	// Count autarch mail.
	var mailCount int
	if count, err := sphereStore.CountPending(config.Autarch); err == nil {
		mailCount = count
	}

	if statusJSON {
		combined := struct {
			Consul      status.ConsulInfo          `json:"consul"`
			Escalations *status.EscalationSummary   `json:"escalations,omitempty"`
			*status.WorldStatus
		}{
			Consul:      consulInfo,
			Escalations: escSummary,
			WorldStatus: result,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(combined); err != nil {
			return err
		}
		if code := result.Health(); code != 0 {
			return &exitError{code: code}
		}
		return nil
	}

	fmt.Print(status.RenderCombined(consulInfo, result, mailCount, escSummary))
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
	status.GatherTokens(result, worldStore)

	// Load world config for capacity (non-fatal).
	if worldCfg, err := config.LoadWorldConfig(world); err == nil {
		result.Capacity = worldCfg.Agents.Capacity
	}

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
