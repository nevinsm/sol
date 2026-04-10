// Package lifecycle provides the CLI API types for sol up / sol down results.
package lifecycle

import (
	"time"
)

// UpResult is the CLI API representation of the outcome of sol up.
type UpResult struct {
	SphereDaemons []DaemonStartResult  `json:"sphere_daemons"`
	WorldServices []WorldServicesResult `json:"world_services"`
	StartedAt     time.Time            `json:"started_at"`
}

// DownResult is the CLI API representation of the outcome of sol down.
type DownResult struct {
	SphereDaemons []DaemonStopResult        `json:"sphere_daemons"`
	WorldServices []WorldServicesStopResult  `json:"world_services"`
	StoppedAt     time.Time                 `json:"stopped_at"`
}

// DaemonStartResult represents the start outcome for a single sphere daemon.
type DaemonStartResult struct {
	Name           string `json:"name"`
	Started        bool   `json:"started"`
	PID            int    `json:"pid,omitempty"`
	AlreadyRunning bool   `json:"already_running"`
	Error          string `json:"error,omitempty"`
}

// DaemonStopResult represents the stop outcome for a single sphere daemon.
type DaemonStopResult struct {
	Name       string `json:"name"`
	Stopped    bool   `json:"stopped"`
	WasRunning bool   `json:"was_running"`
	Error      string `json:"error,omitempty"`
}

// WorldServicesResult represents the start outcome for a world's services.
type WorldServicesResult struct {
	World    string               `json:"world"`
	Services []ServiceStartResult `json:"services"`
}

// ServiceStartResult represents the start outcome for a single world service.
type ServiceStartResult struct {
	Name           string `json:"name"`
	Started        bool   `json:"started"`
	AlreadyRunning bool   `json:"already_running"`
	Error          string `json:"error,omitempty"`
}

// WorldServicesStopResult represents the stop outcome for a world's services.
type WorldServicesStopResult struct {
	World    string              `json:"world"`
	Services []ServiceStopResult `json:"services"`
}

// ServiceStopResult represents the stop outcome for a single world service.
type ServiceStopResult struct {
	Name       string `json:"name"`
	Stopped    bool   `json:"stopped"`
	WasRunning bool   `json:"was_running"`
	Error      string `json:"error,omitempty"`
}
