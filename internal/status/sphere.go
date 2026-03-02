package status

import (
	"fmt"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/store"
)

// GatherSphere collects runtime state for the entire sphere.
//
// The function degrades gracefully: if any per-world query fails,
// that world gets partial data rather than causing the whole gather
// to fail. GatherSphere never returns an error.
func GatherSphere(sphereStore SphereStore, worldLister WorldLister,
	checker SessionChecker,
	worldOpener func(string) (*store.Store, error),
	caravanStore CaravanStore) *SphereStatus {

	result := &SphereStatus{
		SOLHome: config.Home(),
	}

	// 1. Check prefect.
	pid, err := prefect.ReadPID()
	if err == nil && pid != 0 && prefect.IsRunning(pid) {
		result.Prefect = PrefectInfo{Running: true, PID: pid}
	}

	// 2. Check consul.
	result.Consul = gatherConsulInfo()

	// 3. Check chronicle.
	const chronicleSessionName = "sol-chronicle"
	if checker.Exists(chronicleSessionName) {
		result.Chronicle = ChronicleInfo{Running: true, SessionName: chronicleSessionName}
	}

	// 4. Gather per-world summaries.
	worlds, err := worldLister.ListWorlds()
	if err == nil {
		for _, w := range worlds {
			summary := gatherWorldSummary(w, sphereStore, checker, worldOpener)
			result.Worlds = append(result.Worlds, summary)
		}
	}

	// 5. Gather open caravans (sphere-wide).
	if caravanStore != nil {
		caravans, err := caravanStore.ListCaravans("open")
		if err == nil {
			for _, c := range caravans {
				items, err := caravanStore.ListCaravanItems(c.ID)
				if err != nil {
					continue
				}
				info := CaravanInfo{
					ID:         c.ID,
					Name:       c.Name,
					Status:     c.Status,
					TotalItems: len(items),
				}
				statuses, err := caravanStore.CheckCaravanReadiness(c.ID, worldOpener)
				if err == nil {
					for _, st := range statuses {
						switch {
						case st.WorkItemStatus == "done" || st.WorkItemStatus == "closed":
							info.DoneItems++
						case st.WorkItemStatus == "open" && st.Ready:
							info.ReadyItems++
						}
					}
				}
				result.Caravans = append(result.Caravans, info)
			}
		}
	}

	// 6. Compute sphere health.
	result.Health = computeSphereHealth(result)

	return result
}

// gatherConsulInfo reads consul heartbeat state.
// Consul is a Go process (not a tmux session), so heartbeat is the canonical signal.
func gatherConsulInfo() ConsulInfo {
	info := ConsulInfo{}

	hb, err := consul.ReadHeartbeat(config.Home())
	if err == nil && hb != nil {
		info.Running = true
		info.PatrolCount = hb.PatrolCount

		age := time.Since(hb.Timestamp)
		info.HeartbeatAge = formatDuration(age)
		info.Stale = hb.IsStale(10 * time.Minute)
	}

	return info
}

// gatherWorldSummary builds a condensed status for a single world.
func gatherWorldSummary(w store.World, sphereStore SphereStore,
	checker SessionChecker,
	worldOpener func(string) (*store.Store, error)) WorldSummary {

	summary := WorldSummary{
		Name:       w.Name,
		SourceRepo: w.SourceRepo,
	}

	// Check forge and sentinel sessions.
	forgeSess := dispatch.SessionName(w.Name, "forge")
	summary.Forge = checker.Exists(forgeSess)

	sentinelSess := dispatch.SessionName(w.Name, "sentinel")
	summary.Sentinel = checker.Exists(sentinelSess)

	// Agent counts from sphere store.
	agents, err := sphereStore.ListAgents(w.Name, "")
	if err == nil {
		summary.Agents = len(agents)
		for _, a := range agents {
			switch a.State {
			case "working":
				summary.Working++
				sessName := dispatch.SessionName(w.Name, a.Name)
				if !checker.Exists(sessName) {
					summary.Dead++
				}
			case "idle":
				summary.Idle++
			case "stalled":
				summary.Stalled++
			}
		}
	}

	// Open world store for MR counts (non-fatal if fails).
	ws, err := worldOpener(w.Name)
	if err != nil {
		summary.Health = "unknown"
		return summary
	}
	defer ws.Close()

	// Get merge request counts.
	mrs, err := ws.ListMergeRequests("")
	if err == nil {
		for _, mr := range mrs {
			switch mr.Phase {
			case "ready":
				summary.MRReady++
			case "failed":
				summary.MRFailed++
			}
		}
	}

	summary.Health = "healthy"
	if summary.MRFailed > 0 || summary.Dead > 0 {
		summary.Health = "unhealthy"
	}

	return summary
}

// formatDuration formats a duration as a compact human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// computeSphereHealth derives overall sphere health from component states.
func computeSphereHealth(s *SphereStatus) string {
	if !s.Prefect.Running {
		return "degraded"
	}
	for _, w := range s.Worlds {
		if w.Health == "unhealthy" || w.Dead > 0 {
			return "unhealthy"
		}
	}
	if s.Consul.Stale {
		return "degraded"
	}
	return "healthy"
}
