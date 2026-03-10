package status

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/ledger"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// WorldStatus holds the complete runtime state for a world.
type WorldStatus struct {
	World      string         `json:"world"`
	Capacity   int            `json:"capacity"` // 0 = unlimited
	Prefect    PrefectInfo    `json:"prefect"`
	Forge      ForgeInfo      `json:"forge"`
	Chronicle  ChronicleInfo  `json:"chronicle"`
	Ledger     LedgerInfo     `json:"ledger"`
	Broker     BrokerInfo     `json:"broker"`
	Senate     SenateInfo     `json:"senate"`
	Sentinel   SentinelInfo   `json:"sentinel"`
	Governor   GovernorInfo   `json:"governor"`
	Agents     []AgentStatus  `json:"agents"`
	Envoys     []EnvoyStatus  `json:"envoys"`
	MergeQueue    MergeQueueInfo    `json:"merge_queue"`
	MergeRequests []MergeRequestInfo `json:"merge_requests,omitempty"`
	Caravans      []CaravanInfo      `json:"caravans,omitempty"`
	Summary       Summary            `json:"summary"`
}

// GovernorInfo holds governor process state.
type GovernorInfo struct {
	Running      bool   `json:"running"`
	SessionAlive bool   `json:"session_alive"`
	BriefAge     string `json:"brief_age,omitempty"`
}

// EnvoyStatus holds the combined state of one envoy agent.
type EnvoyStatus struct {
	Name           string `json:"name"`
	State          string `json:"state"`
	SessionAlive   bool   `json:"session_alive"`
	ActiveWrit     string `json:"active_writ,omitempty"`
	WorkTitle      string `json:"work_title,omitempty"`
	TetheredCount  int    `json:"tethered_count,omitempty"`
	BriefAge       string `json:"brief_age,omitempty"`
	NudgeCount     int    `json:"nudge_count,omitempty"`
}

// PhaseProgress holds progress info for a single phase within a caravan.
type PhaseProgress struct {
	Phase      int `json:"phase"`
	Total      int `json:"total"`
	Done       int `json:"done"`       // awaiting merge
	Closed     int `json:"closed"`     // fully merged
	Ready      int `json:"ready"`
	Dispatched int `json:"dispatched"` // in progress (tethered/working)
}

// CaravanInfo holds summary information about a caravan relevant to a world.
type CaravanInfo struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Status          string          `json:"status"`
	TotalItems      int             `json:"total_items"`
	ReadyItems      int             `json:"ready_items"`
	DoneItems       int             `json:"done_items"`       // awaiting merge
	ClosedItems     int             `json:"closed_items"`     // fully merged
	DispatchedItems int             `json:"dispatched_items"` // in progress (tethered/working)
	Phases          []PhaseProgress `json:"phases,omitempty"`
}

// PrefectInfo holds prefect process state (sphere-level, not per-world).
type PrefectInfo struct {
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

// ForgeInfo holds forge process state.
type ForgeInfo struct {
	Running      bool   `json:"running"`
	SessionName  string `json:"session_name,omitempty"`
	PatrolCount  int    `json:"patrol_count,omitempty"`
	QueueDepth   int    `json:"queue_depth,omitempty"`
	MergesTotal  int    `json:"merges_total,omitempty"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
	Paused       bool   `json:"paused,omitempty"`
}

// ChronicleInfo holds chronicle process state (sphere-level).
type ChronicleInfo struct {
	Running     bool   `json:"running"`
	SessionName string `json:"session_name,omitempty"`
	PID         int    `json:"pid,omitempty"`
}

// LedgerInfo holds ledger process state (sphere-level OTLP receiver).
type LedgerInfo struct {
	Running      bool   `json:"running"`
	SessionName  string `json:"session_name,omitempty"`
	PID          int    `json:"pid,omitempty"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
}

// SentinelInfo holds sentinel process state (per-world).
type SentinelInfo struct {
	Running     bool   `json:"running"`
	SessionName string `json:"session_name,omitempty"`
}

// MergeQueueInfo holds merge queue summary.
type MergeQueueInfo struct {
	Ready   int `json:"ready"`
	Claimed int `json:"claimed"`
	Failed  int `json:"failed"`
	Merged  int `json:"merged"`
	Total   int `json:"total"`
}

// MergeRequestInfo holds individual merge request details for display.
type MergeRequestInfo struct {
	ID     string `json:"id"`
	WritID string `json:"writ_id"`
	Phase  string `json:"phase"`
	Title  string `json:"title"`
}

// AgentStatus holds the combined state of one agent.
type AgentStatus struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	SessionAlive bool   `json:"session_alive"`
	ActiveWrit   string `json:"active_writ,omitempty"`
	WorkTitle    string `json:"work_title,omitempty"`
	NudgeCount   int    `json:"nudge_count,omitempty"`
}

