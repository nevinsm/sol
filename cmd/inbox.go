package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	inboxapi "github.com/nevinsm/sol/internal/cliapi/inbox"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/inbox"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var inboxJSON bool
var inboxIdentity string

var inboxCmd = &cobra.Command{
	Use:     "inbox",
	Short:   "Unified TUI for autarch escalations and mail",
	GroupID: groupCommunication,
	Long: `Launch a unified inbox TUI showing escalations and unread mail.

Presents a single priority-sorted view of everything needing the caller's
attention. Navigate with arrow keys, expand with enter, and take inline
actions (ack, resolve, dismiss).

Identity: --identity (default: auto-detected from SOL_WORLD/SOL_AGENT, or
autarch — same resolution as "sol mail") determines whose inbox is shown.
The autarch identity sees open escalations plus its own pending mail (the
original behavior). Any other identity sees only its own pending mail — no
escalations, since escalations are autarch-directed. Applies to both the
TUI and --json output.

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

	identity := resolveMailIdentity(inboxIdentity)

	if inboxJSON {
		return runInboxJSON(sphereStore, identity)
	}

	if err := requireTTYForInboxTUI(); err != nil {
		return err
	}

	cfg := inbox.Config{
		Store:       sphereStore,
		EventLogger: events.NewLogger(config.Home()),
		Identity:    identity,
	}

	m := inbox.NewModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())

	_, err = p.Run()
	return err
}

// requireTTYForInboxTUI returns a clear, actionable error when the inbox
// TUI cannot start because stdin or stdout isn't a terminal (e.g. piped
// output, a non-interactive script, or a CI job), instead of letting
// bubbletea fail deep inside with a raw "could not open a new TTY: open
// /dev/tty" error. --json bypasses this check entirely (see runInbox).
func requireTTYForInboxTUI() error {
	return requireTTY(term.IsTerminal(int(os.Stdin.Fd())), term.IsTerminal(int(os.Stdout.Fd())))
}

// requireTTY is the pure decision behind requireTTYForInboxTUI, split out
// so the logic is unit-testable without a real terminal.
func requireTTY(stdinIsTTY, stdoutIsTTY bool) error {
	if stdinIsTTY && stdoutIsTTY {
		return nil
	}
	return fmt.Errorf("sol inbox requires an interactive terminal; use \"sol inbox --json\" when scripting or redirecting output")
}

func runInboxJSON(sphereStore *store.SphereStore, identity string) error {
	items, err := inbox.FetchItems(sphereStore, identity)
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
	inboxCmd.Flags().StringVar(&inboxIdentity, "identity", "", "Caller identity to scope the inbox to (default: auto-detected from SOL_WORLD/SOL_AGENT, or autarch)")
}
