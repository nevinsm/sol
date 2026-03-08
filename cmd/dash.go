package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dash"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var dashCmd = &cobra.Command{
	Use:   "dash [world]",
	Short: "Live TUI dashboard",
	Long: `Launch a live terminal dashboard.

Without arguments, auto-detects the current world from SOL_WORLD or the
working directory. Falls back to the sphere-level overview if no world
is detected. With a world name, shows detailed status for that world.

The dashboard refreshes every 3 seconds. Press r to force refresh.`,
	Args:          cobra.MaximumNArgs(1),
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE:          runDash,
}

func runDash(cmd *cobra.Command, args []string) error {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return err
	}
	defer sphereStore.Close()

	mgr := session.New()

	cfg := dash.Config{
		SphereStore:  sphereStore,
		WorldOpener:  gatedWorldOpener,
		SessionCheck: mgr,
		CaravanStore: sphereStore,
		SessionMgr:   mgr,
		SOLHome:      config.Home(),
	}

	// Determine view mode from args or auto-detection.
	if len(args) == 0 {
		if detected, err := config.ResolveWorld(""); err == nil {
			cfg.World = detected
		}
		// Silently fall back to sphere view if detection fails.
	} else {
		if err := config.RequireWorld(args[0]); err != nil {
			return err
		}
		cfg.World = args[0]
	}

	m := dash.NewModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	_, err = p.Run()
	return err
}

func init() {
	rootCmd.AddCommand(dashCmd)
}
