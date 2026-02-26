package refinery

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/gt/internal/config"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/store"
)

// RigStore abstracts rig store operations for testing.
type RigStore interface {
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

// TownStore abstracts town store operations for testing.
type TownStore interface {
	CreateAgent(name, rig, role string) (string, error)
	GetAgent(id string) (*store.Agent, error)
	UpdateAgentState(id, state, hookItem string) error
	Close() error
}

// Config holds refinery configuration.
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

// RefineryWorktreePath returns the worktree directory for a rig's refinery.
func RefineryWorktreePath(rig string) string {
	return filepath.Join(config.Home(), rig, "refinery", "rig")
}

// RefineryBranch returns the branch name for a rig's refinery worktree.
func RefineryBranch(rig string) string {
	return "refinery/" + rig
}

// Refinery processes the merge queue for a single rig.
type Refinery struct {
	rig        string
	agentID    string // "{rig}/refinery"
	sourceRepo string // path to the source git repo
	worktree   string // path to the refinery's persistent worktree
	rigStore   RigStore
	townStore  TownStore
	logger     *slog.Logger
	cfg        Config
}

// New creates a new Refinery.
func New(rig, sourceRepo string, rigStore RigStore, townStore TownStore,
	cfg Config, logger *slog.Logger) *Refinery {
	return &Refinery{
		rig:        rig,
		agentID:    rig + "/refinery",
		sourceRepo: sourceRepo,
		worktree:   RefineryWorktreePath(rig),
		rigStore:   rigStore,
		townStore:  townStore,
		logger:     logger,
		cfg:        cfg,
	}
}

// Run starts the refinery's merge loop. Blocks until ctx is cancelled.
func (r *Refinery) Run(ctx context.Context) error {
	// 1. Ensure worktree exists.
	if err := r.EnsureWorktree(); err != nil {
		return err
	}

	// 2. Register refinery agent in town store.
	if err := r.registerAgent(); err != nil {
		return err
	}

	// 3. Log startup.
	r.logger.Info("refinery started", "rig", r.rig, "worktree", r.worktree)

	// 4. Main loop.
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	// Process immediately on startup, then on each tick.
	r.poll()
	for {
		select {
		case <-ctx.Done():
			return r.shutdown()
		case <-ticker.C:
			r.poll()
		}
	}
}

// EnsureWorktree creates or verifies the refinery's persistent worktree.
func (r *Refinery) EnsureWorktree() error {
	branch := RefineryBranch(r.rig)

	// If the worktree directory already exists, verify it's valid.
	if info, err := os.Stat(r.worktree); err == nil && info.IsDir() {
		cmd := exec.Command("git", "-C", r.worktree, "rev-parse", "--is-inside-work-tree")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to verify refinery worktree for rig %q: %w",
				r.rig, fmt.Errorf("%s", strings.TrimSpace(string(out))))
		}
		return nil
	}

	// Create parent directory.
	parentDir := filepath.Dir(r.worktree)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create refinery worktree for rig %q: %w", r.rig, err)
	}

	// Try creating worktree with new branch.
	cmd := exec.Command("git", "-C", r.sourceRepo, "worktree", "add",
		"-b", branch, r.worktree, "HEAD")
	if _, err := cmd.CombinedOutput(); err != nil {
		// Branch may already exist — try without -b.
		cmd2 := exec.Command("git", "-C", r.sourceRepo, "worktree", "add",
			r.worktree, branch)
		if out, err2 := cmd2.CombinedOutput(); err2 != nil {
			return fmt.Errorf("failed to create refinery worktree for rig %q: %w",
				r.rig, fmt.Errorf("%s", strings.TrimSpace(string(out))))
		}
	}

	return nil
}

// registerAgent ensures the refinery agent exists in the town store and sets it to working.
func (r *Refinery) registerAgent() error {
	_, err := r.townStore.GetAgent(r.agentID)
	if err != nil {
		// Agent doesn't exist — create it.
		if _, err := r.townStore.CreateAgent("refinery", r.rig, "refinery"); err != nil {
			return fmt.Errorf("failed to register refinery agent: %w", err)
		}
	}

	if err := r.townStore.UpdateAgentState(r.agentID, "working", ""); err != nil {
		return fmt.Errorf("failed to set refinery agent to working: %w", err)
	}
	return nil
}

