// Package broker provides the CLI API types for broker command output.
package broker

import (
	"time"

	ibroker "github.com/nevinsm/sol/internal/broker"
)

// ProviderEntry is the CLI API representation of a single provider's health state.
type ProviderEntry struct {
	Provider            string `json:"provider"`
	Health              string `json:"health"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	LastProbe           string `json:"last_probe,omitempty"`
	LastHealthy         string `json:"last_healthy,omitempty"`
}

// StatusResponse is the CLI API representation of broker status --json output.
type StatusResponse struct {
	Status              string          `json:"status"`
	Timestamp           string          `json:"timestamp"`
	PatrolCount         int             `json:"patrol_count"`
	Stale               bool            `json:"stale"`
	ProviderHealth      string          `json:"provider_health"`
	ConsecutiveFailures int             `json:"consecutive_failures"`
	LastProbe           *string         `json:"last_probe,omitempty"`
	LastHealthy         *string         `json:"last_healthy,omitempty"`
	Providers           []ProviderEntry `json:"providers,omitempty"`
}

// FromHeartbeat builds a StatusResponse from a broker.Heartbeat.
// staleDuration is the threshold for marking the heartbeat as stale.
func FromHeartbeat(hb *ibroker.Heartbeat, staleDuration time.Duration) StatusResponse {
	resp := StatusResponse{
		Status:      hb.Status,
		Timestamp:   hb.Timestamp.Format(time.RFC3339),
		PatrolCount: hb.PatrolCount,
		Stale:       hb.IsStale(staleDuration),
	}

	// Provider health (backward-compatible default).
	if hb.ProviderHealth != "" {
		resp.ProviderHealth = string(hb.ProviderHealth)
	} else {
		resp.ProviderHealth = "healthy"
	}
	resp.ConsecutiveFailures = hb.ConsecutiveFailures

	if !hb.LastProbe.IsZero() {
		s := hb.LastProbe.Format(time.RFC3339)
		resp.LastProbe = &s
	}
	if !hb.LastHealthy.IsZero() {
		s := hb.LastHealthy.Format(time.RFC3339)
		resp.LastHealthy = &s
	}

	if len(hb.Providers) > 0 {
		resp.Providers = make([]ProviderEntry, len(hb.Providers))
		for i, p := range hb.Providers {
			entry := ProviderEntry{
				Provider:            p.Provider,
				Health:              string(p.Health),
				ConsecutiveFailures: p.ConsecutiveFailures,
			}
			if !p.LastProbe.IsZero() {
				entry.LastProbe = p.LastProbe.Format(time.RFC3339)
			}
			if !p.LastHealthy.IsZero() {
				entry.LastHealthy = p.LastHealthy.Format(time.RFC3339)
			}
			resp.Providers[i] = entry
		}
	}

	return resp
}
