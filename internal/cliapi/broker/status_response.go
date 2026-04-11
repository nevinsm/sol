// Package broker provides the CLI API types for broker command output.
package broker

import (
	"time"

	ibroker "github.com/nevinsm/sol/internal/broker"
)

// ProviderEntry is the CLI API representation of a single provider's health state.
type ProviderEntry struct {
	Provider            string     `json:"provider"`
	Health              string     `json:"health"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastProbeAt         *time.Time `json:"last_probe_at,omitempty"`
	LastHealthyAt       *time.Time `json:"last_healthy_at,omitempty"`
}

// StatusResponse is the CLI API representation of broker status --json output.
type StatusResponse struct {
	Status              string          `json:"status"`
	CheckedAt           time.Time       `json:"checked_at"`
	PatrolCount         int             `json:"patrol_count"`
	Stale               bool            `json:"stale"`
	ProviderHealth      string          `json:"provider_health"`
	ConsecutiveFailures int             `json:"consecutive_failures"`
	LastProbeAt         *time.Time      `json:"last_probe_at,omitempty"`
	LastHealthyAt       *time.Time      `json:"last_healthy_at,omitempty"`
	Providers           []ProviderEntry `json:"providers,omitempty"`
}

// FromHeartbeat builds a StatusResponse from a broker.Heartbeat.
// staleDuration is the threshold for marking the heartbeat as stale.
func FromHeartbeat(hb *ibroker.Heartbeat, staleDuration time.Duration) StatusResponse {
	resp := StatusResponse{
		Status:      hb.Status,
		CheckedAt:   hb.Timestamp.UTC(),
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
		t := hb.LastProbe.UTC()
		resp.LastProbeAt = &t
	}
	if !hb.LastHealthy.IsZero() {
		t := hb.LastHealthy.UTC()
		resp.LastHealthyAt = &t
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
				t := p.LastProbe.UTC()
				entry.LastProbeAt = &t
			}
			if !p.LastHealthy.IsZero() {
				t := p.LastHealthy.UTC()
				entry.LastHealthyAt = &t
			}
			resp.Providers[i] = entry
		}
	}

	return resp
}
