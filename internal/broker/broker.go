package broker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/account"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/heartbeat"
)

// DefaultPatrolInterval is how often the broker probes provider health.
const DefaultPatrolInterval = 5 * time.Minute

// Config holds broker configuration.
type Config struct {
	PatrolInterval time.Duration
	Runtime        string // provider runtime name (default: "claude")
}

// AccountTokenHealth holds token expiry status for one account.
type AccountTokenHealth struct {
	Handle    string     `json:"handle"`
	Type      string     `json:"type"`                 // "oauth_token" or "api_key"
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // nil for API keys
	Status    string     `json:"status"`               // "ok", "expiring_soon", "critical", "expired", "no_expiry", "missing"
}

// Heartbeat records the broker's last patrol status.
type Heartbeat struct {
	Timestamp   time.Time `json:"timestamp"`
	PatrolCount int       `json:"patrol_count"`
	Status      string    `json:"status"` // "running", "stopping"

	// Provider health fields.
	ProviderHealth      ProviderHealth `json:"provider_health,omitempty"`
	ConsecutiveFailures int            `json:"consecutive_failures,omitempty"`
	LastProbe           time.Time      `json:"last_probe,omitempty"`
	LastHealthy         time.Time      `json:"last_healthy,omitempty"`

	// Per-account token health.
	TokenHealth []AccountTokenHealth `json:"token_health,omitempty"`
}

// Broker probes provider health and writes heartbeats.
type Broker struct {
	cfg      Config
	logger   *events.Logger
	health   *HealthTracker
	provider Provider

	patrolCount int
}

// New creates a new Broker. It resolves the provider from the registry using
// the given runtime name. If the runtime is empty, it defaults to "claude".
// If no provider is registered for the runtime, the broker runs without a
// provider (health probes always succeed).
func New(cfg Config, logger *events.Logger) *Broker {
	if cfg.PatrolInterval == 0 {
		cfg.PatrolInterval = DefaultPatrolInterval
	}

	runtime := cfg.Runtime
	if runtime == "" {
		runtime = "claude"
	}
	provider, _ := GetProvider(runtime)

	return &Broker{
		cfg:      cfg,
		logger:   logger,
		health:   NewHealthTracker(provider),
		provider: provider,
	}
}

// SetHealthTracker overrides the health tracker (for testing).
func (b *Broker) SetHealthTracker(ht *HealthTracker) {
	b.health = ht
}

// Run starts the broker loop. Blocks until context is cancelled.
func (b *Broker) Run(ctx context.Context) error {
	fmt.Fprintf(os.Stderr, "Broker starting (patrol every %s)\n", b.cfg.PatrolInterval)

	// Initial patrol immediately (includes health probe).
	b.patrol()

	ticker := time.NewTicker(b.cfg.PatrolInterval)
	defer ticker.Stop()

	// Health probe timer — starts at patrol interval, adjusts based on health state.
	healthInterval := b.health.NextProbeIn(b.cfg.PatrolInterval)
	healthTicker := time.NewTicker(healthInterval)
	defer healthTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.writeHeartbeat("stopping", nil)
			fmt.Fprintln(os.Stderr, "Broker stopping")
			return nil
		case <-ticker.C:
			b.patrol()
			b.resetHealthTicker(healthTicker)
		case <-healthTicker.C:
			// Only run a standalone health probe if not healthy.
			// When healthy, the patrol ticker handles probing.
			if b.health.State().Health != HealthHealthy {
				b.probeHealth()
				b.resetHealthTicker(healthTicker)

				// On recovery, run a full patrol.
				if b.health.State().Health == HealthHealthy {
					fmt.Fprintln(os.Stderr, "broker: provider recovered, running patrol")
					b.patrol()
				}
			}
		}
	}
}

// resetHealthTicker adjusts the health ticker to the appropriate interval
// for the current health state.
func (b *Broker) resetHealthTicker(ticker *time.Ticker) {
	interval := b.health.NextProbeIn(b.cfg.PatrolInterval)
	ticker.Reset(interval)
}

// probeHealth runs a health probe and emits events on state transitions.
func (b *Broker) probeHealth() {
	prevHealth := b.health.State().Health
	changed := b.health.Probe()

	if changed {
		newHealth := b.health.State().Health
		fmt.Fprintf(os.Stderr, "broker: provider health changed: %s → %s (consecutive failures: %d)\n",
			prevHealth, newHealth, b.health.State().ConsecutiveFailures)

		if b.logger != nil {
			b.logger.Emit(events.EventBrokerHealthChange, "broker", "broker", "audit",
				map[string]any{
					"from":                 string(prevHealth),
					"to":                   string(newHealth),
					"consecutive_failures": b.health.State().ConsecutiveFailures,
				})
		}
	}

	// Write a heartbeat after every probe to keep health state fresh.
	b.writeHeartbeat("running", nil)
}

