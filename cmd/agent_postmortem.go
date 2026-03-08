package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/spf13/cobra"
)

var (
	postmortemWorld   string
	postmortemJSON    bool
	postmortemLines   int
	postmortemCommits int
)

// PostmortemReport holds all diagnostic data for a dead (or any) agent.
type PostmortemReport struct {
	Agent       PostmortemAgent       `json:"agent"`
	Session     PostmortemSession     `json:"session"`
	Writ    *PostmortemWrit   `json:"writ,omitempty"`
	Commits     []string              `json:"commits"`
	LastOutput  string                `json:"last_output,omitempty"`
	Handoff     *PostmortemHandoff    `json:"handoff,omitempty"`
}

type PostmortemAgent struct {
	Name      string    `json:"name"`
	World     string    `json:"world"`
	Role      string    `json:"role"`
	State     string    `json:"state"`
	ActiveWrit string   `json:"active_writ,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PostmortemSession struct {
	Name      string        `json:"name"`
	Alive     bool          `json:"alive"`
	StartedAt *time.Time    `json:"started_at,omitempty"`
	Lifetime  string        `json:"lifetime,omitempty"`
}

type PostmortemWrit struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type PostmortemHandoff struct {
	Summary     string    `json:"summary"`
	HandedOffAt time.Time `json:"handed_off_at"`
	Commits     []string  `json:"commits,omitempty"`
}

var agentPostmortemCmd = &cobra.Command{
	Use:          "postmortem <name>",
	Short:        "Show diagnostic information for a dead or stuck agent",
	Long:         "Gathers session metadata, commit history, writ state, and last output for an agent — particularly useful for understanding what happened when an outpost dies mid-work.",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(postmortemWorld)
		if err != nil {
			return err
		}

		agentID := world + "/" + name

		// 1. Look up the agent record.
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		agent, err := sphereStore.GetAgent(agentID)
		if err != nil {
			return fmt.Errorf("agent %q not found: %w", agentID, err)
		}

		// 2. Check session state.
		mgr := session.New()
		sessName := config.SessionName(world, name)
		report := PostmortemReport{
			Agent: PostmortemAgent{
				Name:       agent.Name,
				World:      agent.World,
				Role:       agent.Role,
				State:      agent.State,
				ActiveWrit: agent.ActiveWrit,
				CreatedAt:  agent.CreatedAt,
				UpdatedAt:  agent.UpdatedAt,
			},
			Session: PostmortemSession{
				Name: sessName,
			},
		}

		// 3. Session metadata (persists even after death).
		meta, _ := mgr.GetMeta(sessName)
		if meta != nil {
			report.Session.Alive = meta.Alive
			report.Session.StartedAt = &meta.StartedAt
			var lifetime time.Duration
			if meta.Alive {
				lifetime = time.Since(meta.StartedAt)
			} else {
				lifetime = agent.UpdatedAt.Sub(meta.StartedAt)
				if lifetime < 0 {
					lifetime = time.Since(meta.StartedAt)
				}
			}
			report.Session.Lifetime = status.FormatDuration(lifetime)
		} else {
			report.Session.Alive = mgr.Exists(sessName)
		}

		// 4. Work item details.
		writID := agent.ActiveWrit
		if writID == "" {
			writID, _ = tether.Read(world, name, agent.Role)
		}
		if writID != "" {
			worldStore, err := store.OpenWorld(world)
			if err == nil {
				defer worldStore.Close()
				item, err := worldStore.GetWrit(writID)
				if err == nil {
					report.Writ = &PostmortemWrit{
						ID:     item.ID,
						Title:  item.Title,
						Status: item.Status,
					}
				}
			}
		}

		// 5. Git commits from the worktree.
		worktreeDir := config.WorktreePath(world, name)
		commits, _ := handoff.GitLog(worktreeDir, postmortemCommits)
		report.Commits = commits

		// 6. Try session capture (only works if tmux session still exists).
		if mgr.Exists(sessName) {
			output, err := mgr.Capture(sessName, postmortemLines)
			if err == nil {
				report.LastOutput = output
			}
		}

		// 7. Check for handoff state.
		role := agent.Role
		if role == "" {
			role = "agent"
		}
		handoffState, _ := handoff.Read(world, name, role)
		if handoffState != nil {
			report.Handoff = &PostmortemHandoff{
				Summary:     handoffState.Summary,
				HandedOffAt: handoffState.HandedOffAt,
				Commits:     handoffState.RecentCommits,
			}
		}

		// 8. Render output.
		if postmortemJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(report)
		}

		renderPostmortem(report)
		return nil
	},
}

func renderPostmortem(r PostmortemReport) {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	var b strings.Builder

	// Header.
	b.WriteString(headerStyle.Render(fmt.Sprintf("Agent Postmortem: %s", r.Agent.Name)))
	b.WriteString("\n\n")

	// Agent state.
	stateDisplay := r.Agent.State
	switch {
	case r.Agent.State == "working" && !r.Session.Alive:
		stateDisplay = errorStyle.Render("working (dead!)")
	case r.Agent.State == "working" && r.Session.Alive:
		stateDisplay = okStyle.Render("working")
	case r.Agent.State == "stalled":
		stateDisplay = warnStyle.Render("stalled")
	case r.Agent.State == "idle":
		stateDisplay = dimStyle.Render("idle")
	}

	b.WriteString(fmt.Sprintf("  %-18s %s\n", "State:", stateDisplay))
	b.WriteString(fmt.Sprintf("  %-18s %s\n", "World:", r.Agent.World))
	b.WriteString(fmt.Sprintf("  %-18s %s\n", "Role:", r.Agent.Role))

	// Work item.
	if r.Writ != nil {
		b.WriteString(fmt.Sprintf("  %-18s %s — %s\n", "Writ:", r.Writ.ID, r.Writ.Title))
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Work Status:", r.Writ.Status))
	} else if r.Agent.ActiveWrit != "" {
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Active Writ:", r.Agent.ActiveWrit))
	}

	b.WriteString("\n")

	// Session info.
	b.WriteString(headerStyle.Render("Session"))
	b.WriteString("\n")

	sessStatus := errorStyle.Render("dead")
	if r.Session.Alive {
		sessStatus = okStyle.Render("alive")
	}
	b.WriteString(fmt.Sprintf("  %-18s %s (%s)\n", "Session:", r.Session.Name, sessStatus))

	if r.Session.StartedAt != nil {
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Started:", r.Session.StartedAt.Format(time.RFC3339)))
	}
	if r.Session.Lifetime != "" {
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Lifetime:", r.Session.Lifetime))
	}
	b.WriteString(fmt.Sprintf("  %-18s %s\n", "Last State Update:", r.Agent.UpdatedAt.Format(time.RFC3339)))

	b.WriteString("\n")

	// Commits.
	b.WriteString(headerStyle.Render(fmt.Sprintf("Commits (%d)", len(r.Commits))))
	b.WriteString("\n")
	if len(r.Commits) == 0 {
		b.WriteString(dimStyle.Render("  (no commits found)"))
		b.WriteString("\n")
	} else {
		for _, c := range r.Commits {
			b.WriteString(fmt.Sprintf("  %s\n", c))
		}
	}

	b.WriteString("\n")

	// Last output.
	b.WriteString(headerStyle.Render("Last Session Output"))
	b.WriteString("\n")
	if r.LastOutput != "" {
		lines := strings.Split(strings.TrimRight(r.LastOutput, "\n"), "\n")
		for _, line := range lines {
			b.WriteString(fmt.Sprintf("  %s\n", dimStyle.Render(line)))
		}
	} else {
		b.WriteString(dimStyle.Render("  (session gone — no capture available)"))
		b.WriteString("\n")
	}

	// Handoff state.
	if r.Handoff != nil {
		b.WriteString("\n")
		b.WriteString(headerStyle.Render("Last Handoff"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "At:", r.Handoff.HandedOffAt.Format(time.RFC3339)))
		b.WriteString(fmt.Sprintf("  %-18s %s\n", "Summary:", r.Handoff.Summary))
	}

	fmt.Print(b.String())
}

func init() {
	agentCmd.AddCommand(agentPostmortemCmd)
	agentPostmortemCmd.Flags().StringVar(&postmortemWorld, "world", "", "world name")
	agentPostmortemCmd.Flags().BoolVar(&postmortemJSON, "json", false, "output as JSON")
	agentPostmortemCmd.Flags().IntVar(&postmortemLines, "lines", 50, "lines of session output to capture")
	agentPostmortemCmd.Flags().IntVar(&postmortemCommits, "commits", 10, "number of recent commits to show")
}
