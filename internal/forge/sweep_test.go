package forge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// --- parseWritID unit tests ---

func TestParseWritID(t *testing.T) {
	tests := []struct {
		branch  string
		wantID  string
		wantNil bool
	}{
		// Standard outpost branch.
		{
			branch: "outpost/Toast/sol-aaa1111122223333",
			wantID: "sol-aaa1111122223333",
		},
		// Standard envoy branch with writ ID.
		{
			branch: "envoy/world/Envoy/sol-bbb4444555566667",
			wantID: "sol-bbb4444555566667",
		},
		// Persistent envoy branch (no writ ID suffix).
		{
			branch:  "envoy/world/Envoy",
			wantNil: true,
		},
		// Short test IDs (as used in tests).
		{
			branch: "outpost/Toast/sol-aaa11111",
			wantID: "sol-aaa11111",
		},
		// Agent name starting with "sol-" would be unusual but still safe.
		{
			branch: "outpost/Toast/sol-merged0001",
			wantID: "sol-merged0001",
		},
		// Branch with no sol- segment.
		{
			branch:  "outpost/Toast/feature-branch",
			wantNil: true,
		},
		// Main branch.
		{
			branch:  "main",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := parseWritID(tt.branch)
			if tt.wantNil {
				if got != "" {
					t.Errorf("parseWritID(%q) = %q, want empty", tt.branch, got)
				}
			} else {
				if got != tt.wantID {
					t.Errorf("parseWritID(%q) = %q, want %q", tt.branch, got, tt.wantID)
				}
			}
		})
	}
}

// --- Mock-based unit tests for SweepBranches ---

// setupSweepMock configures a mockCmdRunner for a SweepBranches test.
// remoteBranches are the full branch names (without "origin/" prefix).
// checkedOutBranches are full branch names that appear in worktree list.
func setupSweepMock(
	mock *mockCmdRunner,
	remoteBranches []string,
	localBranches []string,
	checkedOutBranches []string,
	landedWritIDs map[string]bool, // writID → true if on target
) {
	// Fetch always succeeds.
	mock.SetResult("git fetch origin", nil, nil)
	// Target ref exists.
	mock.SetResult("git rev-parse --verify --quiet refs/remotes/origin/main",
		[]byte("abc123\n"), nil)

	// Build remote branch output lines.
	var outpostRemote, envoyRemote []string
	for _, b := range remoteBranches {
		if len(b) > 8 && b[:8] == "outpost/" {
			outpostRemote = append(outpostRemote, "  origin/"+b)
		} else if len(b) > 6 && b[:6] == "envoy/" {
			envoyRemote = append(envoyRemote, "  origin/"+b)
		}
	}
	mock.SetResult("git branch -r --list origin/outpost/*/sol-*",
		[]byte(joinLines(outpostRemote)), nil)
	mock.SetResult("git branch -r --list origin/envoy/*/*/sol-*",
		[]byte(joinLines(envoyRemote)), nil)

	// Build local branch output lines.
	var outpostLocal, envoyLocal []string
	for _, b := range localBranches {
		if len(b) > 8 && b[:8] == "outpost/" {
			outpostLocal = append(outpostLocal, "  "+b)
		} else if len(b) > 6 && b[:6] == "envoy/" {
			envoyLocal = append(envoyLocal, "  "+b)
		}
	}
	mock.SetResult("git branch --list outpost/*/sol-*",
		[]byte(joinLines(outpostLocal)), nil)
	mock.SetResult("git branch --list envoy/*/*/sol-*",
		[]byte(joinLines(envoyLocal)), nil)

	// Build worktree list output.
	worktreeOutput := "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n"
	for i, b := range checkedOutBranches {
		worktreeOutput += "\nworktree /wt" + string(rune('0'+i)) + "\n"
		worktreeOutput += "HEAD def456\n"
		worktreeOutput += "branch refs/heads/" + b + "\n"
	}
	mock.SetResult("git worktree list --porcelain", []byte(worktreeOutput), nil)

	// Set up git log results for each unique writ ID.
	seen := map[string]bool{}
	for _, b := range append(remoteBranches, localBranches...) {
		writID := parseWritID(b)
		if writID == "" || seen[writID] {
			continue
		}
		seen[writID] = true
		if landedWritIDs[writID] {
			mock.SetResult(
				"git log refs/remotes/origin/main --grep="+writID+" -n 1 --format=%H",
				[]byte("cafebabe\n"), nil,
			)
		} else {
			mock.SetResult(
				"git log refs/remotes/origin/main --grep="+writID+" -n 1 --format=%H",
				[]byte(""), nil,
			)
		}
	}
}

