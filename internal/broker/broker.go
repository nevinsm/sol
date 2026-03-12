package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
)

// DefaultPatrolInterval is how often the broker probes provider health.
const DefaultPatrolInterval = 5 * time.Minute

// Config holds broker configuration.
type Config struct {
	PatrolInterval time.Duration
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
}

// Broker probes provider health and writes heartbeats.
type Broker struct {
	cfg    Config
	logger *events.Logger
	health *HealthTracker

	patrolCount int
}

// New creates a new Broker.
func New(cfg Config, logger *events.Logger) *Broker {
	if cfg.PatrolInterval == 0 {
		cfg.PatrolInterval = DefaultPatrolInterval
	}

	return &Broker{
		cfg:    cfg,
		logger: logger,
		health: NewHealthTracker(),
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
			b.writeHeartbeat("stopping")
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
	b.writeHeartbeat("running")
}

// patrol performs one health check cycle: probe provider health, write heartbeat.
func (b *Broker) patrol() {
	b.patrolCount++

	// Probe provider health on every patrol.
	if b.health.ShouldProbe(b.cfg.PatrolInterval) {
		b.probeHealth()
	}

	b.writeHeartbeat("running")

	if b.logger != nil {
		b.logger.Emit(events.EventBrokerPatrol, "broker", "broker", "audit",
			map[string]any{
				"patrol_count": b.patrolCount,
			})
	}
}

// heartbeatPath returns the path to the broker heartbeat file.
func heartbeatPath() string {
	return filepath.Join(config.Home(), ".runtime", "broker-heartbeat.json")
}

func (b *Broker) writeHeartbeat(status string) {
	hs := b.health.State()
	hb := Heartbeat{
		Timestamp:           time.Now().UTC(),
		PatrolCount:         b.patrolCount,
		Status:              status,
		ProviderHealth:      hs.Health,
		ConsecutiveFailures: hs.ConsecutiveFailures,
		LastProbe:           hs.LastProbe,
		LastHealthy:         hs.LastHealthy,
	}

	data, err := json.MarshalIndent(hb, "", "  ")
	if err != nil {
		return
	}

	dir := filepath.Dir(heartbeatPath())
	os.MkdirAll(dir, 0o755)

	tmp := heartbeatPath() + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return
	}
	os.Rename(tmp, heartbeatPath())
}

// ReadHeartbeat reads the broker's heartbeat file.
// Returns nil if not found.
func ReadHeartbeat() (*Heartbeat, error) {
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
	return &hb, nil
}

// IsStale returns true if the heartbeat is older than the given max age.
func (h *Heartbeat) IsStale(maxAge time.Duration) bool {
	return time.Since(h.Timestamp) > maxAge
}
