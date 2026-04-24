package trace

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// TraceData holds all collected data for a writ trace.
type TraceData struct {
	// Core writ info (from world DB).
	World string      `json:"world"`
	Writ  *store.Writ `json:"writ"`

	// World DB data.
	History      []store.HistoryEntry     `json:"history"`
	Tokens       []store.TokenSummary     `json:"tokens"`
	MergeRequests []store.MergeRequest    `json:"merge_requests"`
	Dependencies []string                 `json:"dependencies"`  // writs this one depends on
	Dependents   []string                 `json:"dependents"`    // writs waiting on this one
	Labels       []string                 `json:"labels"`

	// Sphere DB data.
	Escalations  []store.Escalation       `json:"escalations"`
	CaravanItems []store.CaravanItem      `json:"caravan_items"`
	Caravans     map[string]*store.Caravan `json:"caravans,omitempty"` // caravan_id → caravan
	ActiveAgents []store.Agent            `json:"active_agents"`

	// Tether data.
	Tethers []TetherInfo `json:"tethers"`

	// Timeline (merged from all sources).
	Timeline []TimelineEvent `json:"timeline"`

	// Cost.
	Cost *CostSummary `json:"cost,omitempty"`

	// Degradation notes (data sources that were unavailable).
	Degradations []string `json:"degradations,omitempty"`
}

// TetherInfo describes a tether file found for the writ.
type TetherInfo struct {
	Agent string `json:"agent"`
	Role  string `json:"role"`
}

// TimelineEvent is a single entry in the chronological timeline.
type TimelineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
}

// CostSummary holds token cost breakdown.
type CostSummary struct {
	Models    []ModelCost `json:"models"`
	Total     float64     `json:"total"`
	CycleTime string     `json:"cycle_time,omitempty"`
}

