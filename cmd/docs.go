package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var docsCmd = &cobra.Command{
	Use:     "docs",
	Short:   "Documentation tools",
	GroupID: groupPlumbing,
	// Override root PersistentPreRunE — docs commands don't need SOL_HOME.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

var docsGenerateCmd = &cobra.Command{
	Use:          "generate",
	Short:        "Generate CLI reference documentation",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return generateDocs(cmd.Root())
	},
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.AddCommand(docsGenerateCmd)
}

// docSection defines a section in the CLI reference documentation.
type docSection struct {
	heading   string   // markdown heading (## level)
	paths     []string // command paths relative to root
	intro     string   // text before the table
	notes     string   // text after the table
	noHeading bool     // if true, omit the heading
}

const worldResolutionIntro = `## World Resolution

Most commands accept a ` + "`--world`" + ` flag. When omitted, the world is resolved automatically:

1. **` + "`--world=W`" + ` flag** — explicit, always wins
2. **` + "`SOL_WORLD`" + ` env var** — set automatically in agent sessions
3. **Current directory** — if cwd is under ` + "`$SOL_HOME/{world}/`" + `, the world is inferred

This means ` + "`--world`" + ` is optional when running from inside a world directory (e.g., an agent worktree) or when ` + "`SOL_WORLD`" + ` is set.`