// joinLines returns lines joined with "\n", with a trailing newline.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}

// newSweepForge creates a minimal Forge suitable for mock-based sweep tests.
func newSweepForge(worldStore WorldStore) *Forge {
	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	return &Forge{
		world:      "ember",
		agentID:    "ember/forge",
		sourceRepo: "/fake/repo",
		worldStore: worldStore,
		logger:     testLogger(),
		cfg:        forgeCfg,
		cmd:        newMockCmdRunner(),
	}
}

// TestSweepBranches_MergedWrit verifies that a branch whose writ ID appears
// on the target branch is classified as "merged" and marked for deletion.
func TestSweepBranches_MergedWrit(t *testing.T) {
	const branch = "outpost/Toast/sol-merged0001"
	const writID = "sol-merged0001"

	worldStore := newMockWorldStore()
	r := newSweepForge(worldStore)
	mock := r.cmd.(*mockCmdRunner)
	setupSweepMock(mock,
		[]string{branch},
		nil,
		nil,
		map[string]bool{writID: true}, // writ IS on target
	)

	report, err := r.SweepBranches(context.Background(), false, true /* dryRun */)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	if report.Inspected != 1 {
		t.Errorf("Inspected = %d, want 1", report.Inspected)
	}
	if len(report.Deleted) != 1 {
		t.Fatalf("Deleted count = %d, want 1", len(report.Deleted))
	}
	if report.Deleted[0].Branch != branch {
		t.Errorf("Deleted[0].Branch = %q, want %q", report.Deleted[0].Branch, branch)
	}
	if report.Deleted[0].WritID != writID {
		t.Errorf("Deleted[0].WritID = %q, want %q", report.Deleted[0].WritID, writID)
	}
	if report.Deleted[0].Reason != "merged" {
		t.Errorf("Deleted[0].Reason = %q, want 'merged'", report.Deleted[0].Reason)
	}
	if len(report.Preserved) != 0 {
		t.Errorf("Preserved count = %d, want 0", len(report.Preserved))
	}
	if report.DryRun != true {
		t.Error("DryRun should be true")
	}
}

// TestSweepBranches_ClosedOrphanIncluded verifies that a branch whose writ is
// closed in the DB (but not on target) is deleted when includeClosedOrphans=true.
func TestSweepBranches_ClosedOrphanIncluded(t *testing.T) {
	const branch = "outpost/Toast/sol-closed0001"
	const writID = "sol-closed0001"

	worldStore := newMockWorldStore()
	worldStore.items[writID] = &store.Writ{
		ID:     writID,
		Status: store.WritClosed,
	}

	r := newSweepForge(worldStore)
	mock := r.cmd.(*mockCmdRunner)
	setupSweepMock(mock,
		[]string{branch},
		nil,
		nil,
		map[string]bool{writID: false}, // writ NOT on target
	)

	report, err := r.SweepBranches(context.Background(), true /* includeClosedOrphans */, true /* dryRun */)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	if len(report.Deleted) != 1 {
		t.Fatalf("Deleted count = %d, want 1", len(report.Deleted))
	}
	if report.Deleted[0].Reason != "closed-orphan" {
		t.Errorf("Deleted[0].Reason = %q, want 'closed-orphan'", report.Deleted[0].Reason)
	}
	if len(report.Preserved) != 0 {
		t.Errorf("Preserved count = %d, want 0", len(report.Preserved))
	}
}

