package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nevinsm/sol/internal/fileutil"
)

// WorldConfig holds all configuration for a world.
type WorldConfig struct {
	World      WorldSection      `toml:"world" json:"world"`
	Agents     AgentsSection     `toml:"agents" json:"agents"`
	Sphere     SphereSection     `toml:"sphere" json:"sphere"`
	Forge      ForgeSection      `toml:"forge" json:"forge"`
	Ledger     LedgerSection     `toml:"ledger" json:"ledger"`
	WritClean  WritCleanSection  `toml:"writ-clean" json:"writ-clean"`
	Pricing    PricingConfig     `toml:"pricing" json:"pricing,omitempty"`
	Escalation EscalationSection `toml:"escalation" json:"escalation"`
	Sitrep     SitrepSection     `toml:"sitrep" json:"sitrep"`
}

// SphereSection holds sphere-level settings.
// Configured only in sol.toml (not world.toml).
type SphereSection struct {
	MaxSessions int `toml:"max_sessions" json:"max_sessions"` // 0 = unlimited
}

// EscalationSection holds escalation management settings (sphere-level).
// Configured in sol.toml under [escalation].
type EscalationSection struct {
	AgingCritical       string `toml:"aging_critical" json:"aging_critical"`              // re-notify threshold for critical (default: "30m")
	AgingHigh           string `toml:"aging_high" json:"aging_high"`                      // re-notify threshold for high (default: "2h")
	AgingMedium         string `toml:"aging_medium" json:"aging_medium"`                  // re-notify threshold for medium (default: "8h")
	EscalationThreshold int    `toml:"escalation_threshold" json:"escalation_threshold"` // buildup alert threshold (default: 5)
}

// DefaultEscalationConfig returns an EscalationSection with built-in defaults.
func DefaultEscalationConfig() EscalationSection {
	return EscalationSection{
		AgingCritical:       "30m",
		AgingHigh:           "2h",
		AgingMedium:         "8h",
		EscalationThreshold: 5,
	}
}

// AgingThreshold returns the parsed aging threshold for a severity level.
// Returns zero duration for "low" severity (never re-notified).
// Returns error for invalid duration strings.
func (e EscalationSection) AgingThreshold(severity string) (time.Duration, error) {
	var raw string
	switch severity {
	case "critical":
		raw = e.AgingCritical
	case "high":
		raw = e.AgingHigh
	case "medium":
		raw = e.AgingMedium
	case "low":
		return 0, nil // low severity never re-notified
	default:
		return 0, fmt.Errorf("unknown severity %q", severity)
	}
	if raw == "" {
		defaults := DefaultEscalationConfig()
		switch severity {
		case "critical":
			raw = defaults.AgingCritical
		case "high":
			raw = defaults.AgingHigh
		case "medium":
			raw = defaults.AgingMedium
		}
	}
	return time.ParseDuration(raw)
}

// WorldSection holds world-level settings.
type WorldSection struct {
	SourceRepo        string   `toml:"source_repo" json:"source_repo"`
	Branch            string   `toml:"branch" json:"branch"`
	ProtectedBranches []string `toml:"protected_branches" json:"protected_branches"`
	Sleeping          bool     `toml:"sleeping,omitempty" json:"sleeping,omitempty"`
	DefaultAccount    string   `toml:"default_account,omitempty" json:"default_account,omitempty"`
}

// ModelsSection holds per-role model overrides for agents.
// Each field overrides the top-level model_tier for that specific role.
// Valid values are "sonnet", "opus", or "haiku". Empty means no override.
type ModelsSection struct {
	Outpost    string `toml:"outpost,omitempty" json:"outpost,omitempty"`
	Envoy      string `toml:"envoy,omitempty" json:"envoy,omitempty"`
	Governor   string `toml:"governor,omitempty" json:"governor,omitempty"`
	Forge      string `toml:"forge,omitempty" json:"forge,omitempty"`
	Chancellor string `toml:"chancellor,omitempty" json:"chancellor,omitempty"`
}

