package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
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
		Store: sphereStore,
	}

	m := inbox.NewModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	_, err = p.Run()
	return err
}

func runInboxJSON(sphereStore *store.SphereStore) error {
	items := inbox.FetchItems(sphereStore)

	type jsonItem struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		Priority    int    `json:"priority"`
		Source      string `json:"source"`
		Description string `json:"description"`
		Age         string `json:"age"`
		CreatedAt   string `json:"created_at"`
	}

	out := make([]jsonItem, len(items))
	for i, item := range items {
		out[i] = jsonItem{
			ID:          item.ID,
			Type:        item.TypeString(),
			Priority:    item.Priority,
			Source:      item.Source,
			Description: item.Description,
			Age:         item.Age(),
			CreatedAt:   item.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

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
