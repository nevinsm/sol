package status

import (
	"fmt"
	"time"

	"github.com/nevinsm/sol/internal/broker"
	"github.com/nevinsm/sol/internal/chronicle"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/store"
)

// GatherSphere collects runtime state for the entire sphere.
//
// The function degrades gracefully: if any per-world query fails,
// that world gets partial data rather than causing the whole gather
// to fail. GatherSphere never returns an error.
func GatherSphere(sphereStore SphereStore, worldLister WorldLister,
	checker SessionChecker,
	worldOpener func(string) (*store.WorldStore, error),
	caravanStore CaravanStore,
	escalationLister ...EscalationLister) *SphereStatus {

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

	// 3. Check chronicle: PID + heartbeat.
	result.Chronicle = GatherChronicleInfo()

	// 3a. Check ledger: PID + heartbeat.
	result.Ledger = GatherLedgerInfo()

	// 3b. Check chancellor.
	const chancellorSessionName = "sol-chancellor"
	if checker.Exists(chancellorSessionName) {
		result.Chancellor = ChancellorInfo{Running: true, SessionName: chancellorSessionName}
	}

	// 4. Gather per-world summaries.
	worlds, err := worldLister.ListWorlds()
	if err == nil {
		for _, w := range worlds {
			summary := gatherWorldSummary(w, sphereStore, checker, worldOpener)
			result.Worlds = append(result.Worlds, summary)
		}
	}

	// 4b. Gather token data across all worlds (24h rolling window).
	if worldOpener != nil && len(worlds) > 0 {
		since := time.Now().Add(-24 * time.Hour)
		for _, w := range worlds {
			ws, err := worldOpener(w.Name)
			if err != nil {
				continue
			}
			summaries, tErr := ws.TokensSince(since)
			if tErr == nil {
				for _, ts := range summaries {
					result.Tokens.InputTokens += ts.InputTokens
					result.Tokens.OutputTokens += ts.OutputTokens
					result.Tokens.CacheTokens += ts.CacheReadTokens + ts.CacheCreationTokens
				}
			}
			agents, _, tErr := ws.WorldTokenMetaSince(since)
			if tErr == nil {
				result.Tokens.AgentCount += agents
			}
			ws.Close()
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
				statuses, _ := caravanStore.CheckCaravanReadiness(c.ID, worldOpener)
				result.Caravans = append(result.Caravans, buildCaravanInfo(c, items, statuses, worldOpener))
			}
		}
	}

	// 6. Gather escalation summary (non-fatal if fails — DEGRADE).
	if len(escalationLister) > 0 && escalationLister[0] != nil {
		if escs, err := escalationLister[0].ListOpenEscalations(); err == nil && len(escs) > 0 {
			summary := &EscalationSummary{
				Total:      len(escs),
				BySeverity: make(map[string]int),
			}
			for _, esc := range escs {
				summary.BySeverity[esc.Severity]++
			}
			result.Escalations = summary
		}
	}

	// 7. Compute sphere health.
	result.Health = computeSphereHealth(result)

	return result
}

// GatherConsulInfo reads consul PID + heartbeat state.
// Consul is a Go process (not a tmux session), so PID liveness is the canonical
// running signal. Heartbeat data is populated regardless for diagnostic value.
func GatherConsulInfo() ConsulInfo {
	info := ConsulInfo{}

	pid := prefect.ReadDaemonPID("consul")
	if pid > 0 && prefect.IsRunning(pid) {
		info.Running = true
	}

	hb, err := consul.ReadHeartbeat(config.Home())
	if err == nil && hb != nil {
		info.PatrolCount = hb.PatrolCount

		age := time.Since(hb.Timestamp)
		info.HeartbeatAge = FormatDuration(age)
		info.Stale = hb.IsStale(10 * time.Minute)
	}

	return info
}