// TestSweepBranches_ClosedOrphanExcluded verifies that a closed writ is
// preserved (not deleted) when includeClosedOrphans=false.
func TestSweepBranches_ClosedOrphanExcluded(t *testing.T) {
	const branch = "outpost/Toast/sol-closed0001"
	const writID = "sol-closed0001"

	worldStore := newMockWorldStore()
	worldStore.items[writID] = &store.Writ{
		ID:     writID,
		Status: store.WritClosed,
	}

	r := newSweepForge(worldStore)
	mock := r.cmd.(*mockCmdRunner)
	setupSweepMock(mock,
		[]string{branch},
		nil,
		nil,
		map[string]bool{writID: false},
	)

	report, err := r.SweepBranches(context.Background(), false /* conservative */, true)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	if len(report.Deleted) != 0 {
		t.Errorf("Deleted count = %d, want 0 (conservative mode)", len(report.Deleted))
	}
	if len(report.Preserved) != 1 {
		t.Fatalf("Preserved count = %d, want 1", len(report.Preserved))
	}
	if report.Preserved[0].Reason != "not-merged" {
		t.Errorf("Preserved[0].Reason = %q, want 'not-merged'", report.Preserved[0].Reason)
	}
}

// TestSweepBranches_WorktreeCheckedOut verifies that a branch currently
// checked out in a worktree is preserved regardless of writ status.
func TestSweepBranches_WorktreeCheckedOut(t *testing.T) {
	const branch = "outpost/Toast/sol-live00001"
	const writID = "sol-live00001"

	worldStore := newMockWorldStore()
	r := newSweepForge(worldStore)
	mock := r.cmd.(*mockCmdRunner)
	setupSweepMock(mock,
		[]string{branch},
		nil,
		[]string{branch}, // branch IS checked out
		map[string]bool{writID: true}, // even if writ is on target, preserve
	)

	report, err := r.SweepBranches(context.Background(), true, true)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	if len(report.Deleted) != 0 {
		t.Errorf("Deleted count = %d, want 0 (branch checked out)", len(report.Deleted))
	}
	if len(report.Preserved) != 1 {
		t.Fatalf("Preserved count = %d, want 1", len(report.Preserved))
	}
	if report.Preserved[0].Reason != "worktree-checked-out" {
		t.Errorf("Preserved[0].Reason = %q, want 'worktree-checked-out'",
			report.Preserved[0].Reason)
	}
}

// TestSweepBranches_NotMergedNotClosed verifies that a branch whose writ is
// not on target and not closed is preserved.
func TestSweepBranches_NotMergedNotClosed(t *testing.T) {
	const branch = "outpost/Toast/sol-active0001"
	const writID = "sol-active0001"

	worldStore := newMockWorldStore()
	worldStore.items[writID] = &store.Writ{
		ID:     writID,
		Status: store.WritWorking,
	}

	r := newSweepForge(worldStore)
	mock := r.cmd.(*mockCmdRunner)
	setupSweepMock(mock,
		[]string{branch},
		nil,
		nil,
		map[string]bool{writID: false},
	)

	report, err := r.SweepBranches(context.Background(), true, true)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	if len(report.Deleted) != 0 {
		t.Errorf("Deleted count = %d, want 0 (writ still working)", len(report.Deleted))
	}
	if len(report.Preserved) != 1 {
		t.Fatalf("Preserved count = %d, want 1", len(report.Preserved))
	}
	if report.Preserved[0].Reason != "not-merged" {
		t.Errorf("Preserved[0].Reason = %q, want 'not-merged'", report.Preserved[0].Reason)
	}
}

// TestSweepBranches_EnvoyBranchWithWritID verifies that envoy branches that
// carry a writ-ID suffix are treated correctly (deleted if merged).
func TestSweepBranches_EnvoyBranchWithWritID(t *testing.T) {
	const branch = "envoy/world/Envoy/sol-envoy0001"
	const writID = "sol-envoy0001"

	worldStore := newMockWorldStore()
	r := newSweepForge(worldStore)
	mock := r.cmd.(*mockCmdRunner)
	setupSweepMock(mock,
		[]string{branch},
		nil,
		nil,
		map[string]bool{writID: true}, // writ IS on target
	)

	report, err := r.SweepBranches(context.Background(), false, true)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	if len(report.Deleted) != 1 {
		t.Fatalf("Deleted count = %d, want 1", len(report.Deleted))
	}
	if report.Deleted[0].Branch != branch {
		t.Errorf("Deleted[0].Branch = %q, want %q", report.Deleted[0].Branch, branch)
	}
	if report.Deleted[0].Reason != "merged" {
		t.Errorf("Deleted[0].Reason = %q, want 'merged'", report.Deleted[0].Reason)
	}
}