// RuntimesSection holds per-role runtime overrides.
// Each field overrides agents.default_runtime for that specific role.
// Valid values are "claude". Empty means no override (falls back to default_runtime).
type RuntimesSection struct {
	Outpost    string `toml:"outpost,omitempty" json:"outpost,omitempty"`
	Envoy      string `toml:"envoy,omitempty" json:"envoy,omitempty"`
	Governor   string `toml:"governor,omitempty" json:"governor,omitempty"`
	Forge      string `toml:"forge,omitempty" json:"forge,omitempty"`
	Chancellor string `toml:"chancellor,omitempty" json:"chancellor,omitempty"`
}

// AgentsSection holds agent-related settings.
type AgentsSection struct {
	Capacity       int             `toml:"capacity" json:"capacity"`                                   // Deprecated: use MaxActive. 0 = unlimited.
	MaxActive      int             `toml:"max_active" json:"max_active"`                               // 0 = unlimited
	NamePoolPath   string          `toml:"name_pool_path" json:"name_pool_path"`                       // empty = embedded default
	ModelTier      string          `toml:"model_tier" json:"model_tier"`                               // "sonnet", "opus", "haiku"
	Models         ModelsSection   `toml:"models,omitempty" json:"models,omitempty"`                   // per-role model overrides
	DefaultRuntime string          `toml:"default_runtime,omitempty" json:"default_runtime,omitempty"` // e.g. "claude"
	Runtimes       RuntimesSection `toml:"runtimes,omitempty" json:"runtimes,omitempty"`               // per-role runtime overrides
}

// ForgeSection holds forge/merge pipeline settings.
type ForgeSection struct {
	QualityGates []string `toml:"quality_gates" json:"quality_gates"`
	GateTimeout  string   `toml:"gate_timeout" json:"gate_timeout"` // duration string, e.g. "5m"
}

// LedgerSection holds ledger/telemetry settings.
type LedgerSection struct {
	Port int `toml:"port" json:"port"` // OTLP receiver port; 0 = disabled
}

// WritCleanSection holds writ output directory cleanup settings.
type WritCleanSection struct {
	RetentionDays int `toml:"retention_days" json:"retention_days"` // 0 = use default (15)
}

// SitrepSection holds sitrep AI assessment settings.
type SitrepSection struct {
	Model         string `toml:"model" json:"model"`                   // Claude model ID (default: "claude-sonnet-4-6")
	AssessCommand string `toml:"assess_command" json:"assess_command"` // base command (default: "claude")
	Timeout       string `toml:"timeout" json:"timeout"`               // duration string (default: "60s")
}

// DefaultWorldConfig returns a WorldConfig with built-in defaults.
func DefaultWorldConfig() WorldConfig {
	return WorldConfig{
		World: WorldSection{
			Branch: "main",
		},
		Agents: AgentsSection{
			ModelTier: "sonnet",
		},
		Forge: ForgeSection{
			GateTimeout: "5m",
		},
		Ledger: LedgerSection{
			Port: 4318, // ledger.DefaultPort — sphere-scoped, configurable in sol.toml
		},
		Escalation: DefaultEscalationConfig(),
		Sitrep: SitrepSection{
			Model:         "claude-sonnet-4-6",
			AssessCommand: "claude",
			Timeout:       "60s",
		},
	}
}

// WorldConfigPath returns the path to a world's config file.
func WorldConfigPath(world string) string {
	return filepath.Join(Home(), world, "world.toml")
}

// GlobalConfigPath returns the path to the global sol config file.
func GlobalConfigPath() string {
	return filepath.Join(Home(), "sol.toml")
}

