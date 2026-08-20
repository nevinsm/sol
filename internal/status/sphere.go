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

// TrackingOpener wraps a raw world-store opener and memoizes the result
// (store or error) for each world, so repeated lookups for the same world
// within one logical operation open the underlying database at most once.
//
// Stores returned by Open are owned by the TrackingOpener, not by the
// callee — callees must NEVER close them. The opener's owner calls
// CloseAll once every lookup that might reuse a world is finished (e.g.
// via defer in a one-shot CLI command) to release the underlying
// connections. TrackingOpener is not safe for concurrent use; it is meant
// to back a single sequential gather (or one CLI command's lifetime), not
// to be shared across goroutines.
type TrackingOpener struct {
	raw    func(string) (*store.WorldStore, error)
	opened map[string]trackedOpen
}

// trackedOpen memoizes both success and failure so a world that fails to
// open is not retried on every subsequent lookup within the same call.
type trackedOpen struct {
	store *store.WorldStore
	err   error
}

// NewTrackingOpener wraps raw with per-world memoization. raw may be nil,
// in which case Open always returns an error.
func NewTrackingOpener(raw func(string) (*store.WorldStore, error)) *TrackingOpener {
	return &TrackingOpener{raw: raw, opened: make(map[string]trackedOpen)}
}

// Open returns the memoized store for world, opening it via the wrapped
// opener on first use. The returned store must NOT be closed by the
// caller — call CloseAll once all lookups are complete.
func (t *TrackingOpener) Open(world string) (*store.WorldStore, error) {
	if e, ok := t.opened[world]; ok {
		return e.store, e.err
	}
	if t.raw == nil {
		err := fmt.Errorf("trackingOpener: no opener configured")
		t.opened[world] = trackedOpen{err: err}
		return nil, err
	}
	ws, err := t.raw(world)
	t.opened[world] = trackedOpen{store: ws, err: err}
	return ws, err
}

// CloseAll closes every store opened via Open and clears the cache.
func (t *TrackingOpener) CloseAll() {
	for world, e := range t.opened {
		if e.store != nil {
			e.store.Close()
		}
		delete(t.opened, world)
	}
}