// TestSweepBranches_MixedCandidates verifies that a sweep with a mix of
// merged, not-merged, and checked-out branches produces the correct report.
func TestSweepBranches_MixedCandidates(t *testing.T) {
	branches := []string{
		"outpost/Toast/sol-merged0001", // on target → delete
		"outpost/Blaze/sol-inflight01", // not on target, writ working → preserve
		"outpost/Nova/sol-checkedout1", // checked out → preserve
	}

	worldStore := newMockWorldStore()
	worldStore.items["sol-inflight01"] = &store.Writ{
		ID:     "sol-inflight01",
		Status: store.WritWorking,
	}

	r := newSweepForge(worldStore)
	mock := r.cmd.(*mockCmdRunner)
	setupSweepMock(mock,
		branches,
		nil,
		[]string{"outpost/Nova/sol-checkedout1"}, // this branch is checked out
		map[string]bool{
			"sol-merged0001": true,
			"sol-inflight01": false,
			"sol-checkedout1": true, // doesn't matter — checked-out takes priority
		},
	)

	report, err := r.SweepBranches(context.Background(), false, true)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	if report.Inspected != 3 {
		t.Errorf("Inspected = %d, want 3", report.Inspected)
	}
	if len(report.Deleted) != 1 {
		t.Errorf("Deleted count = %d, want 1", len(report.Deleted))
	}
	if len(report.Deleted) > 0 && report.Deleted[0].Reason != "merged" {
		t.Errorf("Deleted[0].Reason = %q, want 'merged'", report.Deleted[0].Reason)
	}
	if len(report.Preserved) != 2 {
		t.Errorf("Preserved count = %d, want 2", len(report.Preserved))
	}
}

// TestSweepBranches_LocalOnlyBranch verifies that a branch present only in
// local refs (no remote counterpart) is also swept when its writ is on target.
func TestSweepBranches_LocalOnlyBranch(t *testing.T) {
	const branch = "outpost/Toast/sol-localonly"
	const writID = "sol-localonly"

	worldStore := newMockWorldStore()
	r := newSweepForge(worldStore)
	mock := r.cmd.(*mockCmdRunner)
	// Only in local, not in remote listing.
	setupSweepMock(mock,
		nil,           // no remote branches
		[]string{branch}, // local only
		nil,
		map[string]bool{writID: true},
	)

	report, err := r.SweepBranches(context.Background(), false, true)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	if len(report.Deleted) != 1 {
		t.Fatalf("Deleted count = %d, want 1", len(report.Deleted))
	}
	if report.Deleted[0].Branch != branch {
		t.Errorf("Deleted[0].Branch = %q, want %q", report.Deleted[0].Branch, branch)
	}
}

// TestSweepBranches_NoBranchesFound verifies that a sweep with no candidates
// returns an empty report with no errors.
func TestSweepBranches_NoBranchesFound(t *testing.T) {
	worldStore := newMockWorldStore()
	r := newSweepForge(worldStore)
	mock := r.cmd.(*mockCmdRunner)
	setupSweepMock(mock, nil, nil, nil, nil)

	report, err := r.SweepBranches(context.Background(), false, true)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	if report.Inspected != 0 {
		t.Errorf("Inspected = %d, want 0", report.Inspected)
	}
	if len(report.Deleted) != 0 {
		t.Errorf("Deleted count = %d, want 0", len(report.Deleted))
	}
	if len(report.Preserved) != 0 {
		t.Errorf("Preserved count = %d, want 0", len(report.Preserved))
	}
	if len(report.Errors) != 0 {
		t.Errorf("Errors count = %d, want 0", len(report.Errors))
	}
}

