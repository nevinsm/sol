package broker

import (
	"time"
)

// ProviderHealth represents the liveness state of the AI provider runtimes.
type ProviderHealth string

const (
	HealthHealthy ProviderHealth = "healthy"
	HealthDown    ProviderHealth = "down"
)

// ProviderHealthInfo is the exported health signal that other components read.
type ProviderHealthInfo struct {
	Health ProviderHealth `json:"health"`
	Stale  bool           `json:"stale"` // true if heartbeat is too old
}

// ReadProviderHealth reads the current provider health from the broker heartbeat.
// Returns nil if the heartbeat file doesn't exist (broker not running).
// Consumers should check Stale to determine if the signal is trustworthy.
func ReadProviderHealth() (*ProviderHealthInfo, error) {
	hb, err := ReadHeartbeat()
	if err != nil {
		return nil, err
	}
	if hb == nil {
		return nil, nil
	}

	// Treat stopping or uninitialized status as down — broker has explicitly
	// signalled it is not serving, so returning healthy would be a false signal.
	if hb.Status == "stopping" || hb.Status == "" {
		return &ProviderHealthInfo{
			Health: HealthDown,
			Stale:  true,
		}, nil
	}

	health := HealthHealthy
	if !hb.AllOK() {
		health = HealthDown
	}

	return &ProviderHealthInfo{
		Health: health,
		Stale:  hb.IsStale(10 * time.Minute),
	}, nil
}
