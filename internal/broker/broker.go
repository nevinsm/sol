package broker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/heartbeat"
)

// DefaultPatrolInterval is how often the broker probes runtime liveness.
const DefaultPatrolInterval = 5 * time.Minute

// probeTimeout is how long to wait for a runtime --version call.
const probeTimeout = 10 * time.Second

// Config holds broker configuration.
type Config struct {
	PatrolInterval time.Duration
	Runtime        string        // runtime name (default: "claude") — used when DiscoverFn is nil
	DiscoverFn     func() []string // returns runtime names in use; nil = use Runtime field
}

// RuntimeLiveness holds the liveness state for a single runtime binary.
type RuntimeLiveness struct {
	Runtime   string    `json:"runtime"`
	OK        bool      `json:"ok"`
	LastProbe time.Time `json:"last_probe,omitzero"`
}

// Heartbeat records the broker's last patrol status.
type Heartbeat struct {
	Timestamp   time.Time         `json:"timestamp"`
	PatrolCount int               `json:"patrol_count"`
	Status      string            `json:"status"` // "running", "stopping"
	Runtimes    []RuntimeLiveness `json:"runtimes,omitempty"`
}

// AllOK returns true if all runtimes reported OK on the last patrol.
// Returns false for nil or empty Runtimes: no probes is not the same as all OK.
func (h *Heartbeat) AllOK() bool {
	if len(h.Runtimes) == 0 {
		return false // no probes = not healthy
	}
	for _, r := range h.Runtimes {
		if !r.OK {
			return false
		}
	}
	return true
}

// Broker probes runtime liveness and writes heartbeats.
type Broker struct {
	cfg         Config
	logger      *events.Logger
	patrolCount int
	probeFns    map[string]func() bool // injectable for testing
}

// New creates a new Broker.
func New(cfg Config, logger *events.Logger) *Broker {
	if cfg.PatrolInterval == 0 {
		cfg.PatrolInterval = DefaultPatrolInterval
	}
	return &Broker{
		cfg:      cfg,
		logger:   logger,
		probeFns: make(map[string]func() bool),
	}
}

// SetProbeFn overrides the probe function for a specific runtime (for testing).
func (b *Broker) SetProbeFn(runtime string, fn func() bool) {
	b.probeFns[runtime] = fn
}

// Run starts the broker loop. Blocks until context is cancelled.
func (b *Broker) Run(ctx context.Context) error {
	fmt.Fprintf(os.Stderr, "Broker starting (patrol every %s)\n", b.cfg.PatrolInterval)

	// Initial patrol immediately.
	b.patrol()

	ticker := time.NewTicker(b.cfg.PatrolInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.writeHeartbeat("stopping", nil)
			fmt.Fprintln(os.Stderr, "Broker stopping")
			return nil
		case <-ticker.C:
			b.patrol()
		}
	}
}

// patrol probes all discovered runtimes and writes a heartbeat.
func (b *Broker) patrol() {
	b.patrolCount++

	runtimes := b.discoverRuntimes()
	liveness := make([]RuntimeLiveness, 0, len(runtimes))
	now := time.Now().UTC()

	for _, rt := range runtimes {
		ok := b.probeRuntime(rt)
		if !ok {
			fmt.Fprintf(os.Stderr, "broker: runtime %q liveness probe failed\n", rt)
		}
		liveness = append(liveness, RuntimeLiveness{
			Runtime:   rt,
			OK:        ok,
			LastProbe: now,
		})
	}

	// Stable sort for deterministic heartbeat output.
	sort.Slice(liveness, func(i, j int) bool {
		return liveness[i].Runtime < liveness[j].Runtime
	})

	b.writeHeartbeat("running", liveness)

	if b.logger != nil {
		b.logger.Emit(events.EventBrokerPatrol, "broker", "broker", "feed",
			map[string]any{
				"patrol_count": b.patrolCount,
				"runtimes":     liveness,
			})
	}
}

// probeRuntime runs <runtime> --version and returns true if it exits 0.
// Uses an injectable probe function if one is set (for testing).
func (b *Broker) probeRuntime(runtime string) bool {
	if fn, ok := b.probeFns[runtime]; ok {
		return fn()
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, runtime, "--version").Run() == nil
}

// discoverRuntimes returns the current set of runtime names to monitor.
func (b *Broker) discoverRuntimes() []string {
	if b.cfg.DiscoverFn != nil {
		if rts := b.cfg.DiscoverFn(); len(rts) > 0 {
			return rts
		}
	}
	rt := b.cfg.Runtime
	if rt == "" {
		rt = "claude"
	}
	return []string{rt}
}

// writeHeartbeat serialises and writes the broker heartbeat file.
func (b *Broker) writeHeartbeat(status string, runtimes []RuntimeLiveness) {
	hb := Heartbeat{
		Timestamp:   time.Now().UTC(),
		PatrolCount: b.patrolCount,
		Status:      status,
		Runtimes:    runtimes,
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

// heartbeatPath returns the path to the broker heartbeat file.
func heartbeatPath() string {
	return filepath.Join(config.RuntimeDir(), "broker-heartbeat.json")
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
