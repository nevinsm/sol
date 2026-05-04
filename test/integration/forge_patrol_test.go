package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/store"
)

// TestForgeTTLClaimRelease verifies that stale MR claims (>30 min old) are
// automatically released back to "ready" by ReleaseStaleClaims — the TTL
// recovery mechanism documented in LOOP2_ACCEPTANCE.md §7.
//
// This is an integration test because it exercises the real SQLite store
// end-to-end: the timestamp backdating uses raw SQL and the release
// uses the production code path.
func TestForgeTTLClaimRelease(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnvWithRepo(t)
	_ = gtHome

	worldStore, _ := openStores(t, "ttltest")

	// Create a writ.
	writID, err := worldStore.CreateWrit("TTL test writ", "TTL recovery test", "test", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Create an MR.
	mrID, err := worldStore.CreateMergeRequest(writID, "outpost/Agent/"+writID, 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}

	// Claim the MR.
	claimed, err := worldStore.ClaimMergeRequest("forge/ttltest", 0)
	if err != nil {
		t.Fatalf("ClaimMergeRequest: %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatalf("expected to claim MR %s, got %v", mrID, claimed)
	}

	// Verify it's in claimed state.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.Phase != store.MRClaimed {
		t.Fatalf("MR phase = %q before backdate, want %q", mr.Phase, store.MRClaimed)
	}

	// Backdate claimed_at by 31 minutes to simulate a stale claim.
	staleTime := time.Now().UTC().Add(-31 * time.Minute).Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`UPDATE merge_requests SET claimed_at = ? WHERE id = ?`, staleTime, mrID,
	); err != nil {
		t.Fatalf("backdate claimed_at: %v", err)
	}

	// Run ReleaseStaleClaims with a 30-minute TTL — should release the backdated MR.
	released, err := worldStore.ReleaseStaleClaims(30*time.Minute, 0)
	if err != nil {
		t.Fatalf("ReleaseStaleClaims: %v", err)
	}
	if released != 1 {
		t.Errorf("ReleaseStaleClaims returned %d, want 1", released)
	}

	// Verify the MR is back to ready phase with cleared claim fields.
	mr, err = worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest after release: %v", err)
	}
	if mr.Phase != store.MRReady {
		t.Errorf("MR phase = %q, want %q", mr.Phase, store.MRReady)
	}
	if mr.ClaimedBy != "" {
		t.Errorf("MR claimed_by = %q, want empty", mr.ClaimedBy)
	}
	if mr.ClaimedAt != nil {
		t.Errorf("MR claimed_at = %v, want nil", mr.ClaimedAt)
	}
}

