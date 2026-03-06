package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// WorldConfig holds all configuration for a world.
type WorldConfig struct {
	World  WorldSection  `toml:"world" json:"world"`
	Agents AgentsSection `toml:"agents" json:"agents"`
	Forge  ForgeSection  `toml:"forge" json:"forge"`
}

// WorldSection holds world-level settings.
type WorldSection struct {
	SourceRepo     string `toml:"source_repo" json:"source_repo"`
	Sleeping       bool   `toml:"sleeping,omitempty" json:"sleeping,omitempty"`
	DefaultAccount string `toml:"default_account,omitempty" json:"default_account,omitempty"`
}

// AgentsSection holds agent-related settings.
type AgentsSection struct {
	Capacity     int    `toml:"capacity" json:"capacity"`               // 0 = unlimited
	NamePoolPath string `toml:"name_pool_path" json:"name_pool_path"`   // empty = embedded default
	ModelTier    string `toml:"model_tier" json:"model_tier"`           // "sonnet", "opus", "haiku"
}

// ForgeSection holds forge/merge pipeline settings.
type ForgeSection struct {
	TargetBranch string   `toml:"target_branch" json:"target_branch"`
	QualityGates []string `toml:"quality_gates" json:"quality_gates"`
	GateTimeout  string   `toml:"gate_timeout" json:"gate_timeout"` // duration string, e.g. "5m"
}

// DefaultWorldConfig returns a WorldConfig with built-in defaults.
func DefaultWorldConfig() WorldConfig {
	return WorldConfig{
		Agents: AgentsSection{
			ModelTier: "sonnet",
		},
		Forge: ForgeSection{
			TargetBranch: "main",
			GateTimeout:  "5m",
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

// Validate checks that config values are within acceptable ranges.
func (c WorldConfig) Validate() error {
	if c.Agents.Capacity < 0 {
		return fmt.Errorf("agents.capacity must be >= 0, got %d", c.Agents.Capacity)
	}
	if c.Agents.ModelTier != "" {
		switch c.Agents.ModelTier {
		case "sonnet", "opus", "haiku":
			// valid
		default:
			return fmt.Errorf("agents.model_tier must be sonnet, opus, or haiku; got %q", c.Agents.ModelTier)
		}
	}
	if c.Forge.GateTimeout != "" {
		if _, err := time.ParseDuration(c.Forge.GateTimeout); err != nil {
			return fmt.Errorf("forge.gate_timeout %q is not a valid duration: %w", c.Forge.GateTimeout, err)
		}
	}
	return nil
}

// IsSleeping returns true if the world is marked as sleeping in its config.
// Returns false if the config cannot be loaded (fail-open).
func IsSleeping(world string) bool {
	cfg, err := LoadWorldConfig(world)
	if err != nil {
		return false
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

	// Write to temp file first for atomic rename.
	tmp, err := os.CreateTemp(dir, ".world.toml.*")
	if err != nil {
		return fmt.Errorf("failed to create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()

	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(cfg); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file for %s: %w", path, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file to %s: %w", path, err)
	}
	return nil
}
