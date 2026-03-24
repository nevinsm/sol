// Package sitrep collects system state and generates AI-powered situation reports.
package sitrep

import (
	"fmt"

	"github.com/nevinsm/sol/internal/store"
)

// Scope controls what data is collected.
type Scope struct {
	// Sphere indicates a sphere-wide report (all worlds).
	Sphere bool
	// World is the target world name (empty for sphere scope).
	World string
}

// WorldData holds collected data for a single world.
type WorldData struct {
	Name            string               `json:"name"`
	Writs           []store.Writ         `json:"writs"`
	MergeRequests   []store.MergeRequest `json:"merge_requests"`
	BlockedMRs      []store.MergeRequest `json:"blocked_merge_requests"`
	MRSummary       map[string]int       `json:"mr_summary,omitempty"`
}

// CollectedData holds all data gathered for a sitrep.
type CollectedData struct {
	Scope    string                    `json:"scope"` // "sphere" or world name
	Agents   []store.Agent             `json:"agents"`
	Caravans []store.Caravan           `json:"caravans"`
	CaravanReadiness map[string][]store.CaravanItemStatus `json:"caravan_readiness,omitempty"`
	Worlds   []WorldData               `json:"worlds"`
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

	// Collect world data.
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
		}
	} else {
		wd, err := collectWorldData(worldOpener, scope.World)
		if err != nil {
			return nil, fmt.Errorf("failed to collect world data for %q: %w", scope.World, err)
		}
		data.Worlds = append(data.Worlds, *wd)
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
