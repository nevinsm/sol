package setup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/doctor"
	"github.com/nevinsm/sol/internal/fileutil"
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
		// Repo directory already exists. Check if it's a valid git repo
		// (crash recovery: clone succeeded but setup didn't finish).
		checkCmd := exec.Command("git", "-C", repoPath, "rev-parse", "--git-dir")
		if err := checkCmd.Run(); err == nil {
			// Valid git repo — skip clone, just ensure excludes are current.
			if err := InstallExcludes(repoPath); err != nil {
				return fmt.Errorf("failed to install git excludes for world %q: %w", world, err)
			}
			return nil
		}
		// Not a valid git repo — remove the partial directory and re-clone.
		if err := os.RemoveAll(repoPath); err != nil {
			return fmt.Errorf("failed to clean up partial repo for world %q: %w", world, err)
		}
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

	// Exclude sol-managed files from git tracking via .git/info/exclude.
	// Only excludes specific local files sol writes — the project's shared
	// .claude/ contents (settings.json, CLAUDE.md, agents/, rules/) are preserved.
	if err := InstallExcludes(repoPath); err != nil {
		return fmt.Errorf("failed to install git excludes for world %q: %w", world, err)
	}

	return nil
}

// excludeBlock is the canonical set of sol-managed path patterns.
// If you add a new sol-managed path that gets written inside worktrees,
// add it here. Keep in sync with the "Worktree excludes" note in CLAUDE.md.
const excludeBlock = `# BEGIN sol-managed paths
.claude/settings.local.json
.claude/system-prompt.md
.claude/skills/
CLAUDE.local.md
.workflow/
.forge-result.json
.forge-injection.md
.guidelines.md
AGENTS.override.md
.agents/skills/
.codex/
# END sol-managed paths
`

// InstallExcludes writes sol-managed path patterns to .git/info/exclude in the
// given repo. These patterns propagate to all worktrees created from the repo.
//
// Uses a BEGIN/END delimited block that gets fully replaced on every call,
// ensuring the exclude list stays current as new patterns are added.
//
// Handles three cases:
//   - BEGIN/END block exists → replace the block with the current canonical block
//   - Legacy "# sol-managed paths" marker (without BEGIN) → replace from marker to EOF
//   - Neither exists → append the block
//
// Only excludes specific files sol writes — NOT the entire .claude/ directory,
// because the project may have shared .claude/settings.json, .claude/CLAUDE.md,
// .claude/agents/, or .claude/rules/ that belong in version control.
func InstallExcludes(repoPath string) error {
	excludePath := filepath.Join(repoPath, ".git", "info", "exclude")

	existing, err := os.ReadFile(excludePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to read %s: %w", excludePath, err)
		}
		// File doesn't exist yet (manually constructed repo) — treat as empty.
		existing = nil
	}
	content := string(existing)

	var updated string
	switch {
	case strings.Contains(content, "# BEGIN sol-managed paths"):
		// Replace existing BEGIN/END block.
		beginIdx := strings.Index(content, "# BEGIN sol-managed paths")
		endMarker := "# END sol-managed paths"
		endIdx := strings.Index(content, endMarker)
		if endIdx == -1 {
			// Malformed: BEGIN without END — replace from BEGIN to EOF.
			updated = content[:beginIdx] + excludeBlock
		} else {
			// Consume trailing newline after END marker if present.
			afterEnd := endIdx + len(endMarker)
			if afterEnd < len(content) && content[afterEnd] == '\n' {
				afterEnd++
			}
			updated = content[:beginIdx] + excludeBlock + content[afterEnd:]
		}

	case strings.Contains(content, "# sol-managed paths"):
		// Legacy format: replace from legacy marker to EOF.
		idx := strings.Index(content, "# sol-managed paths")
		updated = content[:idx] + excludeBlock

	default:
		// Fresh install: append block, with a separating newline only if
		// there is existing content to separate from.
		if content == "" {
			updated = excludeBlock
		} else {
			updated = content + "\n" + excludeBlock
		}
	}

	if err := fileutil.AtomicWrite(excludePath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", excludePath, err)
	}
	return nil
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
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("cannot check world config %q: %w", tomlPath, err)
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

	// cleanups tracks rollback functions to call on error, in reverse order.
	// Each step appends its undo action; on failure all prior steps are reversed
	// so re-running setup can succeed without manual cleanup.
	var cleanups []func()
	runCleanups := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	if p.SourceRepo != "" {
		cleanups = append(cleanups, func() {
			os.RemoveAll(config.RepoPath(p.WorldName))
		})
	}

	// 5. Create world database.
	worldStore, err := store.OpenWorld(p.WorldName)
	if err != nil {
		runCleanups()
		return nil, fmt.Errorf("failed to create world database: %w", err)
	}
	worldStore.Close()

	worldDBPath := filepath.Join(config.StoreDir(), p.WorldName+".db")
	cleanups = append(cleanups, func() {
		os.Remove(worldDBPath)
		os.Remove(worldDBPath + "-wal")
		os.Remove(worldDBPath + "-shm")
	})

	// 6. Register in sphere.db.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		runCleanups()
		return nil, fmt.Errorf("failed to open sphere database: %w", err)
	}
	if err := sphereStore.RegisterWorld(p.WorldName, p.SourceRepo); err != nil {
		sphereStore.Close()
		runCleanups()
		return nil, fmt.Errorf("failed to register world: %w", err)
	}
	sphereStore.Close()

	cleanups = append(cleanups, func() {
		if s, err := store.OpenSphere(); err == nil {
			s.DeleteWorldData(p.WorldName)
			s.Close()
		}
	})

	// 7. Write world.toml.
	cfg := config.DefaultWorldConfig()
	cfg.World.SourceRepo = p.SourceRepo
	if err := config.WriteWorldConfig(p.WorldName, cfg); err != nil {
		runCleanups()
		return nil, fmt.Errorf("failed to write world config: %w", err)
	}

	// 8. Seed .claude-defaults/ with embedded defaults.
	// Non-fatal: agents still work without defaults, they just get bare Claude Code settings.
	if err := config.EnsureClaudeDefaults(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to seed claude defaults: %v\n", err)
	}

	return &Result{
		SOLHome:    home,
		WorldName:  p.WorldName,
		ConfigPath: config.WorldConfigPath(p.WorldName),
		DBPath:     filepath.Join(config.StoreDir(), p.WorldName+".db"),
		SourceRepo: p.SourceRepo,
	}, nil
}
