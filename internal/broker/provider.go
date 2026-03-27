package broker

import (
	"context"
	"time"
)

// Provider abstracts runtime-specific provider health monitoring, rate limit
// detection, and credential expiry tracking. Each AI runtime (Claude, Codex,
// etc.) registers its own Provider implementation.
type Provider interface {
	// Name returns the provider's registered name (e.g. "claude").
	Name() string

	// ProbeHealth checks whether the provider's API is reachable.
	// Returns nil if healthy, an error if unhealthy.
	ProbeHealth(ctx context.Context) error

	// DetectRateLimit scans session output for rate limit patterns.
	// Returns a *RateLimitSignal if a rate limit is detected, or nil if none.
	DetectRateLimit(output string) *RateLimitSignal

	// CredentialExpires reports whether the given credential type has an
	// expiration (e.g. oauth_token expires, api_key does not).
	CredentialExpires(credType string) bool
}

// RateLimitSignal carries parsed rate limit information extracted from
// session output.
type RateLimitSignal struct {
	Account  string        `json:"account,omitempty"`
	ResetsAt time.Time     `json:"resets_at,omitzero"`
	ResetsIn time.Duration `json:"resets_in,omitzero"`
}

var providers = map[string]Provider{}

// RegisterProvider adds a provider to the registry under the given name.
// Providers call this from their init() function.
func RegisterProvider(name string, p Provider) { providers[name] = p }

// GetProvider retrieves a provider by name. Returns false if not found.
func GetProvider(name string) (Provider, bool) { p, ok := providers[name]; return p, ok }