// poll runs one poll cycle: release stale claims, claim next MR, process it.
func (r *Refinery) poll() {
	// 1. Release stale claims.
	n, err := r.rigStore.ReleaseStaleClaims(r.cfg.ClaimTTL)
	if err != nil {
		r.logger.Error("failed to release stale claims", "error", err)
	} else if n > 0 {
		r.logger.Warn("released stale claims", "count", n)
	}

	// 2. Claim next MR.
	mr, err := r.rigStore.ClaimMergeRequest(r.agentID)
	if err != nil {
		r.logger.Error("failed to claim merge request", "error", err)
		return
	}
	if mr == nil {
		return // No ready MRs.
	}

	// 3. Check max attempts.
	if mr.Attempts > r.cfg.MaxAttempts {
		if err := r.rigStore.UpdateMergeRequestPhase(mr.ID, "failed"); err != nil {
			r.logger.Error("failed to mark MR as failed", "mr", mr.ID, "error", err)
		}
		r.logger.Error("max attempts exceeded", "mr", mr.ID, "branch", mr.Branch,
			"attempts", mr.Attempts, "max", r.cfg.MaxAttempts)
		return
	}

	// 4. Acquire merge slot.
	lock, err := dispatch.AcquireMergeSlotLock(r.rig)
	if err != nil {
		r.logger.Warn("merge slot busy, releasing claim", "mr", mr.ID, "error", err)
		if err := r.rigStore.UpdateMergeRequestPhase(mr.ID, "ready"); err != nil {
			r.logger.Error("failed to release MR claim", "mr", mr.ID, "error", err)
		}
		return
	}
	defer lock.Release()

	// 5. Process the merge.
	if err := r.processMerge(mr); err != nil {
		r.logger.Error("merge processing failed", "mr", mr.ID, "error", err)
	}
}

// processMerge is the core merge pipeline: sync, merge, test, push.
func (r *Refinery) processMerge(mr *store.MergeRequest) error {
	branch := RefineryBranch(r.rig)

	// 1. Sync worktree to target branch.
	if out, err := exec.Command("git", "-C", r.worktree, "fetch", "origin").CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("git", "-C", r.worktree, "checkout", branch).CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := exec.Command("git", "-C", r.worktree, "reset", "--hard",
		"origin/"+r.cfg.TargetBranch).CombinedOutput(); err != nil {
		return fmt.Errorf("git reset failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// 2. Merge polecat's branch.
	mergeCmd := exec.Command("git", "-C", r.worktree, "merge", "--no-ff", "origin/"+mr.Branch)
	if out, err := mergeCmd.CombinedOutput(); err != nil {
		// Merge conflict — abort and mark failed.
		exec.Command("git", "-C", r.worktree, "merge", "--abort").Run()
		if err := r.rigStore.UpdateMergeRequestPhase(mr.ID, "failed"); err != nil {
			r.logger.Error("failed to mark MR as failed after conflict", "mr", mr.ID, "error", err)
		}
		r.logger.Error("rebase conflict", "mr", mr.ID, "branch", mr.Branch,
			"output", truncate(string(out), 500))
		return nil
	}

	// 3. Run quality gates.
	for _, gate := range r.cfg.QualityGates {
		cmd := exec.Command("sh", "-c", gate)
		cmd.Dir = r.worktree
		cmd.Env = append(os.Environ(),
			"GT_HOME="+config.Home(),
			"GT_RIG="+r.rig,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Gate failed — reset and set MR back to ready for retry.
			exec.Command("git", "-C", r.worktree, "reset", "--hard",
				"origin/"+r.cfg.TargetBranch).Run()
			if err := r.rigStore.UpdateMergeRequestPhase(mr.ID, "ready"); err != nil {
				r.logger.Error("failed to reset MR phase after gate failure",
					"mr", mr.ID, "error", err)
			}
			r.logger.Warn("quality gate failed", "mr", mr.ID, "gate", gate,
				"output", truncate(string(output), 500))
			return nil
		}
	}

	// 4. Push to target branch.
	pushCmd := exec.Command("git", "-C", r.worktree, "push", "origin",
		"HEAD:"+r.cfg.TargetBranch)
	if out, err := pushCmd.CombinedOutput(); err != nil {
		// Push rejected — reset and set MR back to ready for retry.
		exec.Command("git", "-C", r.worktree, "reset", "--hard",
			"origin/"+r.cfg.TargetBranch).Run()
		if err := r.rigStore.UpdateMergeRequestPhase(mr.ID, "ready"); err != nil {
			r.logger.Error("failed to reset MR phase after push rejection",
				"mr", mr.ID, "error", err)
		}
		r.logger.Warn("push rejected, will retry", "mr", mr.ID,
			"output", truncate(string(out), 500))
		return nil
	}

	// 5. Success — update state.
	if err := r.rigStore.UpdateMergeRequestPhase(mr.ID, "merged"); err != nil {
		r.logger.Error("failed to mark MR as merged", "mr", mr.ID, "error", err)
	}
	if err := r.rigStore.UpdateWorkItem(mr.WorkItemID, store.WorkItemUpdates{Status: "closed"}); err != nil {
		r.logger.Error("failed to close work item", "work_item", mr.WorkItemID, "error", err)
	}
	r.logger.Info("merged", "mr", mr.ID, "work_item", mr.WorkItemID,
		"branch", mr.Branch, "attempts", mr.Attempts)

	// Clean up remote branch (best-effort).
	exec.Command("git", "-C", r.worktree, "push", "origin", "--delete", mr.Branch).Run()

	return nil
}

// shutdown sets the refinery agent to idle and logs.
func (r *Refinery) shutdown() error {
	if err := r.townStore.UpdateAgentState(r.agentID, "idle", ""); err != nil {
		r.logger.Error("failed to set refinery agent to idle on shutdown", "error", err)
	}
	r.logger.Info("refinery stopped", "rig", r.rig)
	return nil
}

// truncate returns the first n bytes of s, or s if shorter.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
