package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	cliagents "github.com/nevinsm/sol/internal/cliapi/agents"
	"github.com/nevinsm/sol/internal/cliformat"
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
	agentCreateJSON  bool
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

		// Enforce max_active for outpost agents (role=outpost).
		if agentCreateRole == "outpost" {
			worldCfg, err := config.LoadWorldConfig(world)
			if err != nil {
				return fmt.Errorf("failed to load world config for %q: %w", world, err)
			}
			if worldCfg.Agents.MaxActive > 0 {
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
				if count >= worldCfg.Agents.MaxActive {
					return fmt.Errorf("world %s has reached agent capacity (%d)", world, worldCfg.Agents.MaxActive)
				}
			}
		}

		id, err := sphereStore.CreateAgent(name, world, agentCreateRole)
		if err != nil {
			return err
		}

		if agentCreateJSON {
			agent, err := sphereStore.GetAgent(id)
			if err != nil {
				return fmt.Errorf("failed to read created agent: %w", err)
			}
			return printJSON(cliagents.FromStoreAgent(*agent, "", "", nil))
		}

		fmt.Printf("Created agent %s\n", id)
		return nil
	},
}

// --- sol agent list ---

var (
	agentListWorld string
	agentListJSON  bool
	agentListAll   bool
)

// agentListRow is an alias for the canonical cliapi type.
type agentListRow = cliagents.AgentListRow

var agentListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List agents",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// When --all is set, list across every world and do NOT resolve a
		// single world context. Otherwise use the standard resolution
		// (flag > $SOL_WORLD > detected from cwd).
		var world string
		if !agentListAll {
			w, err := config.ResolveWorld(agentListWorld)
			if err != nil {
				return err
			}
			world = w
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

		rows := buildAgentListRows(agents, time.Now())

		if agentListJSON {
			if rows == nil {
				rows = []agentListRow{}
			}
			return printJSON(rows)
		}

		if len(rows) == 0 {
			fmt.Println("No agents found.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tWORLD\tROLE\tSTATE\tACTIVE WRIT\tMODEL\tACCOUNT\tLAST SEEN")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.ID, r.Name, r.World, r.Role, r.State,
				r.ActiveWrit, r.Model, r.Account, r.LastSeen)
		}
		tw.Flush()
		fmt.Println(cliformat.FormatCount(len(rows), "agent", "agents"))
		return nil
	},
}

// buildAgentListRows enriches raw store.Agent records with the columns
// `sol agent list` surfaces: active writ, model (from world config),
// account (from agent claude-config), and last-seen (from agents.updated_at
// as a proxy — no dedicated heartbeat column exists in the sphere schema).
//
// A per-world config cache avoids re-reading world.toml for each agent.
// now is passed explicitly so tests can pin the relative-timestamp output.
func buildAgentListRows(agents []store.Agent, now time.Time) []agentListRow {
	if len(agents) == 0 {
		return nil
	}

	// Cache loaded world configs so --all doesn't repeat I/O per agent.
	// A sentinel entry with ok=false records worlds whose config failed
	// to load so we don't retry them.
	type cfgEntry struct {
		cfg config.WorldConfig
		ok  bool
	}
	worldCfgCache := make(map[string]cfgEntry)
	getWorldCfg := func(w string) (config.WorldConfig, bool) {
		if e, seen := worldCfgCache[w]; seen {
			return e.cfg, e.ok
		}
		cfg, err := config.LoadWorldConfig(w)
		if err != nil {
			worldCfgCache[w] = cfgEntry{ok: false}
			return config.WorldConfig{}, false
		}
		worldCfgCache[w] = cfgEntry{cfg: cfg, ok: true}
		return cfg, true
	}

	rows := make([]agentListRow, 0, len(agents))
	for _, a := range agents {
		row := agentListRow{
			ID:         a.ID,
			Name:       a.Name,
			World:      a.World,
			Role:       a.Role,
			State:      a.State,
			ActiveWrit: valueOrEmptyMarker(a.ActiveWrit),
			Model:      cliformat.EmptyMarker,
			Account:    cliformat.EmptyMarker,
			LastSeen:   cliformat.FormatTimestampOrRelative(a.UpdatedAt, now),
		}

		if cfg, ok := getWorldCfg(a.World); ok {
			runtime := cfg.ResolveRuntime(a.Role)
			if m := cfg.ResolveModel(a.Role, runtime); m != "" {
				row.Model = m
			}
		}

		if acct := readAgentAccountBinding(a.World, a.Role, a.Name); acct != "" {
			row.Account = acct
		}

		rows = append(rows, row)
	}
	return rows
}

// valueOrEmptyMarker returns s, or cliformat.EmptyMarker when s is empty.
// Used for table cells where an empty string should render as "-".
func valueOrEmptyMarker(s string) string {
	if s == "" {
		return cliformat.EmptyMarker
	}
	return s
}

