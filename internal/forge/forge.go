package forge

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// WorldStore defines the world store operations used by forge.
// It composes canonical interfaces from the store package.
type WorldStore interface {
	store.WritReader
	store.WritWriter
	store.MRReader
	store.MRWriter
	io.Closer
}

// SphereStore defines the sphere store operations used by forge.
// It composes canonical interfaces from the store package.
type SphereStore interface {
	store.EscalationStore
	store.AgentWriter
	store.CaravanDepReader
	io.Closer
}

// Config holds forge configuration.
type Config struct {
	PollInterval time.Duration // how often to poll for ready MRs (default: 10s)
	ClaimTTL     time.Duration // TTL before stale claims are released (default: 30min)
	MaxAttempts  int           // max merge attempts before marking failed (default: 3)
	TargetBranch string        // branch to merge into (default: "main")
	QualityGates []string      // commands to run as quality gates
	GateTimeout  time.Duration // gate execution timeout (default: 5m)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		PollInterval: 10 * time.Second,
		ClaimTTL:     30 * time.Minute,
		MaxAttempts:  3,
		TargetBranch: "main",
		QualityGates: []string{"go test ./..."},
		GateTimeout:  5 * time.Minute,
	}
}

// WorktreePath returns the worktree directory for a world's forge.
func WorktreePath(world string) string {
	return filepath.Join(config.Home(), world, "forge", "worktree")
}

// Forge processes the merge queue for a single world.
type Forge struct {
	world       string
	agentID     string // "{world}/forge"
	sourceRepo  string // path to the source git repo
	worktree    string // path to the forge's persistent worktree
	worldStore  WorldStore
	sphereStore SphereStore
	sessions    ForgeSessionManager // session manager for merge sessions (nil = legacy mode)
	launcher    SessionLauncher     // overridable for testing; nil = startup.Launch
	logger      *slog.Logger
	cfg         Config
}

// New creates a new Forge. The sessions parameter is optional — if nil, the forge
// operates in legacy mode (direct merge execution). If provided, the forge uses
// ephemeral Claude sessions for merge execution (ADR-0028).
func New(world, sourceRepo string, worldStore WorldStore, sphereStore SphereStore,
	cfg Config, logger *slog.Logger, sessions ...ForgeSessionManager) *Forge {
	var sess ForgeSessionManager
	if len(sessions) > 0 {
		sess = sessions[0]
	}
	return &Forge{
		world:       world,
		agentID:     world + "/forge",
		sourceRepo:  sourceRepo,
		worktree:    WorktreePath(world),
		worldStore:  worldStore,
		sphereStore: sphereStore,
		sessions:    sess,
		logger:      logger,
		cfg:         cfg,
	}
}

// EnsureWorktree creates or verifies the forge's persistent worktree.
// The worktree operates in detached HEAD mode, pointed at origin/{targetBranch}.
func (r *Forge) EnsureWorktree() error {
	targetRef := "origin/" + r.cfg.TargetBranch

	// If the worktree directory already exists, verify it's valid.
	if info, err := os.Stat(r.worktree); err == nil && info.IsDir() {
		cmd := exec.Command("git", "-C", r.worktree, "rev-parse", "--is-inside-work-tree")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to verify forge worktree for world %q: %s: %w",
				r.world, strings.TrimSpace(string(out)), err)
		}

		// Detach HEAD if currently on a branch (migration from branch-based worktree).
		branchCmd := exec.Command("git", "-C", r.worktree, "symbolic-ref", "--quiet", "HEAD")
		if branchCmd.Run() == nil {
			// HEAD is symbolic (on a branch) — detach it.
			r.logger.Info("detaching forge worktree HEAD (was on a branch)", "world", r.world)
			detachCmd := exec.Command("git", "-C", r.worktree, "checkout", "--detach")
			if out, err := detachCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to detach forge worktree HEAD for world %q: %s: %w",
					r.world, strings.TrimSpace(string(out)), err)
			}
		}
		return nil
	}

	// Fetch first so origin/{targetBranch} is available.
	fetchCmd := exec.Command("git", "-C", r.sourceRepo, "fetch", "origin")
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch origin for forge worktree: %s: %w",
			strings.TrimSpace(string(out)), err)
	}

	// Create parent directory.
	parentDir := filepath.Dir(r.worktree)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create forge worktree for world %q: %w", r.world, err)
	}

	// Create worktree in detached HEAD mode at origin/{targetBranch}.
	cmd := exec.Command("git", "-C", r.sourceRepo, "worktree", "add",
		"--detach", r.worktree, targetRef)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create forge worktree for world %q: %s: %w",
			r.world, strings.TrimSpace(string(out)), err)
	}

	return nil
}

// truncate returns the first n bytes of s, or s if shorter.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