// TestSweepBranches_TargetBranchNotConfigured verifies that SweepBranches
// returns an error when the target branch is not configured.
func TestSweepBranches_TargetBranchNotConfigured(t *testing.T) {
	worldStore := newMockWorldStore()
	r := &Forge{
		world:      "ember",
		sourceRepo: "/fake/repo",
		worldStore: worldStore,
		logger:     testLogger(),
		cfg:        Config{TargetBranch: ""}, // not configured
		cmd:        newMockCmdRunner(),
	}

	_, err := r.SweepBranches(context.Background(), false, true)
	if err == nil {
		t.Fatal("expected error when target branch not configured")
	}
}

// --- Real-git integration tests ---

// setupSweepRepo creates a real git repo with:
//   - main branch with a commit tagged "sol-merged0001"
//   - outpost/Toast/sol-merged0001 (writ on target — safe to delete)
//   - outpost/Toast/sol-notmerged1 (writ NOT on target — preserve)
//
// Returns the sourceRepo path.
func setupSweepRepo(t *testing.T) (sourceRepo, mergedBranch, notMergedBranch string) {
	t.Helper()
	dir := t.TempDir()

	bare := filepath.Join(dir, "origin.git")
	run(t, "git", "init", "--bare", bare)

	work := filepath.Join(dir, "work")
	run(t, "git", "clone", bare, work)
	run(t, "git", "-C", work, "config", "user.email", "t@t.com")
	run(t, "git", "-C", work, "config", "user.name", "Test")

	mergedBranch = "outpost/Toast/sol-merged0001"
	notMergedBranch = "outpost/Toast/sol-notmerged1"

	// Initial commit.
	os.WriteFile(filepath.Join(work, "README"), []byte("init\n"), 0o644)
	run(t, "git", "-C", work, "add", ".")
	run(t, "git", "-C", work, "commit", "-m", "init")

	// Squash-merge commit for merged branch (tagged with writ ID).
	os.WriteFile(filepath.Join(work, "merged.txt"), []byte("merged work\n"), 0o644)
	run(t, "git", "-C", work, "add", ".")
	run(t, "git", "-C", work, "commit", "-m", "Add merged feature (sol-merged0001)")
	run(t, "git", "-C", work, "push", "origin", "HEAD:main")

	// Push merged branch pointing at main tip.
	run(t, "git", "-C", work, "push", "origin", "HEAD:refs/heads/"+mergedBranch)

	// Unmerged branch: off initial commit, NOT in main.
	initSHA := run(t, "git", "-C", work, "rev-parse", "HEAD~1")
	run(t, "git", "-C", work, "checkout", "--detach", initSHA)
	os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature\n"), 0o644)
	run(t, "git", "-C", work, "add", ".")
	run(t, "git", "-C", work, "commit", "-m", "Unmerged feature (sol-notmerged1)")
	run(t, "git", "-C", work, "push", "origin", "HEAD:refs/heads/"+notMergedBranch)

	// Restore to main.
	run(t, "git", "-C", work, "checkout", "main")
	run(t, "git", "-C", work, "fetch", "origin")

	return work, mergedBranch, notMergedBranch
}

