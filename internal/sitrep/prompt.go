package sitrep

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
)

//go:embed sitrep-prompt.md
var defaultPrompt string

// promptFilename is the name of the ejected prompt file in SOL_HOME.
const promptFilename = "sitrep-prompt.md"

// PromptPath returns the path where an ejected sitrep prompt would live.
func PromptPath() string {
	return filepath.Join(config.Home(), promptFilename)
}

// LoadSystemPrompt returns the system prompt for the sitrep AI call.
// If an ejected prompt file exists at $SOL_HOME/sitrep-prompt.md, it is used.
// Otherwise, the embedded default is returned.
func LoadSystemPrompt() (string, error) {
	ejectedPath := PromptPath()
	if data, err := os.ReadFile(ejectedPath); err == nil {
		return string(data), nil
	}
	return defaultPrompt, nil
}

// Eject writes the default prompt template to $SOL_HOME/sitrep-prompt.md.
// Returns an error if the file already exists and force is false.
func Eject(force bool) (string, error) {
	dest := PromptPath()

	if _, err := os.Stat(dest); err == nil {
		if !force {
			return "", fmt.Errorf("sitrep prompt already exists: %s (use --force to overwrite)", dest)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("failed to create directory for sitrep prompt: %w", err)
	}

	if err := os.WriteFile(dest, []byte(defaultPrompt), 0o644); err != nil {
		return "", fmt.Errorf("failed to write sitrep prompt: %w", err)
	}

	return dest, nil
}

// BuildPrompt constructs the full prompt from system prompt + data payload.
func BuildPrompt(data *CollectedData) (string, error) {
	systemPrompt, err := LoadSystemPrompt()
	if err != nil {
		return "", err
	}

	dataPayload := formatDataPayload(data)
	return systemPrompt + "\n\n---\n\n" + dataPayload, nil
}

// formatDataPayload renders CollectedData as structured markdown for the AI.
func formatDataPayload(data *CollectedData) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# System Data — Scope: %s\n\n", data.Scope))

	// Agents summary.
	b.WriteString("## Agents\n\n")
	if len(data.Agents) == 0 {
		b.WriteString("No agents registered.\n\n")
	} else {
		var idle, working, stalled int
		for _, a := range data.Agents {
			switch a.State {
			case "idle":
				idle++
			case "working":
				working++
			case "stalled":
				stalled++
			}
		}
		b.WriteString(fmt.Sprintf("Total: %d (idle: %d, working: %d, stalled: %d)\n\n",
			len(data.Agents), idle, working, stalled))

		// Detail for non-idle agents.
		for _, a := range data.Agents {
			if a.State == "idle" {
				continue
			}
			writ := a.ActiveWrit
			if writ == "" {
				writ = "(none)"
			}
			b.WriteString(fmt.Sprintf("- %s [%s/%s]: %s — writ: %s\n",
				a.Name, a.World, a.Role, a.State, writ))
		}
		if working > 0 || stalled > 0 {
			b.WriteString("\n")
		}
	}

	// Caravans summary.
	if len(data.Caravans) > 0 {
		b.WriteString("## Caravans\n\n")
		var open, drydock, ready, closed int
		for _, c := range data.Caravans {
			switch c.Status {
			case "open":
				open++
			case "drydock":
				drydock++
			case "ready":
				ready++
			case "closed":
				closed++
			}
		}
		b.WriteString(fmt.Sprintf("Total: %d (open: %d, drydock: %d, ready: %d, closed: %d)\n\n",
			len(data.Caravans), open, drydock, ready, closed))

		// Detail for non-closed caravans.
		for _, c := range data.Caravans {
			if c.Status == "closed" {
				continue
			}
			b.WriteString(fmt.Sprintf("### %s (%s) — %s\n", c.Name, c.ID, c.Status))
			if statuses, ok := data.CaravanReadiness[c.ID]; ok && len(statuses) > 0 {
				var readyCount, doneCount, closedCount, activeCount int
				for _, s := range statuses {
					switch {
					case s.WritStatus == "closed":
						closedCount++
					case s.WritStatus == "done":
						doneCount++
					case s.Ready:
						readyCount++
					case s.IsDispatched():
						activeCount++
					}
				}
				b.WriteString(fmt.Sprintf("Items: %d total — %d closed, %d done, %d ready, %d active\n",
					len(statuses), closedCount, doneCount, readyCount, activeCount))
			}
			b.WriteString("\n")
		}
	}

	// World data.
	for _, w := range data.Worlds {
		b.WriteString(fmt.Sprintf("## World: %s\n\n", w.Name))

		// Writs summary.
		if len(w.Writs) == 0 {
			b.WriteString("No writs.\n\n")
		} else {
			statusCounts := map[string]int{}
			for _, wr := range w.Writs {
				statusCounts[wr.Status]++
			}
			b.WriteString(fmt.Sprintf("Writs: %d total", len(w.Writs)))
			for _, s := range []string{"open", "tethered", "done", "closed"} {
				if n, ok := statusCounts[s]; ok {
					b.WriteString(fmt.Sprintf(", %s: %d", s, n))
				}
			}
			b.WriteString("\n")

			// Detail for active writs (non-closed).
			activeWrits := 0
			for _, wr := range w.Writs {
				if wr.Status == "closed" {
					continue
				}
				activeWrits++
				assignee := wr.Assignee
				if assignee == "" {
					assignee = "unassigned"
				}
				b.WriteString(fmt.Sprintf("- [%s] %s: %s (%s, p%d)\n",
					wr.Status, wr.ID, wr.Title, assignee, wr.Priority))
			}
			if activeWrits > 0 {
				b.WriteString("\n")
			}
		}

		// Merge requests summary.
		if len(w.MergeRequests) > 0 {
			b.WriteString("### Merge Requests\n\n")
			phaseCounts := map[string]int{}
			for _, mr := range w.MergeRequests {
				phaseCounts[mr.Phase]++
			}
			b.WriteString(fmt.Sprintf("Total: %d", len(w.MergeRequests)))
			for _, p := range []string{"ready", "claimed", "merged", "failed", "superseded"} {
				if n, ok := phaseCounts[p]; ok {
					b.WriteString(fmt.Sprintf(", %s: %d", p, n))
				}
			}
			b.WriteString("\n")

			// Detail for non-terminal MRs.
			for _, mr := range w.MergeRequests {
				if mr.Phase == "merged" || mr.Phase == "superseded" {
					continue
				}
				b.WriteString(fmt.Sprintf("- [%s] %s for writ %s (branch: %s, attempts: %d)\n",
					mr.Phase, mr.ID, mr.WritID, mr.Branch, mr.Attempts))
			}
			b.WriteString("\n")
		}

		// Blocked MRs.
		if len(w.BlockedMRs) > 0 {
			b.WriteString("### Blocked Merge Requests\n\n")
			for _, mr := range w.BlockedMRs {
				b.WriteString(fmt.Sprintf("- %s for writ %s — blocked by: %s\n",
					mr.ID, mr.WritID, mr.BlockedBy))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}
