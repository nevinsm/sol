package broker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Runtime        string // provider runtime name (default: "claude") — used when DiscoverFn is nil
	DiscoverFn     func() []string // returns runtime names in use; nil = use Runtime field
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

	// Provider health fields (backward-compatible: reflects worst provider state).
	ProviderHealth      ProviderHealth `json:"provider_health,omitempty"`
	ConsecutiveFailures int            `json:"consecutive_failures,omitempty"`
	LastProbe           time.Time      `json:"last_probe,omitzero"`
	LastHealthy         time.Time      `json:"last_healthy,omitzero"`

	// Per-provider health entries (populated when multiple providers are in use).
	Providers []ProviderHealthEntry `json:"providers,omitempty"`

	// Per-account token health.
	TokenHealth []AccountTokenHealth `json:"token_health,omitempty"`
}

// Broker probes provider health and writes heartbeats.
type Broker struct {
	cfg            Config
	logger         *events.Logger
	healthTrackers map[string]*HealthTracker // keyed by provider name

	patrolCount int
}

// New creates a new Broker. When DiscoverFn is nil, it uses the Runtime field
// (defaulting to "claude") as a single provider. Providers are resolved from
// the registry; unknown runtimes get a nil provider (probes always succeed).
func New(cfg Config, logger *events.Logger) *Broker {
	if cfg.PatrolInterval == 0 {
		cfg.PatrolInterval = DefaultPatrolInterval
	}

	b := &Broker{
		cfg:            cfg,
		logger:         logger,
		healthTrackers: make(map[string]*HealthTracker),
	}

	// Bootstrap with initial provider set.
	runtimes := b.discoverRuntimes()
	for _, rt := range runtimes {
		b.ensureTracker(rt)
	}

	return b
}

// SetHealthTracker overrides the health tracker for a specific provider (for testing).
// If only one tracker exists, it replaces that one regardless of name.
func (b *Broker) SetHealthTracker(ht *HealthTracker) {
	if len(b.healthTrackers) == 1 {
		for name := range b.healthTrackers {
			b.healthTrackers[name] = ht
			return
		}
	}
	// Fallback: set as the default "claude" tracker.
	b.healthTrackers["claude"] = ht
}

// SetHealthTrackerFor sets the health tracker for a specific provider name (for testing).
func (b *Broker) SetHealthTrackerFor(name string, ht *HealthTracker) {
	b.healthTrackers[name] = ht
}

// Run starts the broker loop. Blocks until context is cancelled.
func (b *Broker) Run(ctx context.Context) error {
	fmt.Fprintf(os.Stderr, "Broker starting (patrol every %s)\n", b.cfg.PatrolInterval)

	// Initial patrol immediately (includes health probe).
	b.patrol()

	ticker := time.NewTicker(b.cfg.PatrolInterval)
	defer ticker.Stop()

	// Health probe timer — starts at patrol interval, adjusts based on health state.
	healthInterval := b.minNextProbeIn()
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
			// Probe any non-healthy providers that need probing.
			anyProbed := false
			for _, ht := range b.healthTrackers {
				if ht.State().Health != HealthHealthy {
					anyProbed = true
					b.probeHealthTracker(ht)
				}
			}
			if anyProbed {
				b.resetHealthTicker(healthTicker)

				// On recovery of all providers, run a full patrol.
				allHealthy := true
				for _, ht := range b.healthTrackers {
					if ht.State().Health != HealthHealthy {
						allHealthy = false
						break
					}
				}
				if allHealthy {
					fmt.Fprintln(os.Stderr, "broker: all providers recovered, running patrol")
					b.patrol()
				}
			}
		}
	}
}

// discoverRuntimes returns the current set of runtime names to monitor.
// Uses DiscoverFn if set, otherwise falls back to the Runtime field.
func (b *Broker) discoverRuntimes() []string {
	if b.cfg.DiscoverFn != nil {
		runtimes := b.cfg.DiscoverFn()
		if len(runtimes) > 0 {
			return runtimes
		}
	}

	rt := b.cfg.Runtime
	if rt == "" {
		rt = "claude"
	}
	return []string{rt}
}

// ensureTracker creates a HealthTracker for the given runtime if one doesn't exist.
func (b *Broker) ensureTracker(runtime string) {
	if _, ok := b.healthTrackers[runtime]; ok {
		return
	}
	provider, _ := GetProvider(runtime)
	b.healthTrackers[runtime] = NewHealthTracker(provider)
}

// syncTrackers ensures trackers exist for all discovered runtimes.
// Does not remove trackers for runtimes that are no longer in use
// (conservative: keeps monitoring until broker restarts).
func (b *Broker) syncTrackers() {
	runtimes := b.discoverRuntimes()
	for _, rt := range runtimes {
		b.ensureTracker(rt)
	}
}

// minNextProbeIn returns the minimum NextProbeIn across all health trackers.
// This determines when the health ticker fires next (most urgent provider).
func (b *Broker) minNextProbeIn() time.Duration {
	min := b.cfg.PatrolInterval
	for _, ht := range b.healthTrackers {
		d := ht.NextProbeIn(b.cfg.PatrolInterval)
		if d < min {
			min = d
		}
	}
	return min
}