// readAgentAccountBinding returns the account handle bound to an agent's
// claude-config directory, or "" if no binding exists or cannot be read.
//
// This mirrors the broker-managed .account metadata read by
// internal/account (unexported there); for `sol agent list` we only need
// the handle, not the full binding inspection, so we read the file
// directly rather than widening that package's public surface.
func readAgentAccountBinding(world, role, name string) string {
	configDir := config.ClaudeConfigDir(config.WorldDir(world), role, name)
	data, err := os.ReadFile(filepath.Join(configDir, ".account"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// --- sol agent reset ---

var agentResetWorld string
var agentResetConfirm bool
var agentResetJSON bool

var agentResetCmd = &cobra.Command{
	Use:   "reset <name>",
	Short: "Reset a stuck agent to idle state",
	Long: `Force an agent back to idle when it's stuck in a bad state.

Clears the agent's tether file and sets the agent state to idle. If the
agent's active writ is in a non-terminal state (open/tethered/working/
resolve), it is returned to "open" with its assignee cleared. If the writ
is already in a terminal state (done/closed), it is left untouched — its
status and assignee are part of the historical record. Warns if the
agent's tmux session is still running — consider stopping it first to
avoid conflicting state.

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
			if agentResetJSON {
				return printJSON(cliagents.FromStoreAgent(*agent, "", "", nil))
			}
			fmt.Printf("Agent %s is already idle with no tether — nothing to reset.\n", agentID)
			return nil
		}

		// Look up the active writ's current status so both the preview and
		// the confirm path can decide whether to touch it. A writ already in
		// a terminal state (done/closed) must not be silently reverted to
		// open — doing so would clobber the completed work and the assignee
		// field, which is part of the historical record.
		activeWritID := agent.ActiveWrit
		var worldStore *store.WorldStore
		var activeWritStatus string
		var activeWritTerminal bool
		if activeWritID != "" {
			worldStore, err = store.OpenWorld(world)
			if err != nil {
				return fmt.Errorf("failed to open world store: %w", err)
			}
			defer worldStore.Close()

			w, err := worldStore.GetWrit(activeWritID)
			if err != nil {
				// Writ row is missing — log and fall through. We'll still
				// clear the tether / reset the agent below. Treat as
				// non-terminal so the old behavior (attempting UpdateWrit,
				// which will fail loudly) is preserved for non-confirm paths.
				fmt.Fprintf(os.Stderr, "WARNING: failed to look up active writ %s: %v\n", activeWritID, err)
			} else {
				activeWritStatus = w.Status
				activeWritTerminal = store.IsTerminalStatus(w.Status)
			}
		}

		// Preview mode: show what would be reset and exit 1.
		if !agentResetConfirm {
			fmt.Printf("Would reset agent %s:\n", agentID)
			fmt.Printf("  State: %s → idle\n", agent.State)
			if activeWritID != "" {
				if activeWritTerminal {
					fmt.Printf("  Active writ: %s (already %s, will not be modified)\n", activeWritID, activeWritStatus)
				} else {
					fmt.Printf("  Active writ: %s → returned to open pool\n", activeWritID)
				}
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

		// Untether the writ if one is assigned and not already terminal.
		// For terminal writs, leave status and assignee untouched — those
		// fields belong to the historical record.
		if activeWritID != "" {
			if activeWritTerminal {
				if !agentResetJSON {
					fmt.Printf("Active writ %s is already in terminal state %q; clearing tether only\n", activeWritID, activeWritStatus)
				}
			} else if worldStore != nil {
				if err := worldStore.UpdateWrit(activeWritID, store.WritUpdates{
					Status:   "open",
					Assignee: "-",
				}); err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: failed to untether writ %s: %v\n", activeWritID, err)
				} else if !agentResetJSON {
					fmt.Printf("Untethered writ %s (status → open, assignee cleared)\n", activeWritID)
				}
			}
		}

		// Clear the tether file.
		if tether.IsTethered(world, name, agent.Role) {
			if err := tether.Clear(world, name, agent.Role); err != nil {
				fmt.Fprintf(os.Stderr, "WARNING: failed to clear tether file: %v\n", err)
			} else if !agentResetJSON {
				fmt.Println("Cleared tether file")
			}
		}

		// Reset agent state to idle.
		if err := sphereStore.UpdateAgentState(agentID, store.AgentIdle, ""); err != nil {
			return fmt.Errorf("failed to reset agent state: %w", err)
		}

		if agentResetJSON {
			agent, err := sphereStore.GetAgent(agentID)
			if err != nil {
				return fmt.Errorf("failed to read reset agent: %w", err)
			}
			return printJSON(cliagents.FromStoreAgent(*agent, "", "", nil))
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
	agentCreateCmd.Flags().BoolVar(&agentCreateJSON, "json", false, "output as JSON")

	agentCmd.AddCommand(agentListCmd)
	agentListCmd.Flags().StringVar(&agentListWorld, "world", "",
		"world name (defaults to $SOL_WORLD or detected from current worktree)")
	agentListCmd.Flags().BoolVar(&agentListJSON, "json", false, "output as JSON")
	agentListCmd.Flags().BoolVar(&agentListAll, "all", false,
		"list agents across all worlds (overrides --world and cwd detection)")

	agentCmd.AddCommand(agentResetCmd)
	agentResetCmd.Flags().StringVar(&agentResetWorld, "world", "", "world name")
	agentResetCmd.Flags().BoolVar(&agentResetConfirm, "confirm", false, "confirm the destructive operation")
	agentResetCmd.Flags().BoolVar(&agentResetJSON, "json", false, "output as JSON")
}
