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

Without arguments, shows a sphere-level overview of all processes and worlds.
With a world name, shows detailed status for that world.

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
	}

	// Determine view mode from args.
	if len(args) == 1 {
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
