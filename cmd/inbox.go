package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/spf13/cobra"
)

var (
	inboxWorld string
	inboxAgent string
	inboxJSON  bool
)

var inboxCmd = &cobra.Command{
	Use:          "inbox",
	Short:        "View pending nudge queue messages",
	GroupID:      groupCommunication,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := resolveInboxSession(cmd)
		if err != nil {
			return err
		}

		asJSON, _ := cmd.Flags().GetBool("json")

		msgs, err := nudge.List(session)
		if err != nil {
			return err
		}

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(msgs)
		}

		if len(msgs) == 0 {
			fmt.Println("No pending messages.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SENDER\tTYPE\tSUBJECT\tPRIORITY\tAGE")
		for _, m := range msgs {
			age := time.Since(m.CreatedAt).Truncate(time.Second)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", m.Sender, m.Type, m.Subject, m.Priority, age)
		}
		return w.Flush()
	},
}

var inboxCountCmd = &cobra.Command{
	Use:          "count",
	Short:        "Print count of pending messages",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := resolveInboxSession(cmd)
		if err != nil {
			return err
		}

		count, err := nudge.Peek(session)
		if err != nil {
			return err
		}

		fmt.Println(count)
		return nil
	},
}

var inboxDrainCmd = &cobra.Command{
	Use:          "drain",
	Short:        "Drain and display all pending messages",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := resolveInboxSession(cmd)
		if err != nil {
			return err
		}

		asJSON, _ := cmd.Flags().GetBool("json")

		msgs, err := nudge.Drain(session)
		if err != nil {
			return err
		}

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(msgs)
		}

		if len(msgs) == 0 {
			fmt.Println("No pending messages.")
			return nil
		}

		for i, m := range msgs {
			if i > 0 {
				fmt.Println("---")
			}
			fmt.Printf("From:     %s\n", m.Sender)
			fmt.Printf("Type:     %s\n", m.Type)
			fmt.Printf("Subject:  %s\n", m.Subject)
			fmt.Printf("Priority: %s\n", m.Priority)
			fmt.Printf("Age:      %s\n", time.Since(m.CreatedAt).Truncate(time.Second))
			if m.Body != "" {
				fmt.Printf("\n%s\n", m.Body)
			}
		}
		fmt.Fprintf(os.Stderr, "\nDrained %d message(s).\n", len(msgs))
		return nil
	},
}

// resolveInboxSession resolves the session name from flags, env vars, or cwd.
func resolveInboxSession(cmd *cobra.Command) (string, error) {
	worldFlag, _ := cmd.Flags().GetString("world")
	agent, _ := cmd.Flags().GetString("agent")

	world, err := config.ResolveWorld(worldFlag)
	if err != nil {
		return "", err
	}

	if agent == "" {
		agent = os.Getenv("SOL_AGENT")
	}
	if agent == "" {
		return "", fmt.Errorf("--agent is required (or set SOL_AGENT env var)")
	}

	return config.SessionName(world, agent), nil
}

func init() {
	rootCmd.AddCommand(inboxCmd)

	inboxCmd.PersistentFlags().StringVar(&inboxWorld, "world", "", "world name (defaults to SOL_WORLD env)")
	inboxCmd.PersistentFlags().StringVar(&inboxAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")

	inboxCmd.Flags().BoolVar(&inboxJSON, "json", false, "output as JSON")

	inboxCmd.AddCommand(inboxCountCmd)
	inboxCmd.AddCommand(inboxDrainCmd)
	inboxDrainCmd.Flags().Bool("json", false, "output as JSON")
}