// patrol performs one health check cycle: probe provider health, check token
// expiry, write heartbeat.
func (b *Broker) patrol() {
	b.patrolCount++

	// Probe provider health on every patrol.
	if b.health.ShouldProbe(b.cfg.PatrolInterval) {
		b.probeHealth()
	}

	// Check token expiry for all registered accounts.
	tokenHealth := b.checkAllTokenExpiry()

	b.writeHeartbeat("running", tokenHealth)

	if b.logger != nil {
		b.logger.Emit(events.EventBrokerPatrol, "broker", "broker", "feed",
			map[string]any{
				"patrol_count": b.patrolCount,
			})
	}
}

// checkAllTokenExpiry loads the account registry and checks token expiry for
// each account. Returns a slice of AccountTokenHealth (one per account).
func (b *Broker) checkAllTokenExpiry() []AccountTokenHealth {
	registry, err := account.LoadRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "broker: failed to load account registry: %v\n", err)
		return nil
	}

	if len(registry.Accounts) == 0 {
		return nil
	}

	var tokenHealth []AccountTokenHealth
	for handle := range registry.Accounts {
		tok, err := account.ReadToken(handle)
		if err != nil {
			fmt.Fprintf(os.Stderr, "broker: failed to read token for account %q: %v\n", handle, err)
			tokenHealth = append(tokenHealth, AccountTokenHealth{
				Handle: handle,
				Type:   "unknown",
				Status: "missing",
			})
			continue
		}

		th := checkTokenExpiry(handle, tok, b.logger)
		tokenHealth = append(tokenHealth, th)
	}

	return tokenHealth
}

// checkTokenExpiry computes the token health for an account and logs expiry warnings.
// Returns an AccountTokenHealth describing the current state.
func checkTokenExpiry(handle string, tok *account.Token, logger *events.Logger) AccountTokenHealth {
	th := AccountTokenHealth{Handle: handle}

	if tok.ExpiresAt == nil {
		// API key or other credential type with no expiry.
		th.Type = tok.Type
		th.Status = "no_expiry"
		return th
	}

	th.Type = tok.Type
	th.ExpiresAt = tok.ExpiresAt
	timeLeft := time.Until(*tok.ExpiresAt)

	const (
		threshold30d = 30 * 24 * time.Hour
		threshold7d  = 7 * 24 * time.Hour
		threshold1d  = 24 * time.Hour
	)

	switch {
	case timeLeft <= 0:
		th.Status = "expired"
		fmt.Fprintf(os.Stderr, "broker: CRITICAL: token for account %q has expired\n", handle)
		if logger != nil {
			logger.Emit(events.EventBrokerTokenExpiry, "broker", handle, "audit",
				map[string]any{"account": handle, "status": "expired"})
		}
	case timeLeft <= threshold1d:
		th.Status = "critical"
		fmt.Fprintf(os.Stderr, "broker: CRITICAL: token for account %q expires tomorrow\n", handle)
		if logger != nil {
			logger.Emit(events.EventBrokerTokenExpiry, "broker", handle, "audit",
				map[string]any{"account": handle, "status": "critical", "days": 0})
		}
	case timeLeft <= threshold7d:
		th.Status = "warning"
		days := int(timeLeft.Hours() / 24)
		fmt.Fprintf(os.Stderr, "broker: WARNING: token for account %q expires in %d days\n", handle, days)
		if logger != nil {
			logger.Emit(events.EventBrokerTokenExpiry, "broker", handle, "audit",
				map[string]any{"account": handle, "status": "warning", "days": days})
		}
	case timeLeft <= threshold30d:
		th.Status = "expiring_soon"
		days := int(timeLeft.Hours() / 24)
		fmt.Fprintf(os.Stderr, "broker: token for account %q expires in %d days\n", handle, days)
		if logger != nil {
			logger.Emit(events.EventBrokerTokenExpiry, "broker", handle, "audit",
				map[string]any{"account": handle, "status": "expiring_soon", "days": days})
		}
	default:
		th.Status = "ok"
	}

	return th
}

// heartbeatPath returns the path to the broker heartbeat file.
func heartbeatPath() string {
	return filepath.Join(config.RuntimeDir(), "broker-heartbeat.json")
}

func (b *Broker) writeHeartbeat(status string, tokenHealth []AccountTokenHealth) {
	hs := b.health.State()
	hb := Heartbeat{
		Timestamp:           time.Now().UTC(),
		PatrolCount:         b.patrolCount,
		Status:              status,
		ProviderHealth:      hs.Health,
		ConsecutiveFailures: hs.ConsecutiveFailures,
		LastProbe:           hs.LastProbe,
		LastHealthy:         hs.LastHealthy,
		TokenHealth:         tokenHealth,
	}

	dir := filepath.Dir(heartbeatPath())
	os.MkdirAll(dir, 0o755)

	_ = heartbeat.Write(heartbeatPath(), hb)
}

// ReadHeartbeat reads the broker's heartbeat file.
// Returns nil if not found.
func ReadHeartbeat() (*Heartbeat, error) {
	var hb Heartbeat
	if err := heartbeat.Read(heartbeatPath(), &hb); err != nil {
		if errors.Is(err, heartbeat.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &hb, nil
}

// IsStale returns true if the heartbeat is older than the given max age.
func (h *Heartbeat) IsStale(maxAge time.Duration) bool {
	return heartbeat.IsStale(h.Timestamp, maxAge)
}