// LoadWorldConfig loads configuration by layering:
// defaults → sol.toml → world.toml.
// Missing files are not an error.
func LoadWorldConfig(world string) (WorldConfig, error) {
	cfg := DefaultWorldConfig()

	// Layer global config.
	globalPath := GlobalConfigPath()
	if _, err := os.Stat(globalPath); err == nil {
		if _, err := toml.DecodeFile(globalPath, &cfg); err != nil {
			return cfg, fmt.Errorf("failed to parse %s: %w", globalPath, err)
		}
	} else if !os.IsNotExist(err) {
		return cfg, fmt.Errorf("failed to check %s: %w", globalPath, err)
	}

	// Layer world config.
	worldPath := WorldConfigPath(world)
	if _, err := os.Stat(worldPath); err == nil {
		if _, err := toml.DecodeFile(worldPath, &cfg); err != nil {
			return cfg, fmt.Errorf("failed to parse %s: %w", worldPath, err)
		}
	} else if !os.IsNotExist(err) {
		return cfg, fmt.Errorf("failed to check %s: %w", worldPath, err)
	}

	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid world config for %q: %w", world, err)
	}
	return cfg, nil
}

// ResolveModel returns the model tier for a given role.
// Checks agents.models.<role> first, falls back to agents.model_tier,
// then to "sonnet" as the hardcoded default.
func (c WorldConfig) ResolveModel(role string) string {
	var override string
	switch role {
	case "outpost", "agent":
		override = c.Agents.Models.Outpost
	case "envoy":
		override = c.Agents.Models.Envoy
	case "governor":
		override = c.Agents.Models.Governor
	case "forge", "forge-merge":
		override = c.Agents.Models.Forge
	case "chancellor":
		override = c.Agents.Models.Chancellor
	}
	if override != "" {
		return override
	}
	if c.Agents.ModelTier != "" {
		return c.Agents.ModelTier
	}
	return "sonnet"
}

// ResolveRuntime returns the runtime adapter name for the given role.
// Checks agents.runtimes.<role> first, falls back to agents.default_runtime,
// then to "claude" as the hardcoded default.
// In v1, only "claude" is a valid value.
func (c WorldConfig) ResolveRuntime(role string) string {
	var override string
	switch role {
	case "outpost", "agent":
		override = c.Agents.Runtimes.Outpost
	case "envoy":
		override = c.Agents.Runtimes.Envoy
	case "governor":
		override = c.Agents.Runtimes.Governor
	case "forge", "forge-merge":
		override = c.Agents.Runtimes.Forge
	case "chancellor":
		override = c.Agents.Runtimes.Chancellor
	}
	if override != "" {
		return override
	}
	if c.Agents.DefaultRuntime != "" {
		return c.Agents.DefaultRuntime
	}
	return "claude"
}

// validModelTier returns an error if the given model tier is not a valid
// non-empty value. Empty string is treated as "not set" and is valid.
func validModelTier(field, value string) error {
	switch value {
	case "", "sonnet", "opus", "haiku":
		return nil
	default:
		return fmt.Errorf("%s must be sonnet, opus, or haiku; got %q", field, value)
	}
}