// Summary holds aggregate counts.
type Summary struct {
	Total   int `json:"total"`
	Working int `json:"working"`
	Idle    int `json:"idle"`
	Stalled int `json:"stalled"`
	Dead    int `json:"dead"`
}

// Health returns the world-scoped health level for a single world.
//
// This is distinct from computeSphereHealth() in sphere.go which computes
// sphere-wide health by aggregating across all worlds plus sphere-level
// components (consul staleness, prefect).
//
// World health checks only local conditions:
//   - Prefect running (required for session respawn)
//   - Dead agent sessions (working agents whose tmux sessions have died)
//   - Failed merge requests (work completed but merge failed)
//
// Returns:
//   0 = healthy (all sessions alive or idle, no failed merge requests)
//   1 = unhealthy (at least one dead session or failed merge request)
//   2 = degraded (prefect not running — sessions cannot be respawned)
//
// Forge state does not affect health — an absent forge just means
// merges won't happen, the system is still operational. Envoy and
// governor sessions are human-supervised and do not affect health.
func (r *WorldStatus) Health() int {
	if !r.Prefect.Running {
		return 2
	}
	if r.Summary.Dead > 0 || r.MergeQueue.Failed > 0 {
		return 1
	}
	return 0
}

// HealthString returns the health level as a string.
func (r *WorldStatus) HealthString() string {
	switch r.Health() {
	case 0:
		return "healthy"
	case 1:
		return "unhealthy"
	case 2:
		return "degraded"
	default:
		return fmt.Sprintf("unknown(%d)", r.Health())
	}
}

// SessionChecker abstracts session liveness checks for testing.
type SessionChecker interface {
	Exists(name string) bool
}

// WorldStore abstracts writ lookups for testing.
type WorldStore interface {
	GetWrit(id string) (*store.Writ, error)
}

// SphereStore abstracts agent queries for testing.
type SphereStore interface {
	ListAgents(world string, state string) ([]store.Agent, error)
}

// MergeQueueStore abstracts merge request queries for testing.
type MergeQueueStore interface {
	ListMergeRequests(phase string) ([]store.MergeRequest, error)
}

// CaravanStore abstracts caravan queries for status gathering.
type CaravanStore interface {
	ListCaravans(status string) ([]store.Caravan, error)
	CheckCaravanReadiness(caravanID string, worldOpener func(world string) (*store.Store, error)) ([]store.CaravanItemStatus, error)
	ListCaravanItems(caravanID string) ([]store.CaravanItem, error)
}

// WorldLister abstracts world listing for sphere status.
type WorldLister interface {
	ListWorlds() ([]store.World, error)
}

// EscalationLister abstracts escalation queries for sphere status.
type EscalationLister interface {
	ListOpenEscalations() ([]store.Escalation, error)
}

// SenateInfo holds senate process state (sphere-level).
type SenateInfo struct {
	Running     bool   `json:"running"`
	SessionName string `json:"session_name,omitempty"`
}

// EscalationSummary holds aggregated escalation counts for status display.
type EscalationSummary struct {
	Total      int            `json:"total"`
	BySeverity map[string]int `json:"by_severity"`
}

// SphereStatus holds the complete runtime state for the sphere.
type SphereStatus struct {
	SOLHome     string              `json:"sol_home"`
	Prefect     PrefectInfo         `json:"prefect"`
	Consul      ConsulInfo          `json:"consul"`
	Chronicle   ChronicleInfo       `json:"chronicle"`
	Ledger      LedgerInfo          `json:"ledger"`
	Broker      BrokerInfo          `json:"broker"`
	Senate      SenateInfo          `json:"senate"`
	Worlds      []WorldSummary      `json:"worlds"`
	Caravans    []CaravanInfo       `json:"caravans,omitempty"`
	Escalations *EscalationSummary  `json:"escalations,omitempty"`
	MailCount   int                 `json:"mail_count,omitempty"`
	Health      string              `json:"health"`
}

