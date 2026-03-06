package status

import (
	"fmt"
	"time"

	"github.com/nevinsm/sol/internal/broker"
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
	result.Consul = GatherConsulInfo()

	// 2b. Check broker.
	result.Broker = GatherBrokerInfo()

	// 3. Check chronicle.
	const chronicleSessionName = "sol-chronicle"
	if checker.Exists(chronicleSessionName) {
		result.Chronicle = ChronicleInfo{Running: true, SessionName: chronicleSessionName}
	}

	// 3b. Check senate.
	const senateSessionName = "sol-senate"
	if checker.Exists(senateSessionName) {
		result.Senate = SenateInfo{Running: true, SessionName: senateSessionName}
	}

	// 4. Gather per-world summaries.
	worlds, err := worldLister.ListWorlds()
	if err == nil {
		for _, w := range worlds {
			summary := gatherWorldSummary(w, sphereStore, checker, worldOpener)
			result.Worlds = append(result.Worlds, summary)
		}
	}

	// 5. Gather active caravans (sphere-wide, non-closed).
	if caravanStore != nil {
		allCaravans, err := caravanStore.ListCaravans("")
		if err == nil {
			// Filter to active (non-closed) caravans.
			var caravans []store.Caravan
			for _, ac := range allCaravans {
				if ac.Status != "closed" {
					caravans = append(caravans, ac)
				}
			}
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
						case st.WorkItemStatus == "closed":
							info.ClosedItems++
						case st.WorkItemStatus == "done":
							info.DoneItems++
						case st.IsDispatched():
							info.DispatchedItems++
						case st.WorkItemStatus == "open" && st.Ready:
							info.ReadyItems++
						}
					}
				}
				info.Phases = computePhaseProgress(items, statuses)
				result.Caravans = append(result.Caravans, info)
			}
		}
	}

	// 6. Compute sphere health.
	result.Health = computeSphereHealth(result)

	return result
}

// GatherConsulInfo reads consul heartbeat state.
// Consul is a Go process (not a tmux session), so heartbeat is the canonical signal.
func GatherConsulInfo() ConsulInfo {
	info := ConsulInfo{}

	hb, err := consul.ReadHeartbeat(config.Home())
	if err == nil && hb != nil {
		info.Running = true
		info.PatrolCount = hb.PatrolCount

		age := time.Since(hb.Timestamp)
		info.HeartbeatAge = FormatDuration(age)
		info.Stale = hb.IsStale(10 * time.Minute)
	}

	return info
}

// GatherBrokerInfo reads token broker heartbeat state.
// The broker is a Go process (not a tmux session), so heartbeat is the canonical signal.
func GatherBrokerInfo() BrokerInfo {
	info := BrokerInfo{}

	hb, err := broker.ReadHeartbeat()
	if err == nil && hb != nil {
		info.Running = true
		info.PatrolCount = hb.PatrolCount
		info.Accounts = hb.Accounts
		info.AgentDirs = hb.AgentDirs

		age := time.Since(hb.Timestamp)
		info.HeartbeatAge = FormatDuration(age)
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
		Sleeping:   config.IsSleeping(w.Name),
	}

	// Sleeping worlds get a distinct health indicator and skip further checks.
	if summary.Sleeping {
		summary.Health = "sleeping"
		return summary
	}

	// Check forge and sentinel sessions.
	forgeSess := dispatch.SessionName(w.Name, "forge")
	summary.Forge = checker.Exists(forgeSess)

	sentinelSess := dispatch.SessionName(w.Name, "sentinel")
	summary.Sentinel = checker.Exists(sentinelSess)

	// Check governor.
	govSess := dispatch.SessionName(w.Name, "governor")
	govSessAlive := checker.Exists(govSess)

	// Agent counts from sphere store, separated by role.
	agents, err := sphereStore.ListAgents(w.Name, "")
	if err == nil {
		for _, a := range agents {
			switch a.Role {
			case "governor":
				summary.Governor = govSessAlive
			case "envoy":
				summary.Envoys++
			case "forge", "sentinel", "consul":
				continue
			default: // "agent"
				summary.Agents++
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

// FormatDuration formats a duration as a compact human-readable string.
func FormatDuration(d time.Duration) string {
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
		if w.Sleeping {
			continue
		}
		if w.Health == "unhealthy" || w.Dead > 0 {
			return "unhealthy"
		}
	}
	if s.Consul.Stale {
		return "degraded"
	}
	return "healthy"
}