// resetHealthTicker adjusts the health ticker to the appropriate interval
// for the current health state across all providers.
func (b *Broker) resetHealthTicker(ticker *time.Ticker) {
	interval := b.minNextProbeIn()
	ticker.Reset(interval)
}

// probeHealthTracker runs a health probe for a single tracker and emits events on transitions.
func (b *Broker) probeHealthTracker(ht *HealthTracker) {
	prevHealth := ht.State().Health
	changed := ht.Probe()

	if changed {
		newHealth := ht.State().Health
		// Find the provider name for this tracker.
		providerName := b.trackerName(ht)
		fmt.Fprintf(os.Stderr, "broker: provider %q health changed: %s → %s (consecutive failures: %d)\n",
			providerName, prevHealth, newHealth, ht.State().ConsecutiveFailures)

		if b.logger != nil {
			b.logger.Emit(events.EventBrokerHealthChange, "broker", "broker", "audit",
				map[string]any{
					"provider":             providerName,
					"from":                 string(prevHealth),
					"to":                   string(newHealth),
					"consecutive_failures": ht.State().ConsecutiveFailures,
				})
		}
	}
}

// trackerName returns the provider name for a given health tracker.
func (b *Broker) trackerName(ht *HealthTracker) string {
	for name, t := range b.healthTrackers {
		if t == ht {
			return name
		}
	}
	return "unknown"
}

// patrol performs one health check cycle: discover providers, probe health,
// check token expiry, write heartbeat.
func (b *Broker) patrol() {
	b.patrolCount++

	// Re-discover providers on each patrol cycle.
	b.syncTrackers()

	// Probe all providers that are due.
	for _, ht := range b.healthTrackers {
		if ht.ShouldProbe(b.cfg.PatrolInterval) {
			b.probeHealthTracker(ht)
		}
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

// providerHealthEntries builds the per-provider health entries from current trackers.
func (b *Broker) providerHealthEntries() []ProviderHealthEntry {
	entries := make([]ProviderHealthEntry, 0, len(b.healthTrackers))
	for name, ht := range b.healthTrackers {
		hs := ht.State()
		entries = append(entries, ProviderHealthEntry{
			Provider:            name,
			Health:              hs.Health,
			ConsecutiveFailures: hs.ConsecutiveFailures,
			LastProbe:           hs.LastProbe,
			LastHealthy:         hs.LastHealthy,
		})
	}
	// Stable sort for deterministic heartbeat output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Provider < entries[j].Provider
	})
	return entries
}

func (b *Broker) writeHeartbeat(status string, tokenHealth []AccountTokenHealth) {
	entries := b.providerHealthEntries()

	// Compute backward-compatible top-level fields from worst provider state.
	worstHealth := WorstHealth(entries)
	var worstFailures int
	var latestProbe, latestHealthy time.Time
	for _, e := range entries {
		if e.ConsecutiveFailures > worstFailures {
			worstFailures = e.ConsecutiveFailures
		}
		if e.LastProbe.After(latestProbe) {
			latestProbe = e.LastProbe
		}
		if e.LastHealthy.After(latestHealthy) {
			latestHealthy = e.LastHealthy
		}
	}

	hb := Heartbeat{
		Timestamp:           time.Now().UTC(),
		PatrolCount:         b.patrolCount,
		Status:              status,
		ProviderHealth:      worstHealth,
		ConsecutiveFailures: worstFailures,
		LastProbe:           latestProbe,
		LastHealthy:         latestHealthy,
		TokenHealth:         tokenHealth,
	}

	// Only include per-provider entries when multiple providers are tracked.
	if len(entries) > 1 {
		hb.Providers = entries
	}

	dir := filepath.Dir(heartbeatPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "broker: failed to create heartbeat dir: %v\n", err)
		return
	}

	if err := heartbeat.Write(heartbeatPath(), hb); err != nil {
		fmt.Fprintf(os.Stderr, "broker: failed to write heartbeat: %v\n", err)
	}
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

// DiscoverWorldRuntimes scans all world configs under SOL_HOME and returns
// the deduplicated set of runtime names in use. Falls back to ["claude"]
// if no worlds or runtimes are found.
func DiscoverWorldRuntimes() []string {
	home := config.Home()
	entries, err := os.ReadDir(home)
	if err != nil {
		return []string{"claude"}
	}

	seen := map[string]bool{}
	roles := []string{"outpost", "envoy", "forge"}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip hidden directories and runtime directory.
		if len(name) > 0 && name[0] == '.' {
			continue
		}
		// Only process directories that have a world.toml.
		worldPath := config.WorldConfigPath(name)
		if _, err := os.Stat(worldPath); err != nil {
			continue
		}

		worldCfg, err := config.LoadWorldConfig(name)
		if err != nil {
			continue
		}

		for _, role := range roles {
			rt := worldCfg.ResolveRuntime(role)
			seen[rt] = true
		}
	}

	if len(seen) == 0 {
		return []string{"claude"}
	}

	runtimes := make([]string, 0, len(seen))
	for rt := range seen {
		runtimes = append(runtimes, rt)
	}
	sort.Strings(runtimes)
	return runtimes
}