// ConsulInfo holds consul process state.
type ConsulInfo struct {
	Running      bool   `json:"running"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	PatrolCount  int    `json:"patrol_count,omitempty"`
	Stale        bool   `json:"stale"`
}

// BrokerInfo holds token broker process state.
type BrokerInfo struct {
	Running      bool   `json:"running"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	PatrolCount  int    `json:"patrol_count,omitempty"`
	Accounts     int    `json:"accounts,omitempty"`
	AgentDirs    int    `json:"agent_dirs,omitempty"`
	Stale        bool   `json:"stale"`
}

// WorldSummary holds a condensed view of one world for the sphere overview.
type WorldSummary struct {
	Name       string `json:"name"`
	SourceRepo string `json:"source_repo,omitempty"`
	Sleeping   bool   `json:"sleeping,omitempty"`
	Agents     int    `json:"agents"`
	Capacity   int    `json:"capacity"` // 0 = unlimited
	Envoys     int    `json:"envoys"`
	Governor   bool   `json:"governor"`
	Working    int    `json:"working"`
	Idle       int    `json:"idle"`
	Stalled    int    `json:"stalled"`
	Dead       int    `json:"dead"`
	Forge      bool   `json:"forge"`
	Sentinel   bool   `json:"sentinel"`
	MRReady    int    `json:"mr_ready"`
	MRFailed   int    `json:"mr_failed"`
	Health     string `json:"health"`
}

