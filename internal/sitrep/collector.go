// Package sitrep collects system state and generates AI-powered situation reports.
package sitrep

import (
	"fmt"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
)

// Scope controls what data is collected.
type Scope struct {
	// Sphere indicates a sphere-wide report (all worlds).
	Sphere bool
	// World is the target world name (empty for sphere scope).
	World string
}

// ForgeMRDetail holds details about a claimed merge request.
type ForgeMRDetail struct {
	ID     string `json:"id"`
	WritID string `json:"writ_id"`
	Title  string `json:"title"`
	Branch string `json:"branch"`
	Age    string `json:"age"`
}

// ForgeEvent holds details about a notable forge event (merge or failure).
type ForgeEvent struct {
	MRID      string    `json:"mr_id"`
	Title     string    `json:"title,omitempty"`
	Branch    string    `json:"branch"`
	Timestamp time.Time `json:"timestamp"`
}

// ForgeStatus holds forge process and queue state for a world.
type ForgeStatus struct {
	Running       bool           `json:"running"`
	Paused        bool           `json:"paused"`
	Merging       bool           `json:"merging"`
	QueueReady    int            `json:"queue_ready"`
	QueueFailed   int            `json:"queue_failed"`
	QueueBlocked  int            `json:"queue_blocked"`
	MergedTotal   int            `json:"merged_total"`
	MergedLast1h  int            `json:"merged_last_1h"`
	MergedLast24h int            `json:"merged_last_24h"`
	ClaimedMR     *ForgeMRDetail `json:"claimed_mr,omitempty"`
	LastMerge     *ForgeEvent    `json:"last_merge,omitempty"`
	LastFailure   *ForgeEvent    `json:"last_failure,omitempty"`
}

// WorldData holds collected data for a single world.
type WorldData struct {
	Name          string               `json:"name"`
	Writs         []store.Writ         `json:"writs"`
	MergeRequests []store.MergeRequest `json:"merge_requests"`
	BlockedMRs    []store.MergeRequest `json:"blocked_merge_requests"`
	MRSummary     map[string]int       `json:"mr_summary,omitempty"`
}

// CollectedData holds all data gathered for a sitrep.
type CollectedData struct {
	Scope            string                               `json:"scope"` // "sphere" or world name
	Agents           []store.Agent                        `json:"agents"`
	Escalations      []store.Escalation                   `json:"escalations,omitempty"`
	Caravans         []store.Caravan                      `json:"caravans"`
	CaravanReadiness map[string][]store.CaravanItemStatus `json:"caravan_readiness,omitempty"`
	ForgeStatuses    map[string]ForgeStatus               `json:"forge_statuses,omitempty"`
	Worlds           []WorldData                          `json:"worlds"`
}

// WorldOpener opens a world store by name.
type WorldOpener func(world string) (*store.WorldStore, error)

// Collect gathers system state from sphere and world stores.
func Collect(sphereStore *store.SphereStore, worldOpener WorldOpener, scope Scope) (*CollectedData, error) {
	data := &CollectedData{}

	// Collect agents.
	if scope.Sphere {
		data.Scope = "sphere"
		agents, err := sphereStore.ListAgents("", "")
		if err != nil {
			return nil, fmt.Errorf("failed to list agents: %w", err)
		}
		data.Agents = agents
	} else {
		data.Scope = scope.World
		agents, err := sphereStore.ListAgents(scope.World, "")
		if err != nil {
			return nil, fmt.Errorf("failed to list agents for world %q: %w", scope.World, err)
		}
		data.Agents = agents
	}

	// Collect open escalations (non-fatal on error).
	escalations, err := sphereStore.ListEscalations("open")
	if err == nil {
		data.Escalations = escalations
	}

	// Collect only actionable caravans (open + drydock).
	openCaravans, err := sphereStore.ListCaravans(store.CaravanOpen)
	if err != nil {
		return nil, fmt.Errorf("failed to list open caravans: %w", err)
	}
	drydockCaravans, err := sphereStore.ListCaravans(store.CaravanDrydock)
	if err != nil {
		return nil, fmt.Errorf("failed to list drydock caravans: %w", err)
	}
	data.Caravans = append(openCaravans, drydockCaravans...)

	// Check readiness for all collected caravans (all are non-closed).
	data.CaravanReadiness = make(map[string][]store.CaravanItemStatus)
	for _, c := range data.Caravans {
		statuses, err := sphereStore.CheckCaravanReadiness(c.ID, worldOpener)
		if err != nil {
			// Non-fatal — skip readiness for this caravan.
			continue
		}
		data.CaravanReadiness[c.ID] = statuses
	}

	// Collect world data and forge statuses.
	data.ForgeStatuses = make(map[string]ForgeStatus)

	if scope.Sphere {
		worlds, err := sphereStore.ListWorlds()
		if err != nil {
			return nil, fmt.Errorf("failed to list worlds: %w", err)
		}
		for _, w := range worlds {
			wd, err := collectWorldData(worldOpener, w.Name)
			if err != nil {
				// Non-fatal — include what we can.
				data.Worlds = append(data.Worlds, WorldData{Name: w.Name})
				continue
			}
			data.Worlds = append(data.Worlds, *wd)
			data.ForgeStatuses[w.Name] = collectForgeStatus(worldOpener, w.Name)
		}
	} else {
		wd, err := collectWorldData(worldOpener, scope.World)
		if err != nil {
			return nil, fmt.Errorf("failed to collect world data for %q: %w", scope.World, err)
		}
		data.Worlds = append(data.Worlds, *wd)
		data.ForgeStatuses[scope.World] = collectForgeStatus(worldOpener, scope.World)
	}

	return data, nil
}