// GatherChronicleInfo reads chronicle PID + heartbeat state.
// Chronicle is a Go process (not a tmux session), supervised via PID + heartbeat.
func GatherChronicleInfo() ChronicleInfo {
	info := ChronicleInfo{}

	pid := readChroniclePID()
	if pid > 0 && prefect.IsRunning(pid) {
		info.Running = true
		info.PID = pid
	}

	hb, err := chronicle.ReadHeartbeat()
	if err == nil && hb != nil {
		info.EventsProcessed = hb.EventsProcessed

		age := time.Since(hb.Timestamp)
		info.HeartbeatAge = FormatDuration(age)
		info.Stale = hb.IsStale(5 * time.Minute)
	}

	return info
}

// GatherBrokerInfo reads broker PID + heartbeat state.
// The broker is a Go process (not a tmux session), so PID liveness is the canonical
// running signal. Heartbeat data is populated regardless for diagnostic value.
func GatherBrokerInfo() BrokerInfo {
	info := BrokerInfo{}

	pid := prefect.ReadDaemonPID("broker")
	if pid > 0 && prefect.IsRunning(pid) {
		info.Running = true
	}

	hb, err := broker.ReadHeartbeat()
	if err == nil && hb != nil {
		info.PatrolCount = hb.PatrolCount

		age := time.Since(hb.Timestamp)
		info.HeartbeatAge = FormatDuration(age)
		info.Stale = hb.IsStale(10 * time.Minute)

		if hb.ProviderHealth != "" {
			info.ProviderHealth = string(hb.ProviderHealth)
		}

		info.TokenHealth = hb.TokenHealth
	}

	return info
}

// gatherWorldSummary builds a condensed status for a single world.
func gatherWorldSummary(w store.World, sphereStore SphereStore,
	checker SessionChecker,
	worldOpener func(string) (*store.WorldStore, error)) WorldSummary {

	summary := WorldSummary{
		Name:       w.Name,
		SourceRepo: w.SourceRepo,
		Sleeping:   config.IsSleeping(w.Name),
	}

	// Load world config for capacity (non-fatal if fails — DEGRADE).
	if worldCfg, err := config.LoadWorldConfig(w.Name); err == nil {
		summary.Capacity = worldCfg.Agents.Capacity
	}

	// Sleeping worlds: still count active agents/envoys, but skip
	// forge/sentinel/governor/MR checks.
	if summary.Sleeping {
		summary.Health = "sleeping"

		// Count agents and envoys that may still be winding down.
		agents, err := sphereStore.ListAgents(w.Name, "")
		if err == nil {
			for _, a := range agents {
				switch a.Role {
				case "envoy":
					summary.Envoys++
				case "forge", "sentinel", "consul", "governor":
					continue
				default: // "outpost"
					summary.Agents++
					switch a.State {
					case "working":
						summary.Working++
						sessName := config.SessionName(w.Name, a.Name)
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
		return summary
	}

	// Check forge via PID file.
	forgePID := forge.ReadPID(w.Name)
	summary.Forge = forgePID > 0 && forge.IsRunning(forgePID)

	// Check sentinel via PID + heartbeat (sentinel is a direct Go process).
	sentinelPID := sentinel.ReadPID(w.Name)
	summary.Sentinel = sentinelPID > 0 && prefect.IsRunning(sentinelPID)

	// Check governor.
	govSess := config.SessionName(w.Name, "governor")
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
			default: // "outpost"
				summary.Agents++
				switch a.State {
				case "working":
					summary.Working++
					sessName := config.SessionName(w.Name, a.Name)
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

// computeSphereHealth derives sphere-wide health by aggregating component states.
//
// This is distinct from WorldStatus.Health() in status.go which computes
// health for a single world. Sphere health considers:
//   - Prefect running (sphere-level orchestrator — if down, no sessions respawn)
//   - Any world unhealthy or having dead sessions (propagates upward)
//   - Consul staleness (sphere-level patrol — stale means tether reaping is delayed)
//   - Provider health (degraded/down broker signals AI provider issues)
//
// Sleeping worlds are excluded from health computation since they are
// intentionally inactive and their state is not actionable.
//
// Returns "healthy", "degraded", or "unhealthy".
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
	// Provider health affects sphere health.
	if s.Broker.ProviderHealth == "down" || s.Broker.ProviderHealth == "degraded" {
		return "degraded"
	}
	return "healthy"
}