// TestSweepBranches_RealGitDeletesMergedBranch exercises the full SweepBranches
// flow against a real git repository, verifying that the merged branch is
// actually deleted from both remote and local refs.
func TestSweepBranches_RealGitDeletesMergedBranch(t *testing.T) {
	sourceRepo, merged, notMerged := setupSweepRepo(t)

	worldStore := newMockWorldStore()
	worldStore.items["sol-notmerged1"] = &store.Writ{
		ID:     "sol-notmerged1",
		Status: store.WritWorking,
	}

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:      "ember",
		agentID:    "ember/forge",
		sourceRepo: sourceRepo,
		worldStore: worldStore,
		logger:     testLogger(),
		cfg:        forgeCfg,
		cmd:        &realCmdRunner{},
	}

	report, err := r.SweepBranches(context.Background(), false, false /* not dryRun */)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	// Merged branch should be in the deleted list.
	if len(report.Deleted) != 1 {
		t.Errorf("Deleted count = %d, want 1; report=%+v", len(report.Deleted), report)
	} else if report.Deleted[0].Branch != merged {
		t.Errorf("Deleted[0].Branch = %q, want %q", report.Deleted[0].Branch, merged)
	} else if report.Deleted[0].Reason != "merged" {
		t.Errorf("Deleted[0].Reason = %q, want 'merged'", report.Deleted[0].Reason)
	}

	// Not-merged branch should be preserved.
	if len(report.Preserved) != 1 {
		t.Errorf("Preserved count = %d, want 1", len(report.Preserved))
	} else if report.Preserved[0].Branch != notMerged {
		t.Errorf("Preserved[0].Branch = %q, want %q", report.Preserved[0].Branch, notMerged)
	}

	// Verify the merged branch ref was actually removed from origin.
	run(t, "git", "-C", sourceRepo, "fetch", "origin", "--prune")
	if err := exec.Command("git", "-C", sourceRepo, "rev-parse", "--verify", "--quiet",
		"refs/remotes/origin/"+merged).Run(); err == nil {
		t.Errorf("remote branch %s should have been deleted", merged)
	}

	// Verify the not-merged branch still exists on origin.
	if err := exec.Command("git", "-C", sourceRepo, "rev-parse", "--verify", "--quiet",
		"refs/remotes/origin/"+notMerged).Run(); err != nil {
		t.Errorf("remote branch %s should still exist", notMerged)
	}
}

// TestSweepBranches_RealGitDryRunPreservesAll verifies that dry-run mode
// reports what would be deleted without actually deleting anything.
func TestSweepBranches_RealGitDryRunPreservesAll(t *testing.T) {
	sourceRepo, merged, _ := setupSweepRepo(t)

	worldStore := newMockWorldStore()
	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:      "ember",
		agentID:    "ember/forge",
		sourceRepo: sourceRepo,
		worldStore: worldStore,
		logger:     testLogger(),
		cfg:        forgeCfg,
		cmd:        &realCmdRunner{},
	}

	report, err := r.SweepBranches(context.Background(), false, true /* dryRun */)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	// Dry-run: merged branch should still appear as "would delete".
	if len(report.Deleted) != 1 {
		t.Fatalf("Deleted count = %d, want 1 (dry-run should report planned deletes)", len(report.Deleted))
	}
	if report.DryRun != true {
		t.Error("DryRun should be true in report")
	}

	// Branch should still exist (no actual deletion performed).
	run(t, "git", "-C", sourceRepo, "fetch", "origin", "--prune")
	if err := exec.Command("git", "-C", sourceRepo, "rev-parse", "--verify", "--quiet",
		"refs/remotes/origin/"+merged).Run(); err != nil {
		t.Errorf("dry-run: remote branch %s should still exist", merged)
	}
}

// TestSweepBranches_RealGitWorktreeCheckedOut verifies that a branch checked
// out in a worktree is preserved even if its writ is on target.
func TestSweepBranches_RealGitWorktreeCheckedOut(t *testing.T) {
	sourceRepo, merged, _ := setupSweepRepo(t)

	// Add a worktree that checks out the merged branch.
	wtPath := filepath.Join(filepath.Dir(sourceRepo), "worktree1")
	run(t, "git", "-C", sourceRepo, "fetch", "origin")
	// Create a local branch tracking the remote.
	run(t, "git", "-C", sourceRepo, "branch", "--track",
		"outpost/Toast/sol-merged0001", "origin/outpost/Toast/sol-merged0001")
	run(t, "git", "-C", sourceRepo, "worktree", "add", wtPath, merged)
	defer exec.Command("git", "-C", sourceRepo, "worktree", "remove", "--force", wtPath).Run() //nolint

	worldStore := newMockWorldStore()
	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:      "ember",
		agentID:    "ember/forge",
		sourceRepo: sourceRepo,
		worldStore: worldStore,
		logger:     testLogger(),
		cfg:        forgeCfg,
		cmd:        &realCmdRunner{},
	}

	report, err := r.SweepBranches(context.Background(), false, true /* dryRun */)
	if err != nil {
		t.Fatalf("SweepBranches() error: %v", err)
	}

	// The checked-out branch should be in Preserved, not Deleted.
	for _, e := range report.Deleted {
		if e.Branch == merged {
			t.Errorf("checked-out branch %s should not be deleted", merged)
		}
	}
	var found bool
	for _, e := range report.Preserved {
		if e.Branch == merged && e.Reason == "worktree-checked-out" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s in Preserved with reason='worktree-checked-out', got: %+v",
			merged, report.Preserved)
	}
}

