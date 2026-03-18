package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:     "agent",
	Short:   "Manage agents",
	GroupID: groupAgents,
}

// --- sol agent create ---

var (
	agentCreateWorld string
	agentCreateRole  string
)

var agentCreateCmd = &cobra.Command{
	Use:          "create <name>",
	Short:        "Create an agent",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(agentCreateWorld)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		// Enforce capacity for outpost agents (role=outpost).
		if agentCreateRole == "outpost" {
			worldCfg, err := config.LoadWorldConfig(world)
			if err != nil {
				return fmt.Errorf("failed to load world config for %q: %w", world, err)
			}
			if worldCfg.Agents.Capacity > 0 {
				agents, err := sphereStore.ListAgents(world, "")
				if err != nil {
					return fmt.Errorf("failed to list agents for world %q: %w", world, err)
				}
				count := 0
				for _, a := range agents {
					if a.Role == "outpost" {
						count++
					}
				}
				if count >= worldCfg.Agents.Capacity {
					return fmt.Errorf("world %s has reached agent capacity (%d)", world, worldCfg.Agents.Capacity)
				}
			}
		}

		id, err := sphereStore.CreateAgent(name, world, agentCreateRole)
		if err != nil {
			return err
		}
		fmt.Printf("Created agent %s\n", id)
		return nil
	},
}

// --- sol agent list ---

var (
	agentListWorld string
	agentListJSON  bool
)

var agentListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List agents",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(agentListWorld)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		agents, err := sphereStore.ListAgents(world, "")
		if err != nil {
			return err
		}

		if agentListJSON {
			return printJSON(agents)
		}

		if len(agents) == 0 {
			fmt.Println("No agents found.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tNAME\tWORLD\tROLE\tSTATE\tTETHER ITEM\n")
		for _, a := range agents {
			activeWrit := a.ActiveWrit
			if activeWrit == "" {
				activeWrit = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Name, a.World, a.Role, a.State, activeWrit)
		}
		tw.Flush()
		return nil
	},
}

// --- sol agent reset ---

var agentResetWorld string
var agentResetConfirm bool

var agentResetCmd = &cobra.Command{
	Use:   "reset <name>",
	Short: "Reset a stuck agent to idle state",
	Long: `Force an agent back to idle when it's stuck in a bad state.

Clears the agent's tether file, returns any assigned writ to "open" status
(with assignee cleared), and sets the agent state to idle. Warns if the
agent's tmux session is still running — consider stopping it first to avoid
conflicting state.

Requires --confirm to proceed; without it, previews what would be reset and exits 1.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(agentResetWorld)
		if err != nil {
			return err
		}

		agentID := world + "/" + name

		// Open sphere store and look up agent.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		agent, err := sphereStore.GetAgent(agentID)
		if err != nil {
			return fmt.Errorf("agent %q not found: %w", agentID, err)
		}

		// Nothing to reset if already idle with no tether.
		if agent.State == "idle" && agent.ActiveWrit == "" && !tether.IsTethered(world, name, agent.Role) {
			fmt.Printf("Agent %s is already idle with no tether — nothing to reset.\n", agentID)
			return nil
		}

		// Preview mode: show what would be reset and exit 1.
		if !agentResetConfirm {
			fmt.Printf("Would reset agent %s:\n", agentID)
			fmt.Printf("  State: %s → idle\n", agent.State)
			if agent.ActiveWrit != "" {
				fmt.Printf("  Active writ: %s → returned to open pool\n", agent.ActiveWrit)
			}
			if tether.IsTethered(world, name, agent.Role) {
				fmt.Println("  Tether file: will be cleared")
			}
			// Warn if the agent session is still alive.
			sessionName := config.SessionName(world, name)
			mgr := session.New()
			if mgr.Exists(sessionName) {
				fmt.Fprintf(os.Stderr, "WARNING: session %q is still alive — consider stopping it first\n", sessionName)
			}
			fmt.Println("Run with --confirm to proceed.")
			return &exitError{code: 1}
		}

		// Warn if the agent session is still alive.
		sessionName := config.SessionName(world, name)
		mgr := session.New()
		if mgr.Exists(sessionName) {
			fmt.Fprintf(os.Stderr, "WARNING: session %q is still alive — consider stopping it first\n", sessionName)
		}

		// Untether the writ if one is assigned.
		activeWritID := agent.ActiveWrit
		if activeWritID != "" {
			worldStore, err := store.OpenWorld(world)
			if err != nil {
				return fmt.Errorf("failed to open world store: %w", err)
			}
			defer worldStore.Close()

			if err := worldStore.UpdateWrit(activeWritID, store.WritUpdates{
				Status:   "open",
				Assignee: "-",
			}); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: failed to untether writ %s: %v\n", activeWritID, err)
			} else {
				fmt.Printf("Untethered writ %s (status → open, assignee cleared)\n", activeWritID)
			}
		}

		// Clear the tether file.
		if tether.IsTethered(world, name, agent.Role) {
			if err := tether.Clear(world, name, agent.Role); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: failed to clear tether file: %v\n", err)
			} else {
				fmt.Println("Cleared tether file")
			}
		}

		// Reset agent state to idle.
		if err := sphereStore.UpdateAgentState(agentID, store.AgentIdle, ""); err != nil {
			return fmt.Errorf("failed to reset agent state: %w", err)
		}
		fmt.Printf("Reset agent %s (state → idle, active_writ cleared)\n", agentID)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)

	agentCmd.AddCommand(agentCreateCmd)
	agentCreateCmd.Flags().StringVar(&agentCreateWorld, "world", "", "world name")
	agentCreateCmd.Flags().StringVar(&agentCreateRole, "role", "outpost", "agent role")

	agentCmd.AddCommand(agentListCmd)
	agentListCmd.Flags().StringVar(&agentListWorld, "world", "", "world name")
	agentListCmd.Flags().BoolVar(&agentListJSON, "json", false, "output as JSON")

	agentCmd.AddCommand(agentResetCmd)
	agentResetCmd.Flags().StringVar(&agentResetWorld, "world", "", "world name")
	agentResetCmd.Flags().BoolVar(&agentResetConfirm, "confirm", false, "confirm the destructive operation")
}