func collectWorldData(worldOpener WorldOpener, world string) (*WorldData, error) {
	ws, err := worldOpener(world)
	if err != nil {
		return nil, fmt.Errorf("failed to open world %q: %w", world, err)
	}
	defer ws.Close()

	wd := &WorldData{Name: world}

	writs, err := ws.ListWrits(store.ListFilters{
		Statuses: []string{store.WritOpen, store.WritTethered, store.WritWorking, store.WritResolve, store.WritDone},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list writs for world %q: %w", world, err)
	}
	wd.Writs = writs

	// Collect only actionable (non-terminal) merge requests for detail.
	for _, phase := range []string{store.MRReady, store.MRClaimed, store.MRFailed} {
		phaseMRs, err := ws.ListMergeRequests(phase)
		if err != nil {
			return nil, fmt.Errorf("failed to list %s merge requests for world %q: %w", phase, world, err)
		}
		wd.MergeRequests = append(wd.MergeRequests, phaseMRs...)
	}

	// Compute MR summary counts by phase for scale context.
	allMRs, err := ws.ListMergeRequests("")
	if err != nil {
		return nil, fmt.Errorf("failed to list all merge requests for world %q: %w", world, err)
	}
	wd.MRSummary = make(map[string]int)
	wd.MRSummary["total"] = len(allMRs)
	for _, mr := range allMRs {
		wd.MRSummary[mr.Phase]++
	}

	blockedMRs, err := ws.ListBlockedMergeRequests()
	if err != nil {
		return nil, fmt.Errorf("failed to list blocked merge requests for world %q: %w", world, err)
	}
	wd.BlockedMRs = blockedMRs

	return wd, nil
}

// collectForgeStatus gathers forge process state and MR-derived queue/velocity data.
func collectForgeStatus(worldOpener WorldOpener, world string) ForgeStatus {
	now := time.Now()
	fs := ForgeStatus{}

	// Process liveness.
	pid := forge.ReadPID(world)
	fs.Running = pid > 0 && forge.IsRunning(pid)

	// Pause state.
	fs.Paused = forge.IsForgePaused(world)

	// Merge session active check.
	mergeSessName := config.SessionName(world, "forge-merge")
	mgr := session.New()
	fs.Merging = mgr.Exists(mergeSessName)

	// MR-derived data from the world store.
	ws, err := worldOpener(world)
	if err != nil {
		return fs
	}
	defer ws.Close()

	allMRs, err := ws.ListMergeRequests("")
	if err != nil {
		return fs
	}

	for _, mr := range allMRs {
		switch mr.Phase {
		case store.MRReady:
			if mr.BlockedBy != "" {
				fs.QueueBlocked++
			} else {
				fs.QueueReady++
			}
		case store.MRClaimed:
			// Track claimed MR details.
			detail := &ForgeMRDetail{
				ID:     mr.ID,
				Branch: mr.Branch,
				WritID: mr.WritID,
			}
			if mr.ClaimedAt != nil {
				detail.Age = time.Since(*mr.ClaimedAt).Truncate(time.Second).String()
			}
			// Look up writ title.
			if item, err := ws.GetWrit(mr.WritID); err == nil {
				detail.Title = item.Title
			} else {
				detail.Title = "(unknown)"
			}
			fs.ClaimedMR = detail
		case store.MRFailed:
			fs.QueueFailed++
			// Track the most recent failure (by updated_at).
			if fs.LastFailure == nil || mr.UpdatedAt.After(fs.LastFailure.Timestamp) {
				fs.LastFailure = &ForgeEvent{
					MRID:      mr.ID,
					Branch:    mr.Branch,
					Timestamp: mr.UpdatedAt,
				}
				if item, err := ws.GetWrit(mr.WritID); err == nil {
					fs.LastFailure.Title = item.Title
				}
			}
		case store.MRMerged:
			fs.MergedTotal++
			if mr.MergedAt != nil {
				if now.Sub(*mr.MergedAt) <= time.Hour {
					fs.MergedLast1h++
				}
				if now.Sub(*mr.MergedAt) <= 24*time.Hour {
					fs.MergedLast24h++
				}
				// Track the most recent merge (by merged_at).
				if fs.LastMerge == nil || mr.MergedAt.After(fs.LastMerge.Timestamp) {
					fs.LastMerge = &ForgeEvent{
						MRID:      mr.ID,
						Branch:    mr.Branch,
						Timestamp: *mr.MergedAt,
					}
					if item, err := ws.GetWrit(mr.WritID); err == nil {
						fs.LastMerge.Title = item.Title
					}
				}
			}
		}
	}

	return fs
}