var docSections = []docSection{
	{
		heading: "Setup",
		paths:   []string{"init", "doctor"},
	},
	{
		heading: "Account Management",
		paths:   []string{"account add", "account list", "account remove", "account default", "account login"},
		notes:   "Accounts are stored under `$SOL_HOME/.accounts/`. Each account has its own config directory with OAuth credentials. Agents reference accounts via credential symlinks in their config dirs.",
	},
	{
		heading: "Quota Management",
		paths:   []string{"quota scan", "quota status"},
		notes:   "Quota state is stored at `$SOL_HOME/.accounts/runtime/quota.json`. The scan command reads the bottom 20 lines of each agent's tmux pane and matches against known Claude rate limit error patterns.",
	},
	{
		heading: "World Management",
		paths:   []string{"world init", "world list", "world status", "world delete", "world clone", "world sync", "world import", "world sleep", "world wake", "world summary", "world query", "world export"},
	},
	{
		heading: "Dispatch",
		paths:   []string{"cast", "tether", "untether", "prime", "resolve"},
		notes: "`cast` accepts `--world` (or `SOL_WORLD` env), `--agent` (auto-selects idle if omitted), `--formula`, `--var`, and `--account` flags.\n\n" +
			"### Account resolution for credentials\n\n" +
			"When an agent session starts, credentials are symlinked from the resolved account's directory. Resolution priority:\n\n" +
			"1. `--account` flag on `sol cast` (per-dispatch override)\n" +
			"2. `default_account` in `world.toml` (per-world default)\n" +
			"3. `sol account default` (sphere-level default from registry)\n" +
			"4. `~/.claude/.credentials.json` (fallback when no accounts are configured)",
	},
	{
		heading: "Agents",
		paths:   []string{"agent create", "agent list", "agent reset", "agent postmortem", "agent history", "agent handoffs", "agent stats"},
	},
	{
		heading: "Store (Work Items)",
		paths:   []string{"store create", "store status", "store list", "store update", "store close", "store query"},
	},
	{
		heading: "Dependencies",
		paths:   []string{"store dep add", "store dep remove", "store dep list"},
	},
	{
		heading: "Sessions",
		paths:   []string{"session start", "session stop", "session list", "session health", "session capture", "session attach", "session inject"},
	},
	{
		heading: "Daemon Management",
		paths:   []string{"up", "down"},
		notes: "Without flags, `sol up` starts sphere daemons (prefect, consul, chronicle, ledger) and world services " +
			"(sentinel, forge) for all non-sleeping worlds. `sol down` stops everything.\n\n" +
			"`--world` — manage only world services, skip sphere daemons. " +
			"`--world=W` targets a specific world.",
	},
	{
		heading: "Supervision",
		paths:   []string{"prefect run", "prefect stop", "status"},
	},
	{
		heading: "Sentinel (Per-World Health Monitor)",
		paths:   []string{"sentinel run", "sentinel start", "sentinel stop", "sentinel attach"},
	},
	{
		heading: "Merge Requests (Plumbing)",
		paths:   []string{"mr create"},
	},
	{
		heading: "Forge (Merge Pipeline)",
		paths:   []string{"forge start", "forge stop", "forge sync", "forge attach", "forge status", "forge queue", "forge pause", "forge resume"},
	},
	{
		noHeading: true,
		intro:     "Toolbox subcommands (used by the forge Claude session):",
		paths:     []string{"forge ready", "forge blocked", "forge claim", "forge release", "forge mark-merged", "forge mark-failed", "forge create-resolution", "forge check-unblocked", "forge await"},
	},
	{
		heading: "Messaging",
		paths:   []string{"mail send", "mail inbox", "mail read", "mail ack", "mail check"},
	},
	{
		heading: "Nudge Queue (Inbox)",
		paths:   []string{"inbox", "inbox count", "inbox drain"},
		notes:   "Nudge queue counts are also shown in the NUDGE column of `sol status --world=W` agent and envoy tables.",
	},
	{
		heading: "Escalations",
		paths:   []string{"escalate", "escalation list", "escalation ack", "escalation resolve"},
	},
	{
		heading: "Observability",
		paths:   []string{"feed", "log-event", "chronicle run", "chronicle start", "chronicle stop"},
	},
	{
		heading: "Ledger (Token Tracking)",
		paths:   []string{"ledger run", "ledger start", "ledger stop"},
		notes: "Sphere-scoped OTLP HTTP receiver on port 4318. Accepts `claude_code.api_request` log events from " +
			"Claude Code agent sessions, extracts token counts (input, output, cache_read, cache_creation) and model, " +
			"and writes `token_usage` records to the appropriate world database. Source agent identification via " +
			"`OTEL_RESOURCE_ATTRIBUTES` (agent.name, world, work_item_id) injected at cast time.",
	},
	{
		heading: "Workflows",
		paths:   []string{"workflow instantiate", "workflow manifest", "workflow current", "workflow advance", "workflow status"},
	},
	{
		heading: "Caravans",
		paths:   []string{"caravan create", "caravan add", "caravan list", "caravan check", "caravan status", "caravan launch", "caravan commission", "caravan drydock", "caravan set-phase", "caravan close", "caravan dep add", "caravan dep remove", "caravan dep list"},
	},
	{
		heading: "Agent Memories",
		paths:   []string{"remember", "memories", "forget"},
		notes: "Memories are key-value pairs stored in the world database, scoped to each agent name. " +
			"They survive across sessions and handoffs. With a single argument, `sol remember` auto-generates " +
			"a key from a hash of the value. Memories are injected during prime so successor sessions see them automatically.",
	},
	{
		heading: "Handoff (Session Continuity)",
		paths:   []string{"handoff"},
		notes: "`--summary` provides a progress summary. `--reason` tags the handoff with a reason (`compact`, `manual`, `health-check`; defaults to `unknown`). " +
			"Captures tmux output, git state, and workflow progress into `.handoff.json`, " +
			"then cycles the session atomically using `tmux respawn-pane`. Safe for self-handoff (agent calling handoff on itself) " +
			"and PreCompact auto-handoff — the old process is replaced without destroying the session. " +
			"Each handoff emits a chronicle event with reason, session age, and role for observability. " +
			"When reason is `compact`, the new session uses `--continue` and gets a lightweight prime that omits the full work item description.",
	},
	{
		heading: "Envoy (Persistent Human-Directed Agents)",
		paths:   []string{"envoy create", "envoy start", "envoy stop", "envoy attach", "envoy list", "envoy brief", "envoy debrief", "envoy sync", "envoy delete"},
	},
	{
		heading: "Governor (Per-World Coordinator)",
		paths:   []string{"governor start", "governor stop", "governor attach", "governor brief", "governor debrief", "governor summary", "governor sync"},
	},
	{
		heading: "Nudge (Inter-Agent Notifications)",
		paths:   []string{"nudge drain"},
	},
	{
		heading: "Brief (Agent Context)",
		paths:   []string{"brief inject"},
	},
	{
		heading: "Consul (Sphere-Level Patrol)",
		paths:   []string{"consul run", "consul status"},
		notes:   "`consul run` accepts `--interval` (default 5m), `--stale-timeout` (default 1h), and `--webhook` for escalation notifications.",
	},
	{
		heading: "Senate (Sphere-Scoped Planner)",
		paths:   []string{"senate start", "senate stop", "senate attach", "senate brief", "senate debrief"},
		notes:   "Senate is an operator-managed sphere-scoped planning session. It reads governor world summaries via `sol world summary` and queries governors via `sol world query`. Not supervised by prefect — start and stop manually.",
	},
	{
		heading: "Service (Systemd Units)",
		paths:   []string{"service install", "service uninstall", "service start", "service stop", "service restart", "service status"},
		notes:   "Linux-only. Manages systemd user units for sol sphere daemons (prefect, consul, chronicle, ledger).",
	},
	{
		heading: "Quota (Rate Limit Rotation)",
		paths:   []string{"quota rotate"},
		notes: "Reads quota state from `$SOL_HOME/.runtime/quota.json` to find rate-limited accounts, " +
			"selects available accounts via LRU, swaps credential symlinks, and respawns agent sessions " +
			"with `--continue` for context preservation. When no accounts are available, agents are paused " +
			"and automatically restarted by the sentinel when accounts become available.",
	},
	{
		heading: "Guard (PreToolUse Hooks)",
		paths:   []string{"guard dangerous-command", "guard workflow-bypass"},
		notes: "Guards are called by PreToolUse hooks in `.claude/settings.local.json`. They read tool input from stdin " +
			"(Claude Code hook protocol) and exit 2 to block, 0 to allow. `workflow-bypass` respects `SOL_ROLE` — " +
			"forge is exempt since it pushes to the target branch for merges.",
	},
	{
		heading: "Documentation",
		paths:   []string{"docs generate"},
	},
}

