package status

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/broker"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/ledger"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// RuntimeTokenInfo holds per-runtime token usage for display.
type RuntimeTokenInfo struct {
	Runtime      string  `json:"runtime"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

// TokenInfo holds aggregated token usage for a 24-hour rolling window.
type TokenInfo struct {
	InputTokens      int64              `json:"input_tokens"`
	OutputTokens     int64              `json:"output_tokens"`
	CacheTokens      int64              `json:"cache_tokens"`  // cache_read + cache_creation combined
	AgentCount       int                `json:"agent_count"`    // distinct agents with token data in window
	CostUSD          float64            `json:"cost_usd,omitempty"`
	RuntimeBreakdown []RuntimeTokenInfo `json:"runtime_breakdown,omitempty"`
}

// WorldStatus holds the complete runtime state for a world.
type WorldStatus struct {
	World      string         `json:"world"`
	MaxActive  int            `json:"max_active"` // 0 = unlimited
	Prefect    PrefectInfo    `json:"prefect"`
	Forge      ForgeInfo      `json:"forge"`
	Chronicle  ChronicleInfo  `json:"chronicle"`
	Ledger     LedgerInfo     `json:"ledger"`
	Broker     BrokerInfo     `json:"broker"`
	Chancellor ChancellorInfo `json:"chancellor"`
	Sentinel   SentinelInfo   `json:"sentinel"`
	Governor   GovernorInfo   `json:"governor"`
	Agents     []AgentStatus  `json:"agents"`
	Envoys     []EnvoyStatus  `json:"envoys"`
	MergeQueue    MergeQueueInfo    `json:"merge_queue"`
	MergeRequests []MergeRequestInfo `json:"merge_requests,omitempty"`
	Caravans      []CaravanInfo      `json:"caravans,omitempty"`
	Tokens        TokenInfo          `json:"tokens"`
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

// CaravanItemDetail holds per-item detail within a caravan.
type CaravanItemDetail struct {
	WritID   string `json:"writ_id"`
	World    string `json:"world"`
	Phase    int    `json:"phase"`
	Status   string `json:"status"`   // writ status: open, tethered, done, closed
	Ready    bool   `json:"ready"`
	Assignee string `json:"assignee,omitempty"`
	Title    string `json:"title"`
}

// CaravanInfo holds summary information about a caravan relevant to a world.
type CaravanInfo struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Status          string              `json:"status"`
	TotalItems      int                 `json:"total_items"`
	ReadyItems      int                 `json:"ready_items"`
	DoneItems       int                 `json:"done_items"`       // awaiting merge
	ClosedItems     int                 `json:"closed_items"`     // fully merged
	DispatchedItems int                 `json:"dispatched_items"` // in progress (tethered/working)
	Phases          []PhaseProgress     `json:"phases,omitempty"`
	Items           []CaravanItemDetail `json:"items,omitempty"`
}

// PrefectInfo holds prefect process state (sphere-level, not per-world).
type PrefectInfo struct {
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

// ForgeInfo holds forge process state.
type ForgeInfo struct {
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	Merging      bool   `json:"merging,omitempty"`
	PatrolCount  int    `json:"patrol_count,omitempty"`
	QueueDepth   int    `json:"queue_depth,omitempty"`
	MergesTotal  int    `json:"merges_total,omitempty"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
	Paused       bool   `json:"paused,omitempty"`

	// Heartbeat details for peek view idle state display.
	Status      string `json:"status,omitempty"`       // "idle", "working", "stopping"
	LastMerge   string `json:"last_merge,omitempty"`    // human-readable age of last merge
	LastError   string `json:"last_error,omitempty"`
	CurrentMR   string `json:"current_mr,omitempty"`
	CurrentWrit string `json:"current_writ,omitempty"`
}