// Gather collects runtime state for a world.
func Gather(world string, sphereStore SphereStore, worldStore WorldStore,
	mqStore MergeQueueStore, checker SessionChecker) (*WorldStatus, error) {
	result := &WorldStatus{World: world}

	// 1. Check prefect (sphere-level).
	pid, err := prefect.ReadPID()
	if err != nil {
		return nil, fmt.Errorf("failed to read prefect PID: %w", err)
	}
	if pid != 0 && prefect.IsRunning(pid) {
		result.Prefect = PrefectInfo{Running: true, PID: pid}
	}

	// 2. Check forge process (Go process in tmux session).
	forgeSessName := config.SessionName(world, "forge")
	if checker.Exists(forgeSessName) {
		forgeInfo := ForgeInfo{Running: true, SessionName: forgeSessName}
		// Read heartbeat for metrics.
		if hb, err := forge.ReadHeartbeat(world); err == nil && hb != nil {
			forgeInfo.PatrolCount = hb.PatrolCount
			forgeInfo.QueueDepth = hb.QueueDepth
			forgeInfo.MergesTotal = hb.MergesTotal
			forgeInfo.HeartbeatAge = FormatDuration(time.Since(hb.Timestamp))
			forgeInfo.Stale = hb.IsStale(5 * time.Minute)
		}
		forgeInfo.Paused = forge.IsForgePaused(world)
		result.Forge = forgeInfo
	}

	// 2b. Check chronicle (sphere-level): tmux session first, PID-file fallback.
	const chronicleSessionName = "sol-chronicle"
	if checker.Exists(chronicleSessionName) {
		result.Chronicle = ChronicleInfo{Running: true, SessionName: chronicleSessionName}
	} else if pid := readChroniclePID(); pid > 0 && prefect.IsRunning(pid) {
		result.Chronicle = ChronicleInfo{Running: true, PID: pid}
	}

	// 2b1. Check ledger (sphere-level): heartbeat primary, session/PID fallback.
	result.Ledger = GatherLedgerInfo(checker)

	// 2b2. Check broker (sphere-level).
	result.Broker = GatherBrokerInfo()

	// 2b3. Check senate (sphere-level).
	const senateSessionName = "sol-senate"
	if checker.Exists(senateSessionName) {
		result.Senate = SenateInfo{Running: true, SessionName: senateSessionName}
	}

	// 2c. Check sentinel session.
	sentinelSessName := config.SessionName(world, "sentinel")
	if checker.Exists(sentinelSessName) {
		result.Sentinel = SentinelInfo{Running: true, SessionName: sentinelSessName}
	}

	// 2d. Check governor.
	govSessName := config.SessionName(world, "governor")
	govSessAlive := checker.Exists(govSessName)

	// 3. List all agents for this world.
	agents, err := sphereStore.ListAgents(world, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	// 4. Build agent statuses, separated by role.
	for _, agent := range agents {
		sessName := config.SessionName(world, agent.Name)
		sessAlive := checker.Exists(sessName)

		switch agent.Role {
		case "governor":
			result.Governor = GovernorInfo{
				Running:      govSessAlive,
				SessionAlive: govSessAlive,
				BriefAge:     briefAge(governor.BriefPath(world)),
			}

		case "envoy":
			es := EnvoyStatus{
				Name:         agent.Name,
				State:        agent.State,
				SessionAlive: sessAlive,
				BriefAge:     briefAge(envoy.BriefPath(world, agent.Name)),
			}
			if agent.ActiveWrit != "" {
				es.ActiveWrit = agent.ActiveWrit
				item, err := worldStore.GetWrit(agent.ActiveWrit)
				if err != nil {
					es.WorkTitle = "(unknown)"
				} else {
					es.WorkTitle = item.Title
				}
			}
			// Count tethered writs from directory listing.
			if tethered, err := tether.List(world, agent.Name, "envoy"); err == nil {
				es.TetheredCount = len(tethered)
			}
			// Nudge queue depth.
			if count, err := nudge.Peek(sessName); err == nil && count > 0 {
				es.NudgeCount = count
			}
			result.Envoys = append(result.Envoys, es)

		default: // "agent", "forge", "sentinel", "consul"
			// forge, sentinel, consul are handled separately above.
			if agent.Role == "forge" || agent.Role == "sentinel" || agent.Role == "consul" {
				continue
			}
			as := AgentStatus{
				Name:         agent.Name,
				State:        agent.State,
				SessionAlive: sessAlive,
			}
			if agent.ActiveWrit != "" {
				as.ActiveWrit = agent.ActiveWrit
				item, err := worldStore.GetWrit(agent.ActiveWrit)
				if err != nil {
					as.WorkTitle = "(unknown)"
				} else {
					as.WorkTitle = item.Title
				}
			}
			// Nudge queue depth.
			if count, err := nudge.Peek(sessName); err == nil && count > 0 {
				as.NudgeCount = count
			}
			result.Agents = append(result.Agents, as)
		}
	}

	// 5. Compute summary counts (outpost agents only — envoys and governor
	// are human-supervised and do not affect health).
	for _, as := range result.Agents {
		result.Summary.Total++
		switch as.State {
		case "working":
			result.Summary.Working++
			if !as.SessionAlive {
				result.Summary.Dead++
			}
		case "idle":
			result.Summary.Idle++
		case "stalled":
			result.Summary.Stalled++
		}
	}

	// 6. Gather merge queue info.
	mrs, err := mqStore.ListMergeRequests("")
	if err != nil {
		return nil, fmt.Errorf("failed to list merge requests: %w", err)
	}
	for _, mr := range mrs {
		result.MergeQueue.Total++
		include := true
		switch mr.Phase {
		case "ready":
			result.MergeQueue.Ready++
		case "claimed":
			result.MergeQueue.Claimed++
		case "failed":
			// Exclude failed MRs whose writs have been re-cast and closed.
			if item, err := worldStore.GetWrit(mr.WritID); err != nil || item.Status != "closed" {
				result.MergeQueue.Failed++
			} else {
				include = false
			}
		case "merged":
			result.MergeQueue.Merged++
		}

		if include {
			title := ""
			if item, err := worldStore.GetWrit(mr.WritID); err == nil {
				title = item.Title
			}
			result.MergeRequests = append(result.MergeRequests, MergeRequestInfo{
				ID:     mr.ID,
				WritID: mr.WritID,
				Phase:  mr.Phase,
				Title:  title,
			})
		}
	}

	return result, nil
}

// briefAge returns a human-readable age of a brief file, or empty if not found.
func briefAge(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return FormatDuration(time.Since(info.ModTime()))
}

// GatherCaravans adds caravan information to a WorldStatus.
// This is separate from Gather because it requires the CaravanStore interface
// which not all callers may have available.
func GatherCaravans(result *WorldStatus, caravanStore CaravanStore, worldOpener func(string) (*store.Store, error)) {
	allCaravans, err := caravanStore.ListCaravans("")
	if err != nil {
		return // non-fatal: degrade gracefully
	}
	// Filter to active (non-closed) caravans.
	var caravans []store.Caravan
	for _, c := range allCaravans {
		if c.Status != "closed" {
			caravans = append(caravans, c)
		}
	}

	for _, c := range caravans {
		items, err := caravanStore.ListCaravanItems(c.ID)
		if err != nil {
			continue
		}

		// Check if this caravan has items in our world.
		hasWorldItems := false
		for _, item := range items {
			if item.World == result.World {
				hasWorldItems = true
				break
			}
		}
		if !hasWorldItems {
			continue
		}

		statuses, _ := caravanStore.CheckCaravanReadiness(c.ID, worldOpener)
		info := buildCaravanInfo(c, items, statuses)
		result.Caravans = append(result.Caravans, info)
	}
}

// buildCaravanInfo computes aggregate item counts and phase progress for a caravan.
// This is the single source of truth for caravan status computation, used by both
// GatherCaravans (world-scoped) and GatherSphere (sphere-scoped).
func buildCaravanInfo(c store.Caravan, items []store.CaravanItem, statuses []store.CaravanItemStatus) CaravanInfo {
	info := CaravanInfo{
		ID:         c.ID,
		Name:       c.Name,
		Status:     c.Status,
		TotalItems: len(items),
	}
	for _, st := range statuses {
		switch {
		case st.WritStatus == "closed":
			info.ClosedItems++
		case st.WritStatus == "done":
			info.DoneItems++
		case st.IsDispatched():
			info.DispatchedItems++
		case st.WritStatus == "open" && st.Ready:
			info.ReadyItems++
		}
	}
	info.Phases = computePhaseProgress(items, statuses)
	return info
}

// computePhaseProgress builds phase breakdown for a caravan.
// Returns nil if all items are in phase 0 (no phase display needed).
func computePhaseProgress(items []store.CaravanItem, statuses []store.CaravanItemStatus) []PhaseProgress {
	// Check if any items have phase > 0.
	hasPhases := false
	for _, item := range items {
		if item.Phase > 0 {
			hasPhases = true
			break
		}
	}
	if !hasPhases {
		return nil
	}

	// Build a status lookup by writ ID.
	statusMap := make(map[string]store.CaravanItemStatus)
	for _, st := range statuses {
		statusMap[st.WritID] = st
	}

	// Group by phase.
	phaseMap := make(map[int]*PhaseProgress)
	for _, item := range items {
		pp, ok := phaseMap[item.Phase]
		if !ok {
			pp = &PhaseProgress{Phase: item.Phase}
			phaseMap[item.Phase] = pp
		}
		pp.Total++
		if st, ok := statusMap[item.WritID]; ok {
			switch {
			case st.WritStatus == "closed":
				pp.Closed++
			case st.WritStatus == "done":
				pp.Done++
			case st.IsDispatched():
				pp.Dispatched++
			case st.WritStatus == "open" && st.Ready:
				pp.Ready++
			}
		}
	}

	// Sort by phase number.
	var result []PhaseProgress
	for p := 0; p <= maxPhase(phaseMap); p++ {
		if pp, ok := phaseMap[p]; ok {
			result = append(result, *pp)
		}
	}
	return result
}

func maxPhase(m map[int]*PhaseProgress) int {
	max := 0
	for p := range m {
		if p > max {
			max = p
		}
	}
	return max
}

// readChroniclePID reads the chronicle PID from its PID file. Returns 0 if not found.
func readChroniclePID() int {
	data, err := os.ReadFile(filepath.Join(config.RuntimeDir(), "chronicle.pid"))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// GatherLedgerInfo reads ledger status using heartbeat as primary signal,
// with tmux session and PID file as fallbacks.
func GatherLedgerInfo(checker SessionChecker) LedgerInfo {
	info := LedgerInfo{}

	const ledgerSessionName = "sol-ledger"

	// Primary: check heartbeat file.
	if hb, err := ledger.ReadHeartbeat(); err == nil && hb != nil {
		info.Running = true
		info.HeartbeatAge = FormatDuration(time.Since(hb.Timestamp))
		info.Stale = hb.IsStale(2 * time.Minute)
		if checker.Exists(ledgerSessionName) {
			info.SessionName = ledgerSessionName
		}
		return info
	}

	// Fallback: tmux session exists.
	if checker.Exists(ledgerSessionName) {
		info.Running = true
		info.SessionName = ledgerSessionName
		return info
	}

	// Fallback: PID file.
	if pid := ledger.ReadPID(); pid > 0 && prefect.IsRunning(pid) {
		info.Running = true
		info.PID = pid
		return info
	}

	return info
}