// skipUndocumented lists command paths that are intentionally undocumented
// (aliases, cobra built-ins, etc.).
var skipUndocumented = map[string]bool{
	"store get": true, // alias for store status
	"help":      true, // cobra built-in
}

// skipUndocumentedPrefixes lists path prefixes for auto-generated command
// trees that should not trigger undocumented warnings.
var skipUndocumentedPrefixes = []string{
	"completion", // cobra built-in shell completion
}

func generateDocs(root *cobra.Command) error {
	allCommands := collectRunnableCommands(root)
	documented := make(map[string]bool)

	var b strings.Builder

	b.WriteString("# CLI Reference\n\n")
	b.WriteString(worldResolutionIntro)
	b.WriteString("\n")

	for _, sec := range docSections {
		if !sec.noHeading {
			b.WriteString("\n## ")
			b.WriteString(sec.heading)
			b.WriteString("\n")
		}

		if sec.intro != "" {
			b.WriteString("\n")
			b.WriteString(sec.intro)
			b.WriteString("\n")
		}

		b.WriteString("\n| Command | Description |\n")
		b.WriteString("|---------|-------------|\n")

		for _, path := range sec.paths {
			cmd, ok := allCommands[path]
			if !ok {
				return fmt.Errorf("documented command not found: %q", path)
			}
			documented[path] = true

			display := formatCommandCell(path, cmd)
			b.WriteString("| ")
			b.WriteString(display)
			b.WriteString(" | ")
			b.WriteString(cmd.Short)
			b.WriteString(" |\n")
		}

		if sec.notes != "" {
			b.WriteString("\n")
			b.WriteString(sec.notes)
			b.WriteString("\n")
		}
	}

	fmt.Print(b.String())

	// Warn about commands not covered by any section.
	var undocumented []string
	for path := range allCommands {
		if !documented[path] && !isSkippedCommand(path) {
			undocumented = append(undocumented, path)
		}
	}
	if len(undocumented) > 0 {
		sort.Strings(undocumented)
		fmt.Fprintf(os.Stderr, "warning: undocumented commands:\n")
		for _, path := range undocumented {
			fmt.Fprintf(os.Stderr, "  %s\n", path)
		}
	}

	return nil
}

func isSkippedCommand(path string) bool {
	if skipUndocumented[path] {
		return true
	}
	for _, prefix := range skipUndocumentedPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+" ") {
			return true
		}
	}
	return false
}

// collectRunnableCommands walks the command tree and returns all runnable
// (non-hidden) commands keyed by their full path relative to root.
func collectRunnableCommands(root *cobra.Command) map[string]*cobra.Command {
	m := make(map[string]*cobra.Command)
	var walk func(cmd *cobra.Command, prefix string)
	walk = func(cmd *cobra.Command, prefix string) {
		for _, child := range cmd.Commands() {
			if child.Hidden {
				continue
			}
			name := child.Name()
			path := name
			if prefix != "" {
				path = prefix + " " + name
			}
			if child.RunE != nil || child.Run != nil {
				m[path] = child
			}
			walk(child, path)
		}
	}
	walk(root, "")
	return m
}

// formatCommandCell builds the markdown cell for a command's display name,
// e.g., "`sol world init <name>`".
func formatCommandCell(path string, cmd *cobra.Command) string {
	args := ""
	if idx := strings.IndexByte(cmd.Use, ' '); idx >= 0 {
		args = cmd.Use[idx:]
	}
	return "`sol " + path + args + "`"
}
