package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	inboxapi "github.com/nevinsm/sol/internal/cliapi/inbox"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/inbox"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var inboxJSON bool

var inboxCmd = &cobra.Command{
	Use:     "inbox",
	Short:   "Unified TUI for autarch escalations and mail",
	GroupID: groupCommunication,
	Long: `Launch a unified inbox TUI showing escalations and unread mail.

Presents a single priority-sorted view of everything needing the
autarch's attention. Navigate with arrow keys, expand with enter,
and take inline actions (ack, resolve, dismiss).

Use --json to dump the unified item list for scripting.`,
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE:          runInbox,
}

func runInbox(cmd *cobra.Command, args []string) error {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return err
	}
	defer sphereStore.Close()

	if inboxJSON {
		return runInboxJSON(sphereStore)
	}

	cfg := inbox.Config{
		Store:       sphereStore,
		EventLogger: events.NewLogger(config.Home()),
	}

	m := inbox.NewModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	_, err = p.Run()
	return err
}

func runInboxJSON(sphereStore *store.SphereStore) error {
	items, err := inbox.FetchItems(sphereStore)
	if err != nil {
		return fmt.Errorf("inbox: fetch error: %w", err)
	}

	out := inboxapi.FromInboxItems(items)

	if len(out) == 0 {
		fmt.Println("[]")
		return nil
	}

	return printJSON(out)
}

func init() {
	rootCmd.AddCommand(inboxCmd)
	inboxCmd.Flags().BoolVar(&inboxJSON, "json", false, "output as JSON")
}