// ChronicleInfo holds chronicle process state (sphere-level).
type ChronicleInfo struct {
	Running         bool   `json:"running"`
	PID             int    `json:"pid,omitempty"`
	EventsProcessed int64  `json:"events_processed,omitempty"`
	HeartbeatAge    string `json:"heartbeat_age,omitempty"`
	Stale           bool   `json:"stale,omitempty"`
}

// LedgerInfo holds ledger process state (sphere-level OTLP receiver).
type LedgerInfo struct {
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	Port         int    `json:"port,omitempty"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
}

// SentinelInfo holds sentinel process state (per-world).
type SentinelInfo struct {
	Running       bool   `json:"running"`
	PID           int    `json:"pid,omitempty"`
	PatrolCount   int    `json:"patrol_count,omitempty"`
	AgentsChecked int    `json:"agents_checked,omitempty"`
	StalledCount  int    `json:"stalled_count,omitempty"`
	ReapedCount   int    `json:"reaped_count,omitempty"`
	HeartbeatAge  string `json:"heartbeat_age,omitempty"`
	Status        string `json:"status,omitempty"` // "running", "assessing", "stopping"
	Stale         bool   `json:"stale,omitempty"`
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
	ListAgents(world string, state store.AgentState) ([]store.Agent, error)
}

// MergeQueueStore abstracts merge request queries for testing.
type MergeQueueStore interface {
	ListMergeRequests(phase store.MRPhase) ([]store.MergeRequest, error)
}

// CaravanStore abstracts caravan queries for status gathering.
type CaravanStore interface {
	ListCaravans(status store.CaravanStatus) ([]store.Caravan, error)
	CheckCaravanReadiness(caravanID string, worldOpener func(world string) (*store.WorldStore, error)) ([]store.CaravanItemStatus, error)
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

// ChancellorInfo holds chancellor process state (sphere-level).
type ChancellorInfo struct {
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
	Chancellor  ChancellorInfo      `json:"chancellor"`
	Worlds      []WorldSummary      `json:"worlds"`
	Tokens      TokenInfo           `json:"tokens"`
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

// BrokerInfo holds broker process state.
type BrokerInfo struct {
	Running        bool                        `json:"running"`
	HeartbeatAge   string                      `json:"heartbeat_age,omitempty"`
	PatrolCount    int                         `json:"patrol_count,omitempty"`
	Stale          bool                        `json:"stale"`
	ProviderHealth string                      `json:"provider_health,omitempty"` // "healthy", "degraded", "down"
	TokenHealth    []broker.AccountTokenHealth `json:"token_health,omitempty"`
}

// WorldSummary holds a condensed view of one world for the sphere overview.
type WorldSummary struct {
	Name       string `json:"name"`
	SourceRepo string `json:"source_repo,omitempty"`
	Sleeping   bool   `json:"sleeping,omitempty"`
	Agents     int    `json:"agents"`
	MaxActive  int    `json:"max_active"` // 0 = unlimited
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

	// 2. Check forge process via PID file (primary) and heartbeat (secondary).
	forgePID := forge.ReadPID(world)
	forgeRunning := forgePID > 0 && forge.IsRunning(forgePID)
	if forgeRunning {
		forgeInfo := ForgeInfo{Running: true, PID: forgePID}
		// Read heartbeat for metrics.
		if hb, err := forge.ReadHeartbeat(world); err == nil && hb != nil {
			forgeInfo.PatrolCount = hb.PatrolCount
			forgeInfo.QueueDepth = hb.QueueDepth
			forgeInfo.MergesTotal = hb.MergesTotal
			forgeInfo.HeartbeatAge = FormatDuration(time.Since(hb.Timestamp))
			forgeInfo.Stale = hb.IsStale(5 * time.Minute)
			forgeInfo.Status = hb.Status
			forgeInfo.CurrentMR = hb.CurrentMR
			forgeInfo.CurrentWrit = hb.CurrentWrit
			forgeInfo.LastError = hb.LastError
			if !hb.LastMerge.IsZero() {
				forgeInfo.LastMerge = FormatDuration(time.Since(hb.LastMerge))
			}
		}
		forgeInfo.Paused = forge.IsForgePaused(world)
		result.Forge = forgeInfo
	} else {
		// Forge not running — still read heartbeat for last merge context.
		forgeInfo := ForgeInfo{Running: false}
		if hb, err := forge.ReadHeartbeat(world); err == nil && hb != nil {
			forgeInfo.Status = hb.Status
			forgeInfo.QueueDepth = hb.QueueDepth
			forgeInfo.MergesTotal = hb.MergesTotal
			forgeInfo.CurrentMR = hb.CurrentMR
			forgeInfo.CurrentWrit = hb.CurrentWrit
			forgeInfo.LastError = hb.LastError
			if !hb.LastMerge.IsZero() {
				forgeInfo.LastMerge = FormatDuration(time.Since(hb.LastMerge))
			}
		}
		forgeInfo.Paused = forge.IsForgePaused(world)
		// Check if a merge session is active.
		mergeSessName := config.SessionName(world, "forge-merge")
		if checker.Exists(mergeSessName) {
			forgeInfo.Merging = true
		}
		result.Forge = forgeInfo
	}

	// 2b. Check chronicle (sphere-level): PID + heartbeat.
	result.Chronicle = GatherChronicleInfo()

	// 2b1. Check ledger (sphere-level): PID + heartbeat.
	result.Ledger = GatherLedgerInfo()

	// 2b2. Check broker (sphere-level).
	result.Broker = GatherBrokerInfo()

	// 2b3. Check chancellor (sphere-level).
	const chancellorSessionName = "sol-chancellor"
	if checker.Exists(chancellorSessionName) {
		result.Chancellor = ChancellorInfo{Running: true, SessionName: chancellorSessionName}
	}

	// 2c. Check sentinel process (direct Go process with PID + heartbeat).
	result.Sentinel = GatherSentinelInfo(world)

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

		default: // "outpost", "forge", "sentinel", "consul"
			// forge, sentinel, consul are handled separately above.
			if agent.Role == "forge" || agent.Role == "forge-merge" || agent.Role == "sentinel" || agent.Role == "consul" {
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
			if isFailedMRRecast(mr.WritID, worldStore) {
				include = false
			} else {
				result.MergeQueue.Failed++
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

// isFailedMRRecast returns true if a failed MR should be excluded because
// its writ has been re-cast and is now closed.
func isFailedMRRecast(writID string, ws WorldStore) bool {
	item, err := ws.GetWrit(writID)
	return err == nil && item.Status == "closed"
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
func GatherCaravans(result *WorldStatus, caravanStore CaravanStore, worldOpener func(string) (*store.WorldStore, error)) {
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
		info := buildCaravanInfo(c, items, statuses, worldOpener)
		result.Caravans = append(result.Caravans, info)
	}
}

// buildCaravanInfo computes aggregate item counts and phase progress for a caravan.
// This is the single source of truth for caravan status computation, used by both
// GatherCaravans (world-scoped) and GatherSphere (sphere-scoped).
func buildCaravanInfo(c store.Caravan, items []store.CaravanItem, statuses []store.CaravanItemStatus, worldOpener func(string) (*store.WorldStore, error)) CaravanInfo {
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

	// Build per-item detail from statuses.
	worldStores := make(map[string]*store.WorldStore) // cache opened stores
	for _, st := range statuses {
		detail := CaravanItemDetail{
			WritID:   st.WritID,
			World:    st.World,
			Phase:    st.Phase,
			Status:   st.WritStatus,
			Ready:    st.Ready,
			Assignee: st.Assignee,
			Title:    "(unknown)",
		}
		// Look up writ title via worldOpener.
		if worldOpener != nil {
			ws, ok := worldStores[st.World]
			if !ok {
				ws, _ = worldOpener(st.World)
				worldStores[st.World] = ws // may be nil
			}
			if ws != nil {
				if w, err := ws.GetWrit(st.WritID); err == nil {
					detail.Title = w.Title
				}
			}
		}
		info.Items = append(info.Items, detail)
	}
	// Close cached stores.
	for _, ws := range worldStores {
		if ws != nil {
			ws.Close()
		}
	}

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

// GatherSentinelInfo reads sentinel PID + heartbeat state.
// The sentinel is a direct Go process (not a tmux session), so PID + heartbeat
// are the canonical signals.
func GatherSentinelInfo(world string) SentinelInfo {
	info := SentinelInfo{}

	pid := sentinel.ReadPID(world)
	if pid > 0 && prefect.IsRunning(pid) {
		info.Running = true
		info.PID = pid
	}

	hb, err := sentinel.ReadHeartbeat(world)
	if err == nil && hb != nil {
		info.PatrolCount = hb.PatrolCount
		info.AgentsChecked = hb.AgentsChecked
		info.StalledCount = hb.StalledCount
		info.ReapedCount = hb.ReapedCount
		info.Status = hb.Status

		age := time.Since(hb.Timestamp)
		info.HeartbeatAge = FormatDuration(age)
		info.Stale = hb.IsStale(15 * time.Minute)
	}

	return info
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

// GatherTokens populates token usage data on a WorldStatus using a 24-hour
// rolling window. Errors are handled gracefully — if the store can't be queried,
// TokenInfo is left zeroed and the renderer handles the zero case.
func GatherTokens(result *WorldStatus, worldStore *store.WorldStore) {
	since := time.Now().Add(-24 * time.Hour)

	summaries, err := worldStore.TokensSince(since)
	if err != nil {
		return
	}
	for _, ts := range summaries {
		result.Tokens.InputTokens += ts.InputTokens
		result.Tokens.OutputTokens += ts.OutputTokens
		result.Tokens.CacheTokens += ts.CacheReadTokens + ts.CacheCreationTokens
		if ts.CostUSD != nil {
			result.Tokens.CostUSD += *ts.CostUSD
		}
	}

	agents, _, err := worldStore.WorldTokenMetaSince(since)
	if err != nil {
		return
	}
	result.Tokens.AgentCount = agents

	// Gather per-runtime breakdown (only populated when multiple runtimes present).
	rtSummaries, err := worldStore.TokensByRuntimeSince(since)
	if err != nil || len(rtSummaries) <= 1 {
		return // single or no runtime — don't clutter display
	}
	for _, rts := range rtSummaries {
		rti := RuntimeTokenInfo{
			Runtime:      rts.Runtime,
			InputTokens:  rts.InputTokens,
			OutputTokens: rts.OutputTokens,
			CacheTokens:  rts.CacheReadTokens + rts.CacheCreationTokens,
		}
		if rts.CostUSD != nil {
			rti.CostUSD = *rts.CostUSD
		}
		if rti.Runtime == "" {
			rti.Runtime = "unknown"
		}
		result.Tokens.RuntimeBreakdown = append(result.Tokens.RuntimeBreakdown, rti)
	}
}

// GatherLedgerInfo reads ledger process state from PID file and heartbeat.
// The ledger is a Go process (not a tmux session), so PID + heartbeat are
// the canonical signals.
func GatherLedgerInfo() LedgerInfo {
	info := LedgerInfo{}

	pid := ledger.ReadPID()
	if pid > 0 && prefect.IsRunning(pid) {
		info.Running = true
		info.PID = pid
		info.Port = ledger.DefaultPort
	}

	hb, err := ledger.ReadHeartbeat()
	if err == nil && hb != nil {
		info.HeartbeatAge = FormatDuration(time.Since(hb.Timestamp))
		info.Stale = hb.IsStale(5 * time.Minute)
	}

	return info
}