// ModelCost holds cost for a single model.
type ModelCost struct {
	Model               string  `json:"model"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	ReasoningTokens     int64   `json:"reasoning_tokens"`
	Cost                float64 `json:"cost"`
}

// Options controls what data to collect.
type Options struct {
	World    string // explicit world (empty = scan all)
	NoEvents bool   // skip event log scan
}

// Collect gathers all trace data for a writ ID.
func Collect(writID string, opts Options) (*TraceData, error) {
	td := &TraceData{}

	// Step 1: Resolve world and get writ record.
	world, worldStore, err := resolveWorld(writID, opts.World)
	if err != nil {
		return nil, err
	}
	defer worldStore.Close()
	td.World = world

	writ, err := worldStore.GetWrit(writID)
	if err != nil {
		return nil, fmt.Errorf("writ %s not found: %w", writID, err)
	}
	td.Writ = writ
	td.Labels = writ.Labels

	// Step 2: World database queries.
	td.History, err = worldStore.HistoryForWrit(writID)
	if err != nil {
		return nil, fmt.Errorf("failed to query history: %w", err)
	}

	td.Tokens, err = worldStore.TokensForWrit(writID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tokens: %w", err)
	}

	td.MergeRequests, err = worldStore.ListMergeRequestsByWrit(writID, "")
	if err != nil {
		return nil, fmt.Errorf("failed to query merge requests: %w", err)
	}

	td.Dependencies, err = worldStore.GetDependencies(writID)
	if err != nil {
		return nil, fmt.Errorf("failed to query dependencies: %w", err)
	}

	td.Dependents, err = worldStore.GetDependents(writID)
	if err != nil {
		return nil, fmt.Errorf("failed to query dependents: %w", err)
	}

	// Step 3: Sphere database queries (degrade on failure).
	collectSphereData(td, writID)

	// Step 4: Tether filesystem scan (degrade on failure).
	collectTetherData(td, world, writID)

	// Step 5: Event log scan (optional, best-effort).
	if !opts.NoEvents {
		collectEventData(td, writID)
	}

	// Step 6: Build timeline.
	td.Timeline = buildTimeline(td)

	// Step 7: Compute cost.
	td.Cost = computeCost(td)

	return td, nil
}

// resolveWorld finds the world containing the writ.
// If world is specified, use it directly. Otherwise scan all worlds.
func resolveWorld(writID, world string) (string, *store.WorldStore, error) {
	if world != "" {
		if err := config.RequireWorld(world); err != nil {
			return "", nil, err
		}
		s, err := store.OpenWorld(world)
		if err != nil {
			return "", nil, fmt.Errorf("failed to open world database %q: %w", world, err)
		}
		return world, s, nil
	}

	// Scan all worlds.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return "", nil, fmt.Errorf("failed to open sphere database to scan worlds: %w", err)
	}
	defer sphereStore.Close()

	worlds, err := sphereStore.ListWorlds()
	if err != nil {
		return "", nil, fmt.Errorf("failed to list worlds: %w", err)
	}

	for _, w := range worlds {
		ws, err := store.OpenWorld(w.Name)
		if err != nil {
			continue // skip unavailable worlds
		}
		_, err = ws.GetWrit(writID)
		if err == nil {
			return w.Name, ws, nil
		}
		ws.Close()
	}

	return "", nil, fmt.Errorf("writ %s not found in any world", writID)
}

// collectSphereData queries the sphere database. Degrades on failure.
func collectSphereData(td *TraceData, writID string) {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		td.Degradations = append(td.Degradations, "(sphere unavailable — escalations, caravan data omitted)")
		return
	}
	defer sphereStore.Close()

	// Escalations.
	escalations, err := sphereStore.ListEscalationsBySourceRef("writ:" + writID)
	if err != nil {
		td.Degradations = append(td.Degradations, "(escalation query failed)")
	} else {
		td.Escalations = escalations
	}

	// Caravan items.
	caravanItems, err := sphereStore.GetCaravanItemsForWrit(writID)
	if err != nil {
		td.Degradations = append(td.Degradations, "(caravan query failed)")
	} else {
		td.CaravanItems = caravanItems
		// Resolve caravan names.
		if len(caravanItems) > 0 {
			td.Caravans = make(map[string]*store.Caravan)
			for _, ci := range caravanItems {
				if _, ok := td.Caravans[ci.CaravanID]; !ok {
					caravan, err := sphereStore.GetCaravan(ci.CaravanID)
					if err == nil {
						td.Caravans[ci.CaravanID] = caravan
					}
				}
			}
		}
	}

	// Active agents.
	agents, err := sphereStore.ListAgents("", "")
	if err == nil {
		for _, a := range agents {
			if a.ActiveWrit == writID {
				td.ActiveAgents = append(td.ActiveAgents, a)
			}
		}
	}
}

// collectTetherData walks the filesystem looking for tether files.
func collectTetherData(td *TraceData, world, writID string) {
	roles := []struct {
		dir  string
		role string
	}{
		{"outposts", "outpost"},
		{"envoys", "envoy"},
	}

	for _, r := range roles {
		baseDir := filepath.Join(config.Home(), world, r.dir)
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			continue // directory may not exist
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			tetherPath := filepath.Join(baseDir, entry.Name(), ".tether", writID)
			if _, err := os.Stat(tetherPath); err == nil {
				td.Tethers = append(td.Tethers, TetherInfo{
					Agent: entry.Name(),
					Role:  r.role,
				})
			}
		}
	}

	// Check forge tether dir.
	for _, role := range []string{"forge"} {
		agentDir := config.AgentDir(world, role, role)
		tetherPath := filepath.Join(agentDir, ".tether", writID)
		if _, err := os.Stat(tetherPath); err == nil {
			td.Tethers = append(td.Tethers, TetherInfo{
				Agent: role,
				Role:  role,
			})
		}
	}
}

// feedEvent represents a single parsed event from the JSONL feed.
type feedEvent struct {
	Timestamp time.Time          `json:"ts"`
	Source    string             `json:"source"`
	Type      string             `json:"type"`
	Actor     string             `json:"actor"`
	Payload   map[string]any     `json:"payload"`
}

// collectEventData scans the event log for writ-related events.
func collectEventData(td *TraceData, writID string) {
	feedPath := filepath.Join(config.Home(), ".events.jsonl")
	f, err := os.Open(feedPath)
	if err != nil {
		return // best-effort, no degradation note for missing events
	}
	defer f.Close()

	// Use bufio.Reader + ReadString('\n') so arbitrarily long lines don't
	// cause silent data loss (CF-L5 pattern). bufio.Scanner has a max token
	// size (default 1MB) and silently drops all subsequent lines on overflow.
	br := bufio.NewReader(f)
	for {
		line, readErr := br.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimRight(line, "\n")
			if trimmed != "" {
				// Quick check before full parse.
				if strings.Contains(trimmed, writID) {
					var evt feedEvent
					if err := json.Unmarshal([]byte(trimmed), &evt); err == nil {
						// Check payload for writ_id match.
						if fmt.Sprint(evt.Payload["writ_id"]) == writID || fmt.Sprint(evt.Payload["writ"]) == writID {
							action := evt.Type
							detail := evt.Actor
							if evt.Source != "" && evt.Source != evt.Actor {
								detail = fmt.Sprintf("%s (%s)", evt.Actor, evt.Source)
							}
							td.Timeline = append(td.Timeline, TimelineEvent{
								Timestamp: evt.Timestamp,
								Action:    action,
								Detail:    detail,
							})
						}
					}
				}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				td.Degradations = append(td.Degradations, fmt.Sprintf("event log scan incomplete: %v", readErr))
			}
			break
		}
	}
}

// buildTimeline merges events from all DB sources into chronological order.
func buildTimeline(td *TraceData) []TimelineEvent {
	var events []TimelineEvent

	// Writ creation.
	events = append(events, TimelineEvent{
		Timestamp: td.Writ.CreatedAt,
		Action:    "created",
		Detail:    fmt.Sprintf("by %s", td.Writ.CreatedBy),
	})

	// Writ closure.
	if td.Writ.ClosedAt != nil {
		detail := "closed"
		if td.Writ.CloseReason != "" {
			detail = td.Writ.CloseReason
		}
		events = append(events, TimelineEvent{
			Timestamp: *td.Writ.ClosedAt,
			Action:    "closed",
			Detail:    detail,
		})
	}

	// Agent history.
	for _, h := range td.History {
		switch h.Action {
		case "cast":
			events = append(events, TimelineEvent{
				Timestamp: h.StartedAt,
				Action:    "cast",
				Detail:    fmt.Sprintf("to %s", h.AgentName),
			})
			if h.EndedAt != nil {
				events = append(events, TimelineEvent{
					Timestamp: *h.EndedAt,
					Action:    "resolved",
					Detail:    fmt.Sprintf("by %s", h.AgentName),
				})
			}
		default:
			events = append(events, TimelineEvent{
				Timestamp: h.StartedAt,
				Action:    h.Action,
				Detail:    h.AgentName,
			})
		}
	}

	// Merge requests.
	for _, mr := range td.MergeRequests {
		events = append(events, TimelineEvent{
			Timestamp: mr.CreatedAt,
			Action:    "mr_created",
			Detail:    mr.ID,
		})
		if mr.MergedAt != nil {
			events = append(events, TimelineEvent{
				Timestamp: *mr.MergedAt,
				Action:    "merged",
				Detail:    mr.ID,
			})
		}
		if mr.Phase == "failed" {
			events = append(events, TimelineEvent{
				Timestamp: mr.UpdatedAt,
				Action:    "mr_failed",
				Detail:    mr.ID,
			})
		}
	}

	// Escalations.
	for _, esc := range td.Escalations {
		events = append(events, TimelineEvent{
			Timestamp: esc.CreatedAt,
			Action:    "escalation",
			Detail:    fmt.Sprintf("[%s] %s", esc.Severity, esc.Description),
		})
	}

	// Add any events already gathered from the feed (in collectEventData).
	// These were added directly to td.Timeline; merge them.
	events = append(events, td.Timeline...)

	// Deduplicate events that are within 1 second and have the same action.
	events = deduplicateEvents(events)

	// Sort chronologically.
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	return events
}

// deduplicateEvents removes events from feed that duplicate DB-sourced events.
// If two events have the same action and are within 2 seconds, keep only one.
func deduplicateEvents(events []TimelineEvent) []TimelineEvent {
	if len(events) <= 1 {
		return events
	}

	// Sort first to group by time.
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	var result []TimelineEvent
	for i, e := range events {
		isDup := false
		for j := 0; j < i; j++ {
			if events[j].Action == e.Action &&
				events[j].Detail == e.Detail &&
				absDuration(events[j].Timestamp.Sub(e.Timestamp)) < 2*time.Second {
				isDup = true
				break
			}
		}
		if !isDup {
			result = append(result, e)
		}
	}
	return result
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// computeCost calculates token costs using DB-stored CostUSD.
func computeCost(td *TraceData) *CostSummary {
	if len(td.Tokens) == 0 {
		return nil
	}

	cs := &CostSummary{}
	var totalCost float64

	for _, t := range td.Tokens {
		mc := ModelCost{
			Model:               t.Model,
			InputTokens:         t.InputTokens,
			OutputTokens:        t.OutputTokens,
			CacheReadTokens:     t.CacheReadTokens,
			CacheCreationTokens: t.CacheCreationTokens,
			ReasoningTokens:     t.ReasoningTokens,
		}

		if t.CostUSD != nil {
			mc.Cost = *t.CostUSD
			totalCost += mc.Cost
		}

		cs.Models = append(cs.Models, mc)
	}

	cs.Total = totalCost

	// Compute cycle time: first cast → last merge (or resolve).
	var firstCast, lastEnd time.Time
	for _, h := range td.History {
		if h.Action == "cast" && (firstCast.IsZero() || h.StartedAt.Before(firstCast)) {
			firstCast = h.StartedAt
		}
	}
	for _, mr := range td.MergeRequests {
		if mr.MergedAt != nil && (lastEnd.IsZero() || mr.MergedAt.After(lastEnd)) {
			lastEnd = *mr.MergedAt
		}
	}
	// Fall back to resolve time if no merge.
	if lastEnd.IsZero() {
		for _, h := range td.History {
			if h.EndedAt != nil && (lastEnd.IsZero() || h.EndedAt.After(lastEnd)) {
				lastEnd = *h.EndedAt
			}
		}
	}
	if !firstCast.IsZero() && !lastEnd.IsZero() {
		cs.CycleTime = formatCycleTime(lastEnd.Sub(firstCast))
	}

	return cs
}

// formatCycleTime formats a duration as "Xh Ym" or "Xm" etc.
func formatCycleTime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