// GatherSphere collects runtime state for the entire sphere.
//
// The function degrades gracefully: if any per-world query fails,
// that world gets partial data rather than causing the whole gather
// to fail. GatherSphere never returns an error.
//
// worldOpener is used to look up world stores for per-world summaries,
// token totals, and caravan writ-title lookups; within one GatherSphere
// call each world is opened at most once via worldOpener regardless of
// how many of those steps need it (see TrackingOpener). GatherSphere
// never closes the stores worldOpener returns — the opener's owner
// (a TrackingOpener for one-shot CLI callers, dash's store cache for the
// dashboard) manages their lifecycle.
//
// readinessOpener is passed through to CaravanStore.CheckCaravanReadiness
// (internal/store, out of scope for this ownership inversion), which
// still opens and closes a world store per readiness check — see the
// comment at its call site below for why it must stay separate from
// worldOpener.
func GatherSphere(sphereStore SphereStore, worldLister WorldLister,
	checker SessionChecker,
	worldOpener func(string) (*store.WorldStore, error),
	readinessOpener func(string) (*store.WorldStore, error),
	caravanStore CaravanStore,
	escalationLister ...EscalationLister) *SphereStatus {

	result := &SphereStatus{
		SOLHome: config.Home(),
	}

	// Per-call memoization: look up each world's store at most once via
	// worldOpener and reuse it across the summary, token, and
	// caravan-title steps below. Never closed here — see doc comment.
	tracked := NewTrackingOpener(worldOpener)

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

	// 4. Gather per-world summaries.
	worlds, err := worldLister.ListWorlds()
	if err == nil {
		for _, w := range worlds {
			summary := gatherWorldSummary(w, sphereStore, checker, tracked.Open, result.Prefect.Running)
			result.Worlds = append(result.Worlds, summary)
		}
	}

	// 4b. Gather token data across all worlds (24h rolling window).
	if worldOpener != nil && len(worlds) > 0 {
		since := time.Now().Add(-24 * time.Hour)
		for _, w := range worlds {
			ws, err := tracked.Open(w.Name)
			if err != nil {
				continue
			}
			summaries, tErr := ws.TokensSince(since)
			if tErr == nil {
				for _, ts := range summaries {
					result.Tokens.InputTokens += ts.InputTokens
					result.Tokens.OutputTokens += ts.OutputTokens
					result.Tokens.CacheTokens += ts.CacheReadTokens + ts.CacheCreationTokens
					if ts.CostUSD != nil {
						result.Tokens.CostUSD += *ts.CostUSD
					}
				}
			}
			agents, _, tErr := ws.WorldTokenMetaSince(since)
			if tErr == nil {
				result.Tokens.AgentCount += agents
			}
			// Do NOT close ws — tracked (and its owner) manages lifecycle.
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
				// CheckCaravanReadiness lives in internal/store and is out
				// of scope for this ownership inversion: it opens a world
				// store per readiness check and closes it internally
				// (defer worldStore.Close()). Feed it readinessOpener — a
				// plain, always-fresh-open opener — rather than tracked,
				// so its Close doesn't invalidate the memoized entry
				// buildCaravanInfo below still needs for this same world.
				// A fresh open per readiness check is accepted residual
				// churn (sol-bf8d0b5ccd1792d7).
				statuses, _ := caravanStore.CheckCaravanReadiness(c.ID, readinessOpener)
				result.Caravans = append(result.Caravans, buildCaravanInfo(c, items, statuses, tracked.Open))
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

	hb, err := consul.ReadHeartbeat()
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
		info.Runtimes = hb.Runtimes
	}

	return info
}

// gatherWorldSummary builds a condensed status for a single world.
func gatherWorldSummary(w store.World, sphereStore SphereStore,
	checker SessionChecker,
	worldOpener func(string) (*store.WorldStore, error),
	prefectRunning bool) WorldSummary {

	summary := WorldSummary{
		Name:       w.Name,
		SourceRepo: w.SourceRepo,
	}

	// Load world config for sleeping status and max_active (non-fatal if fails — DEGRADE).
	if worldCfg, err := config.LoadWorldConfig(w.Name); err == nil {
		summary.Sleeping = worldCfg.World.Sleeping
		summary.MaxActive = worldCfg.Agents.MaxActive
	}

	// Sleeping worlds: still count active agents/envoys, but skip
	// forge/sentinel/MR checks.
	if summary.Sleeping {
		summary.Health = "sleeping"

		// Count agents and envoys that may still be winding down.
		agents, err := sphereStore.ListAgents(w.Name, "")
		if err == nil {
			for _, a := range agents {
				switch a.Role {
				case "envoy":
					summary.Envoys++
				case "forge", "forge-merge", "sentinel", "consul":
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

	// Read the forge heartbeat regardless of whether the daemon is currently
	// running — a persistent remote-git failure (Task B: sol-0ec6b898c083264f)
	// needs to surface in the sphere overview health rollup, not just the
	// per-world detail view.
	forgeRemoteFailures := 0
	if hb, err := forge.ReadHeartbeat(w.Name); err == nil && hb != nil {
		forgeRemoteFailures = hb.ConsecutiveRemoteFailures
	}

	// Check sentinel via PID + heartbeat (sentinel is a direct Go process).
	sentinelPID := sentinel.ReadPID(w.Name)
	summary.Sentinel = sentinelPID > 0 && prefect.IsRunning(sentinelPID)

	// Agent counts from sphere store, separated by role.
	agents, err := sphereStore.ListAgents(w.Name, "")
	if err == nil {
		for _, a := range agents {
			switch a.Role {
			case "envoy":
				summary.Envoys++
			case "forge", "forge-merge", "sentinel", "consul":
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

	// Open world store for MR counts (non-fatal if fails). worldOpener is
	// expected to be a reuse/memoizing opener (TrackingOpener or dash's
	// store cache) — do NOT close ws, its owner manages the lifecycle.
	ws, err := worldOpener(w.Name)
	if err != nil {
		summary.Health = "unknown"
		return summary
	}

	// Get merge request counts.
	mrs, err := ws.ListMergeRequests("")
	if err == nil {
		for _, mr := range mrs {
			switch mr.Phase {
			case "ready":
				summary.MRReady++
			case "failed":
				// Exclude failed MRs whose writs have been re-cast and closed.
				if !isFailedMRRecast(mr.WritID, ws) {
					summary.MRFailed++
				}
			}
		}
	}

	// Delegate to the shared world-health rule encoding — see
	// computeWorldHealthLevel in status.go (also used by WorldStatus.Health).
	summary.Health = levelString(computeWorldHealthLevel(prefectRunning, summary.Dead, summary.MRFailed, forgeRemoteFailures))

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
//   - Any world degraded, e.g. a persistent forge remote-git failure
//     (propagates upward, but yields to a worse "unhealthy" world)
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
	worldDegraded := false
	for _, w := range s.Worlds {
		if w.Sleeping {
			continue
		}
		if w.Health == "unhealthy" || w.Dead > 0 {
			return "unhealthy"
		}
		if w.Health == "degraded" {
			worldDegraded = true
		}
	}
	if s.Consul.Stale {
		return "degraded"
	}
	// Runtime liveness affects sphere health — any failing probe means degraded.
	for _, r := range s.Broker.Runtimes {
		if !r.OK {
			return "degraded"
		}
	}
	if worldDegraded {
		return "degraded"
	}
	return "healthy"
}
