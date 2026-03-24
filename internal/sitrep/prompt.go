package sitrep

import (
	_ "embed"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
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

	// Escalations (high-priority, placed near the top).
	if len(data.Escalations) > 0 {
		b.WriteString("## Escalations\n\n")
		for _, e := range data.Escalations {
			age := formatAge(e.CreatedAt)
			ref := e.SourceRef
			if ref == "" {
				ref = e.Source
			}
			b.WriteString(fmt.Sprintf("- [%s] %s — %s (source: %s, age: %s)\n",
				e.Severity, e.ID, e.Description, ref, age))
		}
		b.WriteString("\n")
	}

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

	// Forge status per world (placed before world detail sections).
	for _, w := range data.Worlds {
		fs, ok := data.ForgeStatuses[w.Name]
		if !ok {
			continue
		}
		b.WriteString(fmt.Sprintf("## Forge: %s\n\n", w.Name))

		// Process state.
		state := "stopped"
		if fs.Running {
			if fs.Paused {
				state = "paused"
			} else {
				state = "running"
			}
		}
		mergeState := "inactive"
		if fs.Merging {
			mergeState = "active"
		}
		b.WriteString(fmt.Sprintf("Process: %s, merging: %s\n", state, mergeState))

		// Queue summary.
		b.WriteString(fmt.Sprintf("Queue: %d ready, %d failed, %d blocked\n", fs.QueueReady, fs.QueueFailed, fs.QueueBlocked))

		// Velocity.
		b.WriteString(fmt.Sprintf("Velocity: %d merged in last hour, %d in last 24h (%d total)\n", fs.MergedLast1h, fs.MergedLast24h, fs.MergedTotal))

		// Current claim.
		if fs.ClaimedMR != nil {
			b.WriteString(fmt.Sprintf("Claimed: %s — %s (writ %s, age: %s)\n", fs.ClaimedMR.ID, fs.ClaimedMR.Title, fs.ClaimedMR.WritID, fs.ClaimedMR.Age))
		}

		// Last merge.
		if fs.LastMerge != nil {
			ago := formatRelativeTime(fs.LastMerge.Timestamp)
			title := fs.LastMerge.Title
			if title == "" {
				title = fs.LastMerge.Branch
			}
			b.WriteString(fmt.Sprintf("Last merge: %s — %s (%s ago)\n", fs.LastMerge.MRID, title, ago))
		}

		// Last failure.
		if fs.LastFailure != nil {
			ago := formatRelativeTime(fs.LastFailure.Timestamp)
			title := fs.LastFailure.Title
			if title == "" {
				title = fs.LastFailure.Branch
			}
			b.WriteString(fmt.Sprintf("Last failure: %s — %s (%s ago)\n", fs.LastFailure.MRID, title, ago))
		}

		b.WriteString("\n")
	}

	// World data.
	for _, w := range data.Worlds {
		b.WriteString(fmt.Sprintf("## World: %s\n\n", w.Name))

		// Writs summary.
		if len(w.Writs) == 0 {
			b.WriteString("No writs.\n\n")
		} else {
			statusCounts := map[store.WritStatus]int{}
			for _, wr := range w.Writs {
				statusCounts[wr.Status]++
			}
			b.WriteString(fmt.Sprintf("Writs: %d total", len(w.Writs)))
			for _, s := range []store.WritStatus{store.WritOpen, store.WritTethered, store.WritDone, store.WritClosed} {
				if n, ok := statusCounts[s]; ok {
					b.WriteString(fmt.Sprintf(", %s: %d", s, n))
				}
			}
			b.WriteString("\n")

			// Detail for active writs (non-closed).
			activeWrits := 0
			for _, wr := range w.Writs {
				if wr.Status == store.WritClosed {
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
			phaseCounts := map[store.MRPhase]int{}
			for _, mr := range w.MergeRequests {
				phaseCounts[mr.Phase]++
			}
			b.WriteString(fmt.Sprintf("Total: %d", len(w.MergeRequests)))
			for _, p := range []store.MRPhase{store.MRReady, store.MRClaimed, store.MRMerged, store.MRFailed, store.MRSuperseded} {
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

// formatRelativeTime returns a human-readable duration since the given timestamp.
func formatRelativeTime(t time.Time) string {
	d := time.Since(t).Truncate(time.Second)
	if d < time.Minute {
		return d.String()
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	if h == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd%dh", days, h)
}

// formatAge returns a human-readable duration since the given time.
func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = 0
	}

	hours := d.Hours()
	if hours < 1 {
		return fmt.Sprintf("%dm", int(math.Max(1, d.Minutes())))
	}
	if hours < 24 {
		return fmt.Sprintf("%dh", int(hours))
	}
	days := int(hours / 24)
	return fmt.Sprintf("%dd", days)
}
