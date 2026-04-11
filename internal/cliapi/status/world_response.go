package status

import (
	internstatus "github.com/nevinsm/sol/internal/status"
)

// WorldStatusResponse is the CLI API response for `sol status <world>`.
type WorldStatusResponse struct {
	World         string             `json:"world"`
	MaxActive     int                `json:"max_active"`
	Prefect       PrefectInfo        `json:"prefect"`
	Forge         ForgeInfo          `json:"forge"`
	Chronicle     ChronicleInfo      `json:"chronicle"`
	Ledger        LedgerInfo         `json:"ledger"`
	Broker        BrokerInfo         `json:"broker"`
	Sentinel      SentinelInfo       `json:"sentinel"`
	Agents        []AgentStatus      `json:"agents"`
	Envoys        []EnvoyStatus      `json:"envoys"`
	MergeQueue    MergeQueueInfo     `json:"merge_queue"`
	MergeRequests []MergeRequestInfo `json:"merge_requests,omitempty"`
	Caravans      []CaravanInfo      `json:"caravans,omitempty"`
	Tokens        TokenInfo          `json:"tokens"`
	Summary       Summary            `json:"summary"`
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
	Status       string `json:"status,omitempty"`
	LastMerge    string `json:"last_merge,omitempty"`
	LastError    string `json:"last_error,omitempty"`
	CurrentMR    string `json:"current_mr_id,omitempty"`
	CurrentWrit  string `json:"current_writ_id,omitempty"`
}

// SentinelInfo holds sentinel process state.
type SentinelInfo struct {
	Running       bool   `json:"running"`
	PID           int    `json:"pid,omitempty"`
	PatrolCount   int    `json:"patrol_count,omitempty"`
	AgentsChecked int    `json:"agents_checked,omitempty"`
	StalledCount  int    `json:"stalled_count,omitempty"`
	ReapedCount   int    `json:"reaped_count,omitempty"`
	HeartbeatAge  string `json:"heartbeat_age,omitempty"`
	Status        string `json:"status,omitempty"`
	Stale         bool   `json:"stale,omitempty"`
}

// AgentStatus holds the combined state of one agent.
type AgentStatus struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	SessionAlive bool   `json:"session_alive"`
	ActiveWrit   string `json:"active_writ_id,omitempty"`
	WorkTitle    string `json:"work_title,omitempty"`
	NudgeCount   int    `json:"nudge_count,omitempty"`
}

// EnvoyStatus holds the combined state of one envoy agent.
type EnvoyStatus struct {
	Name          string `json:"name"`
	State         string `json:"state"`
	SessionAlive  bool   `json:"session_alive"`
	ActiveWrit    string `json:"active_writ_id,omitempty"`
	WorkTitle     string `json:"work_title,omitempty"`
	TetheredCount int    `json:"tethered_count,omitempty"`
	NudgeCount    int    `json:"nudge_count,omitempty"`
}

// MergeQueueInfo holds merge queue summary.
type MergeQueueInfo struct {
	Ready   int `json:"ready"`
	Claimed int `json:"claimed"`
	Failed  int `json:"failed"`
	Merged  int `json:"merged"`
	Total   int `json:"total"`
}

// MergeRequestInfo holds individual merge request details.
type MergeRequestInfo struct {
	ID     string `json:"id"`
	WritID string `json:"writ_id"`
	Phase  string `json:"phase"`
	Title  string `json:"title"`
}

// Summary holds aggregate counts.
type Summary struct {
	Total   int `json:"total"`
	Working int `json:"working"`
	Idle    int `json:"idle"`
	Stalled int `json:"stalled"`
	Dead    int `json:"dead"`
}

// Health returns the world-scoped health level.
// This mirrors status.WorldStatus.Health() to preserve exit code behavior.
func (r *WorldStatusResponse) Health() int {
	if !r.Prefect.Running {
		return 2
	}
	if r.Summary.Dead > 0 || r.MergeQueue.Failed > 0 {
		return 1
	}
	return 0
}

// FromWorldStatus converts an internal status.WorldStatus to the CLI API response type.
func FromWorldStatus(ws *internstatus.WorldStatus) *WorldStatusResponse {
	resp := &WorldStatusResponse{
		World:     ws.World,
		MaxActive: ws.MaxActive,
		Prefect:   convertPrefectInfo(ws.Prefect),
		Forge:     convertForgeInfo(ws.Forge),
		Chronicle: convertChronicleInfo(ws.Chronicle),
		Ledger:    convertLedgerInfo(ws.Ledger),
		Broker:    convertBrokerInfo(ws.Broker),
		Sentinel:  convertSentinelInfo(ws.Sentinel),
		MergeQueue: MergeQueueInfo{
			Ready:   ws.MergeQueue.Ready,
			Claimed: ws.MergeQueue.Claimed,
			Failed:  ws.MergeQueue.Failed,
			Merged:  ws.MergeQueue.Merged,
			Total:   ws.MergeQueue.Total,
		},
		Tokens: convertTokenInfo(ws.Tokens),
		Summary: Summary{
			Total:   ws.Summary.Total,
			Working: ws.Summary.Working,
			Idle:    ws.Summary.Idle,
			Stalled: ws.Summary.Stalled,
			Dead:    ws.Summary.Dead,
		},
	}

	for _, a := range ws.Agents {
		resp.Agents = append(resp.Agents, AgentStatus{
			Name:         a.Name,
			State:        a.State,
			SessionAlive: a.SessionAlive,
			ActiveWrit:   a.ActiveWrit,
			WorkTitle:    a.WorkTitle,
			NudgeCount:   a.NudgeCount,
		})
	}

	for _, e := range ws.Envoys {
		resp.Envoys = append(resp.Envoys, EnvoyStatus{
			Name:          e.Name,
			State:         e.State,
			SessionAlive:  e.SessionAlive,
			ActiveWrit:    e.ActiveWrit,
			WorkTitle:     e.WorkTitle,
			TetheredCount: e.TetheredCount,
			NudgeCount:    e.NudgeCount,
		})
	}

	for _, mr := range ws.MergeRequests {
		resp.MergeRequests = append(resp.MergeRequests, MergeRequestInfo{
			ID:     mr.ID,
			WritID: mr.WritID,
			Phase:  mr.Phase,
			Title:  mr.Title,
		})
	}

	for _, c := range ws.Caravans {
		resp.Caravans = append(resp.Caravans, convertCaravanInfo(c))
	}

	return resp
}

func convertForgeInfo(f internstatus.ForgeInfo) ForgeInfo {
	return ForgeInfo{
		Running:      f.Running,
		PID:          f.PID,
		Merging:      f.Merging,
		PatrolCount:  f.PatrolCount,
		QueueDepth:   f.QueueDepth,
		MergesTotal:  f.MergesTotal,
		HeartbeatAge: f.HeartbeatAge,
		Stale:        f.Stale,
		Paused:       f.Paused,
		Status:       f.Status,
		LastMerge:    f.LastMerge,
		LastError:    f.LastError,
		CurrentMR:    f.CurrentMR,
		CurrentWrit:  f.CurrentWrit,
	}
}

func convertSentinelInfo(s internstatus.SentinelInfo) SentinelInfo {
	return SentinelInfo{
		Running:       s.Running,
		PID:           s.PID,
		PatrolCount:   s.PatrolCount,
		AgentsChecked: s.AgentsChecked,
		StalledCount:  s.StalledCount,
		ReapedCount:   s.ReapedCount,
		HeartbeatAge:  s.HeartbeatAge,
		Status:        s.Status,
		Stale:         s.Stale,
	}
}