// TestSweepBranches_RealGitClosedOrphan verifies the closed-orphan path:
// a branch whose writ is closed in the DB (not on target) is deleted when
// includeClosedOrphans=true.
func TestSweepBranches_RealGitClosedOrphan(t *testing.T) {
	_, notMergedBranch := func() (string, string) {
		dir := t.TempDir()
		bare := filepath.Join(dir, "origin.git")
		run(t, "git", "init", "--bare", bare)
		work := filepath.Join(dir, "work")
		run(t, "git", "clone", bare, work)
		run(t, "git", "-C", work, "config", "user.email", "t@t.com")
		run(t, "git", "-C", work, "config", "user.name", "Test")

		// Initial commit on main.
		os.WriteFile(filepath.Join(work, "README"), []byte("init\n"), 0o644)
		run(t, "git", "-C", work, "add", ".")
		run(t, "git", "-C", work, "commit", "-m", "init")
		run(t, "git", "-C", work, "push", "origin", "HEAD:main")

		// A branch whose writ was force-reset out of history.
		notMerged := "outpost/Toast/sol-forcereset"
		run(t, "git", "-C", work, "push", "origin", "HEAD:refs/heads/"+notMerged)
		run(t, "git", "-C", work, "fetch", "origin")
		return work, notMerged
	}()

	sourceRepo := func() string {
		dir := t.TempDir()
		bare := filepath.Join(dir, "origin.git")
		run(t, "git", "init", "--bare", bare)
		work := filepath.Join(dir, "work")
		run(t, "git", "clone", bare, work)
		run(t, "git", "-C", work, "config", "user.email", "t@t.com")
		run(t, "git", "-C", work, "config", "user.name", "Test")
		os.WriteFile(filepath.Join(work, "README"), []byte("init\n"), 0o644)
		run(t, "git", "-C", work, "add", ".")
		run(t, "git", "-C", work, "commit", "-m", "init")
		run(t, "git", "-C", work, "push", "origin", "HEAD:main")
		const branch = "outpost/Toast/sol-forcereset"
		run(t, "git", "-C", work, "push", "origin", "HEAD:refs/heads/"+branch)
		run(t, "git", "-C", work, "fetch", "origin")
		return work
	}()
	_ = notMergedBranch

	const branch = "outpost/Toast/sol-forcereset"
	const writID = "sol-forcereset"

	worldStore := newMockWorldStore()
	worldStore.items[writID] = &store.Writ{
		ID:     writID,
		Status: store.WritClosed, // writ is closed (force-reset scenario)
	}

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:      "ember",
		agentID:    "ember/forge",
		sourceRepo: sourceRepo,
		worldStore: worldStore,
		logger:     testLogger(),
		cfg:        forgeCfg,
		cmd:        &realCmdRunner{},
	}

	// Conservative mode: should preserve.
	reportConservative, err := r.SweepBranches(context.Background(), false, true)
	if err != nil {
		t.Fatalf("conservative SweepBranches() error: %v", err)
	}
	if len(reportConservative.Deleted) != 0 {
		t.Errorf("conservative: Deleted count = %d, want 0", len(reportConservative.Deleted))
	}

	// Aggressive mode: should delete.
	reportAggressive, err := r.SweepBranches(context.Background(), true /* includeClosedOrphans */, false)
	if err != nil {
		t.Fatalf("aggressive SweepBranches() error: %v", err)
	}
	if len(reportAggressive.Deleted) != 1 {
		t.Errorf("aggressive: Deleted count = %d, want 1", len(reportAggressive.Deleted))
	} else if reportAggressive.Deleted[0].Reason != "closed-orphan" {
		t.Errorf("aggressive: Deleted[0].Reason = %q, want 'closed-orphan'",
			reportAggressive.Deleted[0].Reason)
	}
}
