package setup

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/doctor"
	"github.com/nevinsm/sol/internal/store"
)

// Params holds the inputs for a first-time setup.
type Params struct {
	WorldName  string // required: name for the first world
	SourceRepo string // optional: path to source git repo
	SkipChecks bool   // skip doctor checks
}

// Validate checks that Params are well-formed before running setup.
func (p *Params) Validate() error {
	if p.WorldName == "" {
		return fmt.Errorf("world name is required")
	}
	return config.ValidateWorldName(p.WorldName)
}

// Result holds the output of a successful setup.
type Result struct {
	SOLHome    string
	WorldName  string
	ConfigPath string
	DBPath     string
	SourceRepo string
}

// Run executes the full first-time setup sequence.
//
// Steps:
// 1. Run doctor checks (unless SkipChecks)
// 2. Create SOL_HOME directory structure
// 3. Create .store/ and .runtime/ directories
// 4. Create world directory + outposts/
// 5. Create world database (triggers schema migration)
// 6. Register world in sphere.db
// 7. Write world.toml with defaults + source repo
//
// Returns a Result on success for display by the caller.
func Run(p Params) (*Result, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	// Check if world already initialized.
	tomlPath := config.WorldConfigPath(p.WorldName)
	if _, err := os.Stat(tomlPath); err == nil {
		return nil, fmt.Errorf("world %q is already initialized", p.WorldName)
	}

	// 1. Doctor checks.
	if !p.SkipChecks {
		report := doctor.RunAll()
		if !report.AllPassed() {
			// Build a useful error message listing failures.
			msg := fmt.Sprintf("%d prerequisite check(s) failed:", report.FailedCount())
			for _, c := range report.Checks {
				if !c.Passed {
					msg += fmt.Sprintf("\n  ✗ %s: %s", c.Name, c.Message)
					if c.Fix != "" {
						msg += fmt.Sprintf("\n    → %s", c.Fix)
					}
				}
			}
			msg += "\n\nRun 'sol doctor' for full details, or use --skip-checks to bypass."
			return nil, fmt.Errorf("%s", msg)
		}
	}

	home := config.Home()

	// 2. Create SOL_HOME.
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create SOL_HOME (%s): %w", home, err)
	}

	// 3. Create .store/ and .runtime/.
	if err := config.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}

	// 4. Create world directory + outposts/.
	worldDir := config.WorldDir(p.WorldName)
	if err := os.MkdirAll(filepath.Join(worldDir, "outposts"), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create world directory: %w", err)
	}

	// 5. Create world database.
	worldStore, err := store.OpenWorld(p.WorldName)
	if err != nil {
		return nil, fmt.Errorf("failed to create world database: %w", err)
	}
	worldStore.Close()

	// 6. Register in sphere.db.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return nil, fmt.Errorf("failed to open sphere database: %w", err)
	}
	if err := sphereStore.RegisterWorld(p.WorldName, p.SourceRepo); err != nil {
		sphereStore.Close()
		return nil, fmt.Errorf("failed to register world: %w", err)
	}
	sphereStore.Close()

	// 7. Write world.toml.
	cfg := config.DefaultWorldConfig()
	cfg.World.SourceRepo = p.SourceRepo
	if err := config.WriteWorldConfig(p.WorldName, cfg); err != nil {
		return nil, fmt.Errorf("failed to write world config: %w", err)
	}

	return &Result{
		SOLHome:    home,
		WorldName:  p.WorldName,
		ConfigPath: config.WorldConfigPath(p.WorldName),
		DBPath:     filepath.Join(config.StoreDir(), p.WorldName+".db"),
		SourceRepo: p.SourceRepo,
	}, nil
}
