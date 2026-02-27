package forge

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// WorldStore abstracts world store operations for testing.
type WorldStore interface {
	GetMergeRequest(id string) (*store.MergeRequest, error)
	ClaimMergeRequest(claimerID string) (*store.MergeRequest, error)
	UpdateMergeRequestPhase(id, phase string) error
	ReleaseStaleClaims(ttl time.Duration) (int, error)
	GetWorkItem(id string) (*store.WorkItem, error)
	UpdateWorkItem(id string, updates store.WorkItemUpdates) error
	ListMergeRequests(phase string) ([]store.MergeRequest, error)
	BlockMergeRequest(mrID, blockerID string) error
	UnblockMergeRequest(mrID string) error
	FindMergeRequestByBlocker(blockerID string) (*store.MergeRequest, error)
	CreateWorkItemWithOpts(opts store.CreateWorkItemOpts) (string, error)
	CloseWorkItem(id string) error
	Close() error
}

// SphereStore abstracts sphere store operations for testing.
type SphereStore interface {
	CreateAgent(name, rig, role string) (string, error)
	GetAgent(id string) (*store.Agent, error)
	UpdateAgentState(id, state, tetherItem string) error
	Close() error
}

// Config holds forge configuration.
type Config struct {
	PollInterval time.Duration // how often to poll for ready MRs (default: 10s)
	ClaimTTL     time.Duration // TTL before stale claims are released (default: 30min)
	MaxAttempts  int           // max merge attempts before marking failed (default: 3)
	TargetBranch string        // branch to merge into (default: "main")
	QualityGates []string      // commands to run as quality gates
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		PollInterval: 10 * time.Second,
		ClaimTTL:     30 * time.Minute,
		MaxAttempts:  3,
		TargetBranch: "main",
		QualityGates: []string{"go test ./..."},
	}
}

// WorktreePath returns the worktree directory for a world's forge.
func WorktreePath(world string) string {
	return filepath.Join(config.Home(), world, "forge", "worktree")
}

// ForgeBranch returns the branch name for a world's forge worktree.
func ForgeBranch(world string) string {
	return "forge/" + world
}

// Forge processes the merge queue for a single world.
type Forge struct {
	world      string
	agentID    string // "{world}/forge"
	sourceRepo string // path to the source git repo
	worktree   string // path to the forge's persistent worktree
	worldStore WorldStore
	sphereStore SphereStore
	logger     *slog.Logger
	cfg        Config
}

// New creates a new Forge.
func New(world, sourceRepo string, worldStore WorldStore, sphereStore SphereStore,
	cfg Config, logger *slog.Logger) *Forge {
	return &Forge{
		world:       world,
		agentID:     world + "/forge",
		sourceRepo:  sourceRepo,
		worktree:    WorktreePath(world),
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:     logger,
		cfg:        cfg,
	}
}

// EnsureWorktree creates or verifies the forge's persistent worktree.
func (r *Forge) EnsureWorktree() error {
	branch := ForgeBranch(r.world)

	// If the worktree directory already exists, verify it's valid.
	if info, err := os.Stat(r.worktree); err == nil && info.IsDir() {
		cmd := exec.Command("git", "-C", r.worktree, "rev-parse", "--is-inside-work-tree")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to verify forge worktree for world %q: %w",
				r.world, fmt.Errorf("%s", strings.TrimSpace(string(out))))
		}
		return nil
	}

	// Create parent directory.
	parentDir := filepath.Dir(r.worktree)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create forge worktree for world %q: %w", r.world, err)
	}

	// Try creating worktree with new branch.
	cmd := exec.Command("git", "-C", r.sourceRepo, "worktree", "add",
		"-b", branch, r.worktree, "HEAD")
	if _, err := cmd.CombinedOutput(); err != nil {
		// Branch may already exist — try without -b.
		cmd2 := exec.Command("git", "-C", r.sourceRepo, "worktree", "add",
			r.worktree, branch)
		if out, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("failed to create forge worktree for world %q: %w",
				r.world, fmt.Errorf("%s", strings.TrimSpace(string(out))))
		}
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
