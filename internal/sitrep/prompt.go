package sitrep

import (
	_ "embed"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
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
			age := relativeAge(e.CreatedAt)
			ref := e.SourceRef
			if ref == "" {
				ref = e.Source
			}
			b.WriteString(fmt.Sprintf("- [%s] %s — %s (source: %s, opened %s)\n",
				e.Severity, e.ID, e.Description, ref, age))
		}
		b.WriteString("\n")
	}

	// Dispatchable section — supply (idle agents) paired with demand (ready writs).
	{
		// Count idle outpost agents.
		var idleAgents []store.Agent
		for _, a := range data.Agents {
			if a.State == "idle" && a.Role == "outpost" {
				idleAgents = append(idleAgents, a)
			}
		}

		// Collect all ready-to-dispatch writs across worlds.
		type readyWrit struct {
			store.Writ
			World       string
			CaravanName string
			CaravanPhase int
		}
		var readyWrits []readyWrit
		for _, w := range data.Worlds {
			for _, wr := range w.ReadyToDispatch {
				rw := readyWrit{Writ: wr, World: w.Name}
				readyWrits = append(readyWrits, rw)
			}
		}

		// Cross-reference with caravan readiness for annotations.
		// Build a lookup: writID → (caravanName, phase).
		type caravanRef struct {
			Name  string
			Phase int
		}
		writCaravan := make(map[string]caravanRef)
		if data.CaravanReadiness != nil {
			// Build caravan ID → name lookup.
			caravanNames := make(map[string]string)
			for _, c := range data.Caravans {
				caravanNames[c.ID] = c.Name
			}
			for caravanID, statuses := range data.CaravanReadiness {
				for _, s := range statuses {
					if s.Ready {
						name := caravanNames[caravanID]
						if name == "" {
							name = caravanID
						}
						writCaravan[s.WritID] = caravanRef{Name: name, Phase: s.Phase}
					}
				}
			}
		}

		// Annotate ready writs with caravan context.
		for i, rw := range readyWrits {
			if ref, ok := writCaravan[rw.ID]; ok {
				readyWrits[i].CaravanName = ref.Name
				readyWrits[i].CaravanPhase = ref.Phase
			}
		}

		// Only emit section if there's something to show.
		if len(idleAgents) > 0 || len(readyWrits) > 0 {
			b.WriteString("## Dispatchable\n\n")

			// Supply/demand summary.
			idleStr := fmt.Sprintf("%d idle outpost agents", len(idleAgents))
			if len(idleAgents) == 0 {
				idleStr = "no idle agents"
			} else if len(idleAgents) == 1 {
				idleStr = "1 idle outpost agent"
			}
			writStr := fmt.Sprintf("%d writs ready for dispatch", len(readyWrits))
			if len(readyWrits) == 0 {
				writStr = "no writs ready"
			} else if len(readyWrits) == 1 {
				writStr = "1 writ ready for dispatch"
			}
			b.WriteString(fmt.Sprintf("%s, %s\n\n", idleStr, writStr))

			// List idle agents.
			if len(idleAgents) > 0 {
				b.WriteString("Idle agents: ")
				names := make([]string, len(idleAgents))
				for i, a := range idleAgents {
					names[i] = a.Name
				}
				b.WriteString(strings.Join(names, ", "))
				b.WriteString("\n\n")
			}

			// List ready writs.
			if len(readyWrits) > 0 {
				b.WriteString("Ready writs:\n")
				for _, rw := range readyWrits {
					line := fmt.Sprintf("- %s — %s [%s]", rw.ID, rw.Title, rw.World)
					if rw.CaravanName != "" {
						line += fmt.Sprintf(" (Phase %d, %s caravan)", rw.CaravanPhase, rw.CaravanName)
					}
					b.WriteString(line + "\n")
				}
				b.WriteString("\n")
			}
		}
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
			age := relativeAge(a.UpdatedAt)
			b.WriteString(fmt.Sprintf("- %s [%s/%s]: %s (last updated %s) — writ: %s\n",
				a.Name, a.World, a.Role, a.State, age, writ))
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

		// Build a caravan ID → name lookup for dependency display.
		caravanNames := make(map[string]string)
		for _, c := range data.Caravans {
			caravanNames[c.ID] = c.Name
		}

		// Build writ ID → title lookup from all worlds.
		writTitles := make(map[string]string)
		for _, w := range data.Worlds {
			for _, wr := range w.Writs {
				writTitles[wr.ID] = wr.Title
			}
		}

		// Detail for non-closed caravans.
		for _, c := range data.Caravans {
			if c.Status == "closed" {
				continue
			}
			b.WriteString(fmt.Sprintf("### %s (%s) — %s\n", c.Name, c.ID, c.Status))

			// Show caravan dependencies if present.
			if unsatisfied, ok := data.CaravanUnsatisfiedDeps[c.ID]; ok && len(unsatisfied) > 0 {
				var names []string
				for _, depID := range unsatisfied {
					if name, ok := caravanNames[depID]; ok {
						names = append(names, name)
					} else {
						names = append(names, depID)
					}
				}
				b.WriteString(fmt.Sprintf("Blocked by: %s (unsatisfied)\n", strings.Join(names, ", ")))
			} else if deps, ok := data.CaravanDeps[c.ID]; ok && len(deps) > 0 {
				var names []string
				for _, depID := range deps {
					if name, ok := caravanNames[depID]; ok {
						names = append(names, name)
					} else {
						names = append(names, depID)
					}
				}
				b.WriteString(fmt.Sprintf("Dependencies: %s (all satisfied)\n", strings.Join(names, ", ")))
			}

			// Phase-level breakdown.
			if statuses, ok := data.CaravanReadiness[c.ID]; ok && len(statuses) > 0 {
				// Group items by phase.
				phaseItems := make(map[int][]store.CaravanItemStatus)
				var phases []int
				for _, s := range statuses {
					if _, exists := phaseItems[s.Phase]; !exists {
						phases = append(phases, s.Phase)
					}
					phaseItems[s.Phase] = append(phaseItems[s.Phase], s)
				}
				sort.Ints(phases)

				for _, phase := range phases {
					items := phaseItems[phase]
					var closedCount, doneCount, readyCount, activeCount, blockedCount int
					var readyWritIDs, failedWritIDs []string
					for _, s := range items {
						switch {
						case s.WritStatus == "closed":
							closedCount++
						case s.WritStatus == "done":
							doneCount++
						case s.Ready:
							readyCount++
							readyWritIDs = append(readyWritIDs, s.WritID)
						case s.IsDispatched():
							activeCount++
						default:
							blockedCount++
							if s.WritStatus == "failed" {
								failedWritIDs = append(failedWritIDs, s.WritID)
							}
						}
					}

					b.WriteString(fmt.Sprintf("Phase %d (%d items): %d closed, %d done, %d ready, %d active, %d blocked\n",
						phase, len(items), closedCount, doneCount, readyCount, activeCount, blockedCount))

					// List ready writs by title.
					for _, wid := range readyWritIDs {
						title := writTitles[wid]
						if title == "" {
							title = wid
						}
						b.WriteString(fmt.Sprintf("  ready: %s (%s)\n", title, wid))
					}
					// List failed writs by title.
					for _, wid := range failedWritIDs {
						title := writTitles[wid]
						if title == "" {
							title = wid
						}
						b.WriteString(fmt.Sprintf("  failed: %s (%s)\n", title, wid))
					}
				}
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
			age := relativeAge(fs.LastMerge.Timestamp)
			title := fs.LastMerge.Title
			if title == "" {
				title = fs.LastMerge.Branch
			}
			b.WriteString(fmt.Sprintf("Last merge: %s — %s (%s)\n", fs.LastMerge.MRID, title, age))
		}

		// Last failure.
		if fs.LastFailure != nil {
			age := relativeAge(fs.LastFailure.Timestamp)
			title := fs.LastFailure.Title
			if title == "" {
				title = fs.LastFailure.Branch
			}
			b.WriteString(fmt.Sprintf("Last failure: %s — %s (%s)\n", fs.LastFailure.MRID, title, age))
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
				age := relativeAge(wr.UpdatedAt)
				b.WriteString(fmt.Sprintf("- [%s] %s: %s (%s, p%d, updated %s)\n",
					wr.Status, wr.ID, wr.Title, assignee, wr.Priority, age))
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
				var ageLabel string
				switch mr.Phase {
				case "claimed":
					if mr.ClaimedAt != nil {
						ageLabel = fmt.Sprintf(", claimed %s", relativeAge(*mr.ClaimedAt))
					} else {
						ageLabel = fmt.Sprintf(", claimed %s", relativeAge(mr.UpdatedAt))
					}
				case "failed":
					ageLabel = fmt.Sprintf(", failed %s", relativeAge(mr.UpdatedAt))
				case "ready":
					ageLabel = fmt.Sprintf(", created %s", relativeAge(mr.CreatedAt))
				}
				b.WriteString(fmt.Sprintf("- [%s] %s for writ %s (branch: %s, attempts: %d%s)\n",
					mr.Phase, mr.ID, mr.WritID, mr.Branch, mr.Attempts, ageLabel))
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

// relativeAge returns a concise human-readable age string for the given time.
// It picks the most appropriate unit:
//   - < 1 minute: "just now"
//   - < 1 hour: "Xm ago"
//   - < 24 hours: "Xh ago"
//   - >= 24 hours: "Xd ago"
func relativeAge(t time.Time) string {
	return RelativeAgeSince(t, time.Now())
}

// RelativeAgeSince computes relative age against a given reference time (exported for testability).
func RelativeAgeSince(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	days := int(d.Hours()) / 24
	return fmt.Sprintf("%dd ago", days)
}
