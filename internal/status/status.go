package status

import (
	"fmt"

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/store"
	"github.com/nevinsm/gt/internal/supervisor"
)

// RigStatus holds the complete runtime state for a rig.
type RigStatus struct {
	Rig        string         `json:"rig"`
	Supervisor SupervisorInfo `json:"supervisor"`
	Agents     []AgentStatus  `json:"agents"`
	Summary    Summary        `json:"summary"`
}

// SupervisorInfo holds supervisor process state (town-level, not per-rig).
type SupervisorInfo struct {
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

// AgentStatus holds the combined state of one agent.
type AgentStatus struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	SessionAlive bool   `json:"session_alive"`
	HookItem     string `json:"hook_item,omitempty"`
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
// 2 = degraded (supervisor not running)
func (r *RigStatus) Health() int {
	if !r.Supervisor.Running {
		return 2
	}
	if r.Summary.Dead > 0 {
		return 1
	}
	return 0
}

// HealthString returns the health level as a string.
func (r *RigStatus) HealthString() string {
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

// RigStore abstracts work item lookups for testing.
type RigStore interface {
	GetWorkItem(id string) (*store.WorkItem, error)
}

// TownStore abstracts agent queries for testing.
type TownStore interface {
	ListAgents(rig string, state string) ([]store.Agent, error)
}

// Gather collects runtime state for a rig.
func Gather(rig string, townStore TownStore, rigStore RigStore, checker SessionChecker) (*RigStatus, error) {
	result := &RigStatus{Rig: rig}

	// 1. Check supervisor (town-level).
	pid, err := supervisor.ReadPID()
	if err != nil {
		return nil, fmt.Errorf("failed to read supervisor PID: %w", err)
	}
	if pid != 0 && supervisor.IsRunning(pid) {
		result.Supervisor = SupervisorInfo{Running: true, PID: pid}
	}

	// 2. List all agents for this rig.
	agents, err := townStore.ListAgents(rig, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	// 3. Build agent statuses.
	for _, agent := range agents {
		as := AgentStatus{
			Name:  agent.Name,
			State: agent.State,
		}

		// Check session liveness.
		sessName := dispatch.SessionName(rig, agent.Name)
		as.SessionAlive = checker.Exists(sessName)

		// Look up hooked work item title.
		if agent.HookItem != "" {
			as.HookItem = agent.HookItem
			item, err := rigStore.GetWorkItem(agent.HookItem)
			if err != nil {
				as.WorkTitle = "(unknown)"
			} else {
				as.WorkTitle = item.Title
			}
		}

		result.Agents = append(result.Agents, as)
	}

	// 4. Compute summary counts.
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

	return result, nil
}
