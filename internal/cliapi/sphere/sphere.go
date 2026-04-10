// Package sphere provides the CLI API types for sphere and per-world status.
package sphere

import (
	"time"
)

// SphereStatus is the CLI API representation of the full sphere state.
type SphereStatus struct {
	SOLHome     string            `json:"sol_home"`
	Health      string            `json:"health"`
	Prefect     ProcessInfo       `json:"prefect"`
	Consul      ProcessInfo       `json:"consul"`
	Chronicle   ProcessInfo       `json:"chronicle"`
	Ledger      ProcessInfo       `json:"ledger"`
	Broker      ProcessInfo       `json:"broker"`
	Worlds      []WorldStatus     `json:"worlds"`
	Caravans    []CaravanSummary  `json:"caravans,omitempty"`
	Escalations *EscalationCount  `json:"escalations,omitempty"`
	MailCount   int               `json:"mail_count,omitempty"`
}

// ProcessInfo holds the running state of a sphere-level process.
type ProcessInfo struct {
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

// WorldStatus is the CLI API representation of per-world status within the sphere.
type WorldStatus struct {
	Name       string `json:"name"`
	SourceRepo string `json:"source_repo,omitempty"`
	Health     string `json:"health"`
	Sleeping   bool   `json:"sleeping,omitempty"`
	Agents     int    `json:"agents"`
	Working    int    `json:"working"`
	Idle       int    `json:"idle"`
	Stalled    int    `json:"stalled"`
	Dead       int    `json:"dead"`
	Forge      bool   `json:"forge"`
	Sentinel   bool   `json:"sentinel"`
	MRReady    int    `json:"mr_ready"`
	MRFailed   int    `json:"mr_failed"`
}

// CaravanSummary is a condensed caravan view for the sphere status.
type CaravanSummary struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	ItemsTotal  int        `json:"items_total"`
	ItemsMerged int        `json:"items_merged"`
	CreatedAt   time.Time  `json:"created_at"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
}

// EscalationCount holds escalation totals for the sphere status.
type EscalationCount struct {
	Total      int            `json:"total"`
	BySeverity map[string]int `json:"by_severity"`
}
