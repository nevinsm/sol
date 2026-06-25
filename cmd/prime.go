package cmd

import (
	"fmt"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/spf13/cobra"
)

var (
	primeWorld   string
	primeAgent   string
	primeRole    string
	primeCompact bool
)

var primeCmd = &cobra.Command{
	Use:          "prime",
	Short:        "Assemble and print execution context for an agent",
	GroupID:      groupPlumbing,
	Hidden:       true,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		world, err := config.ResolveWorld(primeWorld)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(primeAgent)
		if err != nil {
			return err
		}

		role := primeRole
		if role == "" {
			// Look up agent to determine role.
			sphereStore, err := store.OpenSphere()
			if err != nil {
				return err
			}
			defer sphereStore.Close()

			agentID := world + "/" + agent
			agentRecord, err := sphereStore.GetAgent(agentID)
			if err != nil {
				return fmt.Errorf("failed to get agent %q: %w", agentID, err)
			}
			role = agentRecord.Role
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			// World DB unavailable — emit a degraded prime and exit 0 so the
			// SessionStart hook does not abort agent startup during a DB outage.
			// This satisfies the DEGRADE guarantee in docs/principles.md:
			// "if SQLite store is unavailable, tethered agents continue."
			writID := tetherWritID(world, agent, role)
			fmt.Println(degradedPrimeMessage(world, agent, writID))
			return nil
		}
		defer worldStore.Close()

		result, err := dispatch.Prime(world, agent, role, worldStore, primeCompact)
		if err != nil {
			return err
		}

		fmt.Println(result.Output)
		return nil
	},
}

// tetherWritID reads the first tethered writ ID for the agent.
// Returns an empty string on any error (best-effort).
func tetherWritID(world, agent, role string) string {
	ids, err := tether.List(world, agent, role)
	if err != nil || len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// degradedPrimeMessage returns a prime output suitable for degraded mode
// (world DB temporarily unavailable). The agent can continue working on
// committed code; a full prime is available once the DB recovers.
func degradedPrimeMessage(world, agent, writID string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== DEGRADED CONTEXT ===\n")
	fmt.Fprintf(&b, "Agent: %s (world: %s)\n", agent, world)
	if writID != "" {
		fmt.Fprintf(&b, "Writ: %s\n", writID)
	}
	fmt.Fprintf(&b, "\nWorld DB is temporarily unavailable.\n")
	fmt.Fprintf(&b, "Continue working from committed state.\n")
	fmt.Fprintf(&b, "Run `sol prime --world=%s --agent=%s` once the DB is restored.\n", world, agent)
	fmt.Fprintf(&b, "=== END CONTEXT ===")
	return b.String()
}

func init() {
	rootCmd.AddCommand(primeCmd)
	primeCmd.Flags().StringVar(&primeWorld, "world", "", "world name")
	primeCmd.Flags().StringVar(&primeAgent, "agent", "", "agent name")
	primeCmd.Flags().StringVar(&primeRole, "role", "", "agent role (skips sphere DB lookup when provided)")
	primeCmd.Flags().BoolVar(&primeCompact, "compact", false, "output a short focus reminder instead of the full prime")
}
