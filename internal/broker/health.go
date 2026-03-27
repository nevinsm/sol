package broker

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

// ProviderHealth represents the health state of the AI provider.
type ProviderHealth string

const (
	HealthHealthy  ProviderHealth = "healthy"
	HealthDegraded ProviderHealth = "degraded"
	HealthDown     ProviderHealth = "down"
)

// Backoff schedule for the "down" state.
var downBackoffSchedule = []time.Duration{
	30 * time.Second,
	1 * time.Minute,
	2 * time.Minute,
	5 * time.Minute,
}

// DegradedProbeInterval is how often to probe when degraded.
const DegradedProbeInterval = 30 * time.Second

// HealthState tracks the provider health state machine.
type HealthState struct {
	Health              ProviderHealth `json:"provider_health"`
	ConsecutiveFailures int            `json:"consecutive_failures"`
	LastProbe           time.Time      `json:"last_probe"`
	LastHealthy         time.Time      `json:"last_healthy"`

	// downBackoffIndex tracks position in the backoff schedule.
	// Not persisted — resets on process restart (conservative: starts at shortest interval).
	downBackoffIndex int
}

// ProbeFn is the signature for a function that probes provider health.
// Returns nil if the provider is healthy, an error if unhealthy.
type ProbeFn func() error

// HealthTracker manages the provider health state machine and probe scheduling.
type HealthTracker struct {
	state   HealthState
	probeFn ProbeFn
	now     func() time.Time // injectable for testing
}

// NewHealthTracker creates a new HealthTracker that probes via the given provider.
// If provider is nil, the probe always succeeds (healthy).
func NewHealthTracker(p Provider) *HealthTracker {
	return &HealthTracker{
		state: HealthState{
			Health:      HealthHealthy,
			LastHealthy: time.Now().UTC(),
		},
		probeFn: ProbeProvider(p),
		now:     time.Now,
	}
}

// SetProbeFn overrides the probe function (for testing).
func (ht *HealthTracker) SetProbeFn(fn ProbeFn) {
	ht.probeFn = fn
}

// State returns the current health state (read-only copy).
func (ht *HealthTracker) State() HealthState {
	return ht.state
}

// ShouldProbe returns true if enough time has elapsed since the last probe
// for the current health state.
func (ht *HealthTracker) ShouldProbe(patrolInterval time.Duration) bool {
	if ht.state.LastProbe.IsZero() {
		return true // never probed
	}

	elapsed := ht.now().Sub(ht.state.LastProbe)

	switch ht.state.Health {
	case HealthHealthy:
		// Probe every patrol interval.
		return elapsed >= patrolInterval
	case HealthDegraded:
		// Probe more frequently to detect recovery.
		return elapsed >= DegradedProbeInterval
	case HealthDown:
		// Exponential backoff.
		interval := ht.currentBackoff()
		return elapsed >= interval
	default:
		return true
	}
}

// Probe runs the health probe and updates the state machine.
// Returns true if the health state changed (for event logging).
func (ht *HealthTracker) Probe() (changed bool) {
	now := ht.now().UTC()
	ht.state.LastProbe = now
	prevHealth := ht.state.Health

	err := ht.probeFn()
	if err == nil {
		// Success — back to healthy.
		ht.state.ConsecutiveFailures = 0
		ht.state.Health = HealthHealthy
		ht.state.LastHealthy = now
		ht.state.downBackoffIndex = 0
	} else {
		// Failure — advance state machine.
		ht.state.ConsecutiveFailures++
		ht.state.Health = ht.healthFromFailures(ht.state.ConsecutiveFailures)

		// Advance backoff index on each failure while down.
		if ht.state.Health == HealthDown && prevHealth == HealthDown {
			if ht.state.downBackoffIndex < len(downBackoffSchedule)-1 {
				ht.state.downBackoffIndex++
			}
		}
	}

	return ht.state.Health != prevHealth
}

// healthFromFailures maps consecutive failure count to health state.
//
//	1 failure: stay healthy (transient)
//	2-3 failures: degraded
//	4+ failures: down
func (ht *HealthTracker) healthFromFailures(failures int) ProviderHealth {
	switch {
	case failures <= 1:
		return HealthHealthy
	case failures <= 3:
		return HealthDegraded
	default:
		return HealthDown
	}
}

// currentBackoff returns the current backoff interval for the down state.
func (ht *HealthTracker) currentBackoff() time.Duration {
	if ht.state.downBackoffIndex < len(downBackoffSchedule) {
		return downBackoffSchedule[ht.state.downBackoffIndex]
	}
	return downBackoffSchedule[len(downBackoffSchedule)-1]
}

// NextProbeIn returns the duration until the next probe should run.
func (ht *HealthTracker) NextProbeIn(patrolInterval time.Duration) time.Duration {
	switch ht.state.Health {
	case HealthHealthy:
		return patrolInterval
	case HealthDegraded:
		return DegradedProbeInterval
	case HealthDown:
		return ht.currentBackoff()
	default:
		return patrolInterval
	}
}

// ProbeProvider delegates to the given Provider's ProbeHealth method.
// Falls back to a no-op (healthy) if provider is nil.
func ProbeProvider(p Provider) ProbeFn {
	if p == nil {
		return func() error { return nil }
	}
	return func() error {
		return p.ProbeHealth(context.Background())
	}
}

// ProviderHealthEntry holds the health state for a single provider.
// Used in heartbeat serialization and status display.
type ProviderHealthEntry struct {
	Provider            string         `json:"provider"`
	Health              ProviderHealth `json:"health"`
	ConsecutiveFailures int            `json:"consecutive_failures"`
	LastProbe           time.Time      `json:"last_probe,omitzero"`
	LastHealthy         time.Time      `json:"last_healthy,omitzero"`
}

// ProviderHealthInfo is the exported health signal that other components read.
type ProviderHealthInfo struct {
	Health              ProviderHealth `json:"provider_health"`
	ConsecutiveFailures int            `json:"consecutive_failures"`
	LastProbe           time.Time      `json:"last_probe"`
	LastHealthy         time.Time      `json:"last_healthy"`
	Stale               bool           `json:"stale"` // true if heartbeat is too old
}

// ReadProviderHealth reads the current provider health from the broker heartbeat.
// Returns nil if the heartbeat file doesn't exist (broker not running).
// Consumers should check Stale to determine if the signal is trustworthy.
func ReadProviderHealth() (*ProviderHealthInfo, error) {
	data, err := os.ReadFile(heartbeatPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var hb Heartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return nil, err
	}

	health := hb.ProviderHealth
	if health == "" {
		health = HealthHealthy // pre-health-probe heartbeat files
	}

	info := &ProviderHealthInfo{
		Health:              health,
		ConsecutiveFailures: hb.ConsecutiveFailures,
		LastProbe:           hb.LastProbe,
		LastHealthy:         hb.LastHealthy,
		Stale:               hb.IsStale(10 * time.Minute),
	}

	return info, nil
}

// WorstHealth returns the most severe health state from a slice of entries.
// Order: down > degraded > healthy.
func WorstHealth(entries []ProviderHealthEntry) ProviderHealth {
	worst := HealthHealthy
	for _, e := range entries {
		if healthSeverity(e.Health) > healthSeverity(worst) {
			worst = e.Health
		}
	}
	return worst
}

// healthSeverity maps health states to a severity ranking for comparison.
func healthSeverity(h ProviderHealth) int {
	switch h {
	case HealthDown:
		return 2
	case HealthDegraded:
		return 1
	default:
		return 0
	}
}