// Validate checks that config values are within acceptable ranges.
func (c WorldConfig) Validate() error {
	if c.World.SourceRepo != "" && c.World.Branch == "" {
		return fmt.Errorf("world.branch must be non-empty when world.source_repo is set")
	}
	if c.Agents.Capacity < 0 {
		return fmt.Errorf("agents.capacity must be >= 0, got %d", c.Agents.Capacity)
	}
	if c.Agents.MaxActive < 0 {
		return fmt.Errorf("agents.max_active must be >= 0, got %d", c.Agents.MaxActive)
	}
	if c.Sphere.MaxSessions < 0 {
		return fmt.Errorf("sphere.max_sessions must be >= 0, got %d", c.Sphere.MaxSessions)
	}
	if err := validModelTier("agents.model_tier", c.Agents.ModelTier); err != nil {
		return err
	}
	if err := validModelTier("agents.models.outpost", c.Agents.Models.Outpost); err != nil {
		return err
	}
	if err := validModelTier("agents.models.envoy", c.Agents.Models.Envoy); err != nil {
		return err
	}
	if err := validModelTier("agents.models.governor", c.Agents.Models.Governor); err != nil {
		return err
	}
	if err := validModelTier("agents.models.forge", c.Agents.Models.Forge); err != nil {
		return err
	}
	if err := validModelTier("agents.models.chancellor", c.Agents.Models.Chancellor); err != nil {
		return err
	}
	if c.Forge.GateTimeout != "" {
		if _, err := time.ParseDuration(c.Forge.GateTimeout); err != nil {
			return fmt.Errorf("forge.gate_timeout %q is not a valid duration: %w", c.Forge.GateTimeout, err)
		}
	}
	if c.Sitrep.Timeout != "" {
		if _, err := time.ParseDuration(c.Sitrep.Timeout); err != nil {
			return fmt.Errorf("sitrep.timeout %q is not a valid duration: %w", c.Sitrep.Timeout, err)
		}
	}
	if c.Escalation.AgingCritical != "" {
		if _, err := time.ParseDuration(c.Escalation.AgingCritical); err != nil {
			return fmt.Errorf("escalation.aging_critical %q is not a valid duration: %w", c.Escalation.AgingCritical, err)
		}
	}
	if c.Escalation.AgingHigh != "" {
		if _, err := time.ParseDuration(c.Escalation.AgingHigh); err != nil {
			return fmt.Errorf("escalation.aging_high %q is not a valid duration: %w", c.Escalation.AgingHigh, err)
		}
	}
	if c.Escalation.AgingMedium != "" {
		if _, err := time.ParseDuration(c.Escalation.AgingMedium); err != nil {
			return fmt.Errorf("escalation.aging_medium %q is not a valid duration: %w", c.Escalation.AgingMedium, err)
		}
	}
	return nil
}

// DeprecationWarnings returns human-readable warnings for deprecated config
// fields that are still set. Callers should print these to stderr so they
// are visible to users without corrupting stdout output.
func (c WorldConfig) DeprecationWarnings() []string {
	var warnings []string
	if c.Agents.Capacity != 0 && c.Agents.MaxActive == 0 {
		warnings = append(warnings, "agents.capacity is deprecated, use agents.max_active instead")
	}
	return warnings
}

// LoadGlobalConfig loads sphere-level configuration from sol.toml.
// Returns defaults if sol.toml does not exist.
func LoadGlobalConfig() (WorldConfig, error) {
	cfg := DefaultWorldConfig()

	globalPath := GlobalConfigPath()
	if _, err := os.Stat(globalPath); err == nil {
		if _, err := toml.DecodeFile(globalPath, &cfg); err != nil {
			return cfg, fmt.Errorf("failed to parse %s: %w", globalPath, err)
		}
	} else if !os.IsNotExist(err) {
		return cfg, fmt.Errorf("failed to check %s: %w", globalPath, err)
	}

	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("invalid global config: %w", err)
	}
	return cfg, nil
}

// LoadSphereConfig loads sphere-level configuration from sol.toml.
// The [sphere] section is only loaded from sol.toml (not world.toml).
// Returns a zero SphereSection if sol.toml does not exist.
func LoadSphereConfig() (SphereSection, error) {
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return SphereSection{}, err
	}
	return cfg.Sphere, nil
}

// IsSleeping returns true if the world is marked as sleeping in its config.
// Returns true if the config cannot be loaded (fail-closed) — a malformed
// world.toml should not allow the world to continue operating.
func IsSleeping(world string) bool {
	cfg, err := LoadWorldConfig(world)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: treating world %q as sleeping due to config error: %v\n", world, err)
		return true
	}
	return cfg.World.Sleeping
}

// WriteWorldConfig writes a world's configuration to world.toml.
// The write is atomic: data is written to a temp file first, then renamed.
func WriteWorldConfig(world string, cfg WorldConfig) error {
	path := WorldConfigPath(world)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory %q: %w", dir, err)
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return fileutil.AtomicWrite(path, buf.Bytes(), 0o644)
}
