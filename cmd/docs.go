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
		heading: "World Management",
		paths:   []string{"world init", "world list", "world status", "world delete", "world sync", "world query", "world summary"},
	},
	{
		heading: "Dispatch",
		paths:   []string{"cast", "tether", "untether", "prime", "resolve"},
		notes:   "`cast` accepts `--world` (or `SOL_WORLD` env), `--agent` (auto-selects idle if omitted), `--formula`, and `--var` flags.",
	},
	{
		heading: "Agents",
		paths:   []string{"agent create", "agent list", "agent reset", "agent postmortem"},
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
		paths:   []string{"forge start", "forge stop", "forge sync", "forge attach", "forge status", "forge queue"},
	},
	{
		noHeading: true,
		intro:     "Toolbox subcommands (used by the forge Claude session):",
		paths:     []string{"forge ready", "forge blocked", "forge claim", "forge release", "forge merge", "forge run-gates", "forge push", "forge mark-merged", "forge mark-failed", "forge create-resolution", "forge check-unblocked"},
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
		heading: "Workflows",
		paths:   []string{"workflow instantiate", "workflow current", "workflow advance", "workflow status"},
	},
	{
		heading: "Caravans",
		paths:   []string{"caravan create", "caravan add", "caravan list", "caravan check", "caravan status", "caravan launch", "caravan set-phase", "caravan close", "caravan dep add", "caravan dep remove", "caravan dep list"},
	},
	{
		heading: "Handoff (Session Continuity)",
		paths:   []string{"handoff"},
		notes: "`--summary` provides a progress summary. Captures tmux output, git state, and workflow progress into `.handoff.json`, " +
			"then cycles the session atomically using `tmux respawn-pane`. Safe for self-handoff (agent calling handoff on itself) " +
			"and PreCompact auto-handoff — the old process is replaced without destroying the session.",
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
		heading: "Service (Systemd Units)",
		paths:   []string{"service install", "service uninstall", "service start", "service stop", "service restart", "service status"},
		notes:   "Linux-only. Manages systemd user units for sol sphere daemons (prefect, consul, chronicle).",
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