// TestForgeTTLClaimRelease_FreshClaimsNotReleased verifies that fresh claims
// (claimed within the TTL window) are not released by ReleaseStaleClaims.
func TestForgeTTLClaimRelease_FreshClaimsNotReleased(t *testing.T) {
	skipUnlessIntegration(t)

	_, _ = setupTestEnvWithRepo(t)

	worldStore, _ := openStores(t, "ttlfreshtest")

	// Create writ and MR.
	writID, err := worldStore.CreateWrit("Fresh claim writ", "Fresh claim test", "test", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	mrID, err := worldStore.CreateMergeRequest(writID, "outpost/Agent/"+writID, 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}

	// Claim now (claimed_at = now, so it is fresh).
	claimed, err := worldStore.ClaimMergeRequest("forge/ttlfreshtest", 0)
	if err != nil {
		t.Fatalf("ClaimMergeRequest: %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatalf("expected to claim MR %s", mrID)
	}

	// ReleaseStaleClaims with 30-min TTL — fresh claim should NOT be released.
	released, err := worldStore.ReleaseStaleClaims(30*time.Minute, 0)
	if err != nil {
		t.Fatalf("ReleaseStaleClaims: %v", err)
	}
	if released != 0 {
		t.Errorf("ReleaseStaleClaims returned %d, want 0 (claim is fresh)", released)
	}

	// Verify MR is still claimed.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.Phase != store.MRClaimed {
		t.Errorf("MR phase = %q, want %q (claim is fresh, should not be released)", mr.Phase, store.MRClaimed)
	}
}

// TestForgeSessionEndToEnd exercises the complete session-based forge merge
// pipeline with real git repositories. It:
//  1. Creates a real bare git repo + working clone
//  2. Creates a writ and MR in the store
//  3. Simulates agent work: creates a branch, commits a file, pushes to origin
//  4. Runs one forge patrol cycle; a goroutine simulates the Claude session by
//     performing the real git merge and writing a "merged" result file
//  5. Verifies: MR phase → merged, commit present in origin/main, writ closed
//
// This is the integration-level counterpart to the unit-level
// TestPatrolSessionPathSuccessfulMerge in internal/forge/patrol_test.go.
// It exercises real git operations (fetch, merge, push) rather than mocking
// the command runner.
func TestForgeSessionEndToEnd(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnvWithRepo(t)
	_ = gtHome

	// Create a bare origin repo and a working clone with "origin" configured.
	_, workingClone := createSourceRepo(t, gtHome)

	// Initialize the world so startup.Launch (called inside patrol) can load
	// world config and find the worktree path.
	setupWorld(t, gtHome, "forgetest", workingClone)

	// Open world and sphere stores for writ/MR operations.
	worldStore, sphereStore := openStores(t, "forgetest")

	// Create a writ.
	writID, err := worldStore.CreateWrit(
		"Add end-to-end feature",
		"Integration test: session-based forge merge with real git repos",
		"test", 2, nil,
	)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Simulate agent work: create a feature branch, commit a change, push.
	branch := "outpost/TestAgent/" + writID
	runGit(t, workingClone, "checkout", "-b", branch)
	writeTestFile(t, filepath.Join(workingClone, "feature.go"), "package main\n\nfunc feature() {}\n")
	runGit(t, workingClone, "add", "feature.go")
	runGit(t, workingClone, "commit", "-m", "Add end-to-end feature ("+writID+")")
	runGit(t, workingClone, "push", "origin", branch)
	runGit(t, workingClone, "checkout", "main")

	// Create MR in ready state.
	mrID, err := worldStore.CreateMergeRequest(writID, branch, 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}

	// Build the forge with a mock session manager so no real Claude process
	// is started (SOL_SESSION_COMMAND=sleep 300 is already set by isolateTmux,
	// and startup.Launch delegates the actual Start call to the mock).
	forgeCfg := forge.DefaultConfig()
	forgeCfg.TargetBranch = "main"
	forgeCfg.QualityGates = nil // no quality gates — test focuses on merge path
	forgeCfg.MaxAttempts = 3
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sessMgr := newMockSessionChecker()
	f := forge.New("forgetest", workingClone, worldStore, sphereStore, forgeCfg, logger, sessMgr)

	// Create the forge worktree: a linked worktree from workingClone at origin/main.
	if err := f.EnsureWorktree(); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	worktreeDir := forge.WorktreePath("forgetest")

	// Configure git identity in the forge worktree so merge commits succeed.
	runGit(t, worktreeDir, "config", "user.email", "test@test.com")
	runGit(t, worktreeDir, "config", "user.name", "Test")

	// sessionName mirrors mergeSessionName("forgetest") = "sol-forgetest-forge-merge".
	sessionName := "sol-forgetest-forge-merge"

	// Canary: signal once the patrol's monitor loop is set up and ready to
	// observe events. Replaces a fragile fixed-duration sleep buffer with a
	// deterministic synchronisation point. The buffered channel is sized 1
	// so the hook is non-blocking even if the test gives up early; the hook
	// itself drops further signals via the default branch.
	monitorStarted := make(chan struct{}, 1)
	f.SetMonitorStartedHook(func(_ string) {
		select {
		case monitorStarted <- struct{}{}:
		default:
		}
	})

	// Goroutine simulates the Claude session:
	//   waits until forge starts it → performs the real git merge →
	//   writes the result file → removes the session (simulates exit).
	goroutineErr := make(chan error, 1)
	go func() {
		// Poll until the forge launches the session via startup.Launch → mock.Start.
		if !pollUntil(3*time.Second, 10*time.Millisecond, func() bool {
			return sessMgr.Exists(sessionName)
		}) {
			goroutineErr <- fmt.Errorf("session %q never started after 3s", sessionName)
			return
		}

		// Wait until patrol's monitorSession has entered its observation
		// loop (timers set up, ready to detect the result file / session
		// exit on the next tick). Without this, the test relied on a 30 ms
		// sleep that was fragile under load.
		select {
		case <-monitorStarted:
		case <-time.After(3 * time.Second):
			goroutineErr <- fmt.Errorf("monitor loop never started after 3s")
			return
		}

		// Perform real git operations in the forge worktree:
		//   fetch origin, merge the feature branch, push to origin/main.
		gitEnv := append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		for _, args := range [][]string{
			{"fetch", "origin"},
			{"merge", "--no-ff", "origin/" + branch, "-m", "Add end-to-end feature (" + writID + ")"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = worktreeDir
			cmd.Env = gitEnv
			if out, runErr := cmd.CombinedOutput(); runErr != nil {
				goroutineErr <- fmt.Errorf("git %v: %s: %v", args, out, runErr)
				return
			}
		}

		// Push the merged detached HEAD to origin/main.
		pushCmd := exec.Command("git", "push", "origin", "HEAD:main")
		pushCmd.Dir = worktreeDir
		if out, runErr := pushCmd.CombinedOutput(); runErr != nil {
			goroutineErr <- fmt.Errorf("git push origin HEAD:main: %s: %v", out, runErr)
			return
		}

		// Write .forge-result.json to signal a successful merge.
		// The file name matches the resultFileName constant in the forge package.
		result := forge.ForgeResult{
			Result:  "merged",
			Summary: "Merged by integration test goroutine",
		}
		data, _ := json.Marshal(result)
		if writeErr := os.WriteFile(
			filepath.Join(worktreeDir, ".forge-result.json"), data, 0o644,
		); writeErr != nil {
			goroutineErr <- fmt.Errorf("write result file: %v", writeErr)
			return
		}

		// Stop session so monitorSession detects exit and returns sessionCompleted.
		sessMgr.Stop(sessionName, false)
		goroutineErr <- nil
	}()

	// Run exactly one patrol cycle. The cycle claims the MR, launches the
	// "session" (mock start), monitors it, picks up the result file, runs
	// verifyPush (real git fetch + log grep), and marks the MR merged.
	pcfg := forge.DefaultPatrolConfig("forgetest")
	pcfg.WaitTimeout = 500 * time.Millisecond
	pcfg.MonitorInterval = 200 * time.Millisecond
	pcfg.AssessCommand = "echo assessment-stub"
	pcfg.AssessTimeout = 1 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := f.RunPatrol(ctx, pcfg); err != nil {
		t.Fatalf("RunPatrol: %v", err)
	}

	// Collect goroutine result — it must have completed before patrol returned.
	select {
	case gErr := <-goroutineErr:
		if gErr != nil {
			t.Fatalf("simulation goroutine failed: %v", gErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("simulation goroutine timed out after RunPatrol returned")
	}

	// 1. Verify MR is marked merged.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.Phase != store.MRMerged {
		t.Errorf("MR phase = %q, want %q", mr.Phase, store.MRMerged)
	}

	// 2. Verify the feature commit is present in origin/main.
	if out, fetchErr := exec.Command("git", "-C", workingClone, "fetch", "origin").CombinedOutput(); fetchErr != nil {
		t.Fatalf("git fetch origin: %s: %v", out, fetchErr)
	}
	logOut := runGitOutput(t, workingClone, "log", "origin/main", "--oneline", "--grep", writID)
	if !strings.Contains(logOut, writID) {
		t.Errorf("writ %s not found in origin/main commits:\n%s", writID, logOut)
	}

	// 3. Verify writ is closed.
	writ, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if writ.Status != store.WritClosed {
		t.Errorf("writ status = %q, want %q", writ.Status, store.WritClosed)
	}
}

// TestForgeDirtyWorktreeRecovery verifies the crash recovery path documented
// in docs/failure-modes.md (lines 98-100):
//
//	"Crash during merge (after git merge --squash, before push): the worktree
//	is dirty. The next patrol cycle runs git reset --hard in the sync step,
//	restoring a clean slate."
//
// The test:
//  1. Sets up a forge worktree and dirties it (simulating a mid-merge crash)
//  2. Runs a forge patrol cycle
//  3. Verifies the worktree is clean after the patrol
//
// The cleanup happens via cleanupSession (deferred in executeMergeSession):
// the patrol claims the MR → runMergeSession's pre-flight detects the dirty
// worktree and returns an error → cleanupSession runs git reset --hard +
// git clean -fd → worktree is restored to a clean state.
func TestForgeDirtyWorktreeRecovery(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnvWithRepo(t)

	// Create a bare origin repo and a working clone.
	_, workingClone := createSourceRepo(t, gtHome)

	// Initialize the world.
	setupWorld(t, gtHome, "forgeclean", workingClone)

	worldStore, sphereStore := openStores(t, "forgeclean")

	// Create a writ.
	writID, err := worldStore.CreateWrit(
		"Dirty worktree recovery",
		"Test that patrol cleans up a dirty worktree left by a crash",
		"test", 2, nil,
	)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Simulate agent work: create a feature branch, commit, push.
	branch := "outpost/CleanBot/" + writID
	runGit(t, workingClone, "checkout", "-b", branch)
	writeTestFile(t, filepath.Join(workingClone, "dirty.go"), "package main\n\nfunc dirty() {}\n")
	runGit(t, workingClone, "add", "dirty.go")
	runGit(t, workingClone, "commit", "-m", "Add dirty file ("+writID+")")
	runGit(t, workingClone, "push", "origin", branch)
	runGit(t, workingClone, "checkout", "main")

	// Create MR in ready state.
	_, err = worldStore.CreateMergeRequest(writID, branch, 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}

	// Build the forge.
	forgeCfg := forge.DefaultConfig()
	forgeCfg.TargetBranch = "main"
	forgeCfg.QualityGates = nil
	forgeCfg.MaxAttempts = 3
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	sessMgr := newMockSessionChecker()
	f := forge.New("forgeclean", workingClone, worldStore, sphereStore, forgeCfg, logger, sessMgr)

	// Create the forge worktree.
	if err := f.EnsureWorktree(); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	worktreeDir := forge.WorktreePath("forgeclean")

	// Configure git identity in the forge worktree.
	runGit(t, worktreeDir, "config", "user.email", "test@test.com")
	runGit(t, worktreeDir, "config", "user.name", "Test")

	// --- Simulate a mid-merge crash: dirty the forge worktree ---
	// Write uncommitted files (as if git merge --squash ran but the process
	// crashed before push/commit).
	writeTestFile(t, filepath.Join(worktreeDir, "dirty.go"), "package main\n\nfunc dirty() {}\n")
	writeTestFile(t, filepath.Join(worktreeDir, "conflict-marker.go"), "package main\n<<<<<<< HEAD\nfunc a() {}\n=======\nfunc b() {}\n>>>>>>> branch\n")

	// Verify the worktree IS dirty before patrol.
	statusCmd := exec.Command("git", "-C", worktreeDir, "status", "--porcelain")
	statusOut, err := statusCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status before patrol: %v: %s", err, statusOut)
	}
	if strings.TrimSpace(string(statusOut)) == "" {
		t.Fatal("worktree should be dirty before patrol, but git status is clean")
	}

	// --- Run one patrol cycle ---
	// The patrol will claim the MR, call executeMergeSession (defer cleanupSession),
	// runMergeSession will fail at the pre-flight dirty-worktree check, and
	// cleanupSession will reset the worktree.
	pcfg := forge.DefaultPatrolConfig("forgeclean")
	pcfg.WaitTimeout = 500 * time.Millisecond
	pcfg.MonitorInterval = 200 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := f.RunPatrol(ctx, pcfg); err != nil {
		t.Fatalf("RunPatrol: %v", err)
	}

	// --- Verify the worktree is clean after patrol ---
	statusCmd = exec.Command("git", "-C", worktreeDir, "status", "--porcelain")
	statusOut, err = statusCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status after patrol: %v: %s", err, statusOut)
	}
	if got := strings.TrimSpace(string(statusOut)); got != "" {
		t.Errorf("worktree should be clean after patrol, but git status --porcelain returned:\n%s", got)
	}
}
