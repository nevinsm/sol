package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// CloneRepo clones the given source (URL or local path) into the managed
// repo directory at config.RepoPath(world). If the source is a local path
// and has an upstream origin remote, the managed clone's origin is set to
// that upstream URL so pushes go directly to the real remote.
func CloneRepo(world, source string) error {
	repoPath := config.RepoPath(world)

	if _, err := os.Stat(repoPath); err == nil {
		return fmt.Errorf("managed repo already exists for world %q", world)
	}

	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory for world %q: %w", world, err)
	}

	cmd := exec.Command("git", "clone", source, repoPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to clone source repo for world %q: %s: %w",
			world, strings.TrimSpace(string(out)), err)
	}

	// Adopt upstream origin for local paths.
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		upstreamCmd := exec.Command("git", "-C", source, "remote", "get-url", "origin")
		if upstreamOut, err := upstreamCmd.Output(); err == nil {
			upstream := strings.TrimSpace(string(upstreamOut))
			if upstream != "" && upstream != source {
				setCmd := exec.Command("git", "-C", repoPath, "remote", "set-url", "origin", upstream)
				if out, err := setCmd.CombinedOutput(); err != nil {
					return fmt.Errorf("failed to set upstream origin for world %q: %s: %w",
						world, strings.TrimSpace(string(out)), err)
				}
			}
		}
	}

	// Exclude sol-specific paths from git tracking via .git/info/exclude.
	// This avoids modifying the project's .gitignore while ensuring .claude/
	// and .brief/ directories (created by sol in worktrees) are never committed.
	if err := InstallExcludes(repoPath); err != nil {
		return fmt.Errorf("failed to install git excludes for world %q: %w", world, err)
	}

	return nil
}

// InstallExcludes appends sol-managed path patterns to .git/info/exclude in the
// given repo. These patterns propagate to all worktrees created from the repo.
// Idempotent — skips if the patterns are already present.
//
// If you add a new sol-managed dotdir that gets written inside worktrees,
// add it here. Keep in sync with the "Worktree excludes" note in CLAUDE.md.
func InstallExcludes(repoPath string) error {
	excludePath := filepath.Join(repoPath, ".git", "info", "exclude")

	existing, err := os.ReadFile(excludePath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", excludePath, err)
	}
	if strings.Contains(string(existing), "# sol-managed paths") {
		return nil
	}

	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", excludePath, err)
	}
	defer f.Close()

	_, err = f.WriteString("\n# sol-managed paths\n.claude/\n.brief/\n.workflow/\n")
	return err
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

	// 4b. Clone source repo into managed repo directory.
	if p.SourceRepo != "" {
		if err := CloneRepo(p.WorldName, p.SourceRepo); err != nil {
			return nil, fmt.Errorf("failed to clone source repo: %w", err)
		}
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
