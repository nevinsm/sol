package status

import (
	"fmt"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/prefect"
)

// WorldStatus holds the complete runtime state for a world.
type WorldStatus struct {
	World      string         `json:"world"`
	Prefect    PrefectInfo    `json:"prefect"`
	Forge      ForgeInfo      `json:"forge"`
	Chronicle  ChronicleInfo  `json:"chronicle"`
	Sentinel   SentinelInfo   `json:"sentinel"`
	Agents     []AgentStatus  `json:"agents"`
	MergeQueue MergeQueueInfo `json:"merge_queue"`
	Caravans   []CaravanInfo  `json:"caravans,omitempty"`
	Summary    Summary        `json:"summary"`
}

// CaravanInfo holds summary information about a caravan relevant to a world.
type CaravanInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	TotalItems int    `json:"total_items"`
	ReadyItems int    `json:"ready_items"`
	DoneItems  int    `json:"done_items"`
}

// PrefectInfo holds prefect process state (sphere-level, not per-world).
type PrefectInfo struct {
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

// ForgeInfo holds forge process state.
type ForgeInfo struct {
	Running     bool   `json:"running"`
	SessionName string `json:"session_name,omitempty"`
}

// ChronicleInfo holds chronicle process state (sphere-level).
type ChronicleInfo struct {
	Running     bool   `json:"running"`
	SessionName string `json:"session_name,omitempty"`
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

// AgentStatus holds the combined state of one agent.
type AgentStatus struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	SessionAlive bool   `json:"session_alive"`
	TetherItem     string `json:"tether_item,omitempty"`
	WorkTitle    string `json:"work_title,omitempty"`
}

// Summary holds aggregate counts.
type Summary struct {
	Total   int `json:"total"`
	Working int `json:"working"`
	Idle    int `json:"idle"`
	Stalled int `json:"stalled"`
	Dead    int `json:"dead"`
}

// Health returns the overall health level.
// 0 = healthy (all sessions alive or idle)
// 1 = unhealthy (at least one dead session)
// 2 = degraded (prefect not running)
// Forge state does not affect health — an absent forge just means
// merges won't happen, the system is still operational.
func (r *WorldStatus) Health() int {
	if !r.Prefect.Running {
		return 2
	}
	if r.Summary.Dead > 0 {
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

// WorldStore abstracts work item lookups for testing.
type WorldStore interface {
	GetWorkItem(id string) (*store.WorkItem, error)
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

// SphereStatus holds the complete runtime state for the sphere.
type SphereStatus struct {
	SOLHome   string         `json:"sol_home"`
	Prefect   PrefectInfo    `json:"prefect"`
	Consul    ConsulInfo     `json:"consul"`
	Chronicle ChronicleInfo  `json:"chronicle"`
	Worlds    []WorldSummary `json:"worlds"`
	Caravans  []CaravanInfo  `json:"caravans,omitempty"`
	Health    string         `json:"health"`
}

// ConsulInfo holds consul process state.
type ConsulInfo struct {
	Running      bool   `json:"running"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	PatrolCount  int    `json:"patrol_count,omitempty"`
	Stale        bool   `json:"stale"`
}

// WorldSummary holds a condensed view of one world for the sphere overview.
type WorldSummary struct {
	Name       string `json:"name"`
	SourceRepo string `json:"source_repo,omitempty"`
	Agents     int    `json:"agents"`
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

	// 2. Check forge session.
	forgeSessName := dispatch.SessionName(world, "forge")
	if checker.Exists(forgeSessName) {
		result.Forge = ForgeInfo{Running: true, SessionName: forgeSessName}
	}

	// 2b. Check chronicle session (sphere-level).
	const chronicleSessionName = "sol-chronicle"
	if checker.Exists(chronicleSessionName) {
		result.Chronicle = ChronicleInfo{Running: true, SessionName: chronicleSessionName}
	}

	// 2c. Check sentinel session.
	sentinelSessName := dispatch.SessionName(world, "sentinel")
	if checker.Exists(sentinelSessName) {
		result.Sentinel = SentinelInfo{Running: true, SessionName: sentinelSessName}
	}

	// 3. List all agents for this world.
	agents, err := sphereStore.ListAgents(world, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	// 4. Build agent statuses.
	for _, agent := range agents {
		as := AgentStatus{
			Name:  agent.Name,
			State: agent.State,
		}

		// Check session liveness.
		sessName := dispatch.SessionName(world, agent.Name)
		as.SessionAlive = checker.Exists(sessName)

		// Look up tethered work item title.
		if agent.TetherItem != "" {
			as.TetherItem = agent.TetherItem
			item, err := worldStore.GetWorkItem(agent.TetherItem)
			if err != nil {
				as.WorkTitle = "(unknown)"
			} else {
				as.WorkTitle = item.Title
			}
		}

		result.Agents = append(result.Agents, as)
	}

	// 5. Compute summary counts.
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
		switch mr.Phase {
		case "ready":
			result.MergeQueue.Ready++
		case "claimed":
			result.MergeQueue.Claimed++
		case "failed":
			result.MergeQueue.Failed++
		case "merged":
			result.MergeQueue.Merged++
		}
	}

	return result, nil
}

// GatherCaravans adds caravan information to a WorldStatus.
// This is separate from Gather because it requires the CaravanStore interface
// which not all callers may have available.
func GatherCaravans(result *WorldStatus, caravanStore CaravanStore, worldOpener func(string) (*store.Store, error)) {
	caravans, err := caravanStore.ListCaravans("open")
	if err != nil {
		return // non-fatal: degrade gracefully
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
