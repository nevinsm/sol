package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// --- Test: Sentinel Reaps Agent Tethered to Closed Writ ---
//
// Exercises the failure mode documented in docs/failure-modes.md:234-239:
// when a writ is closed (cancelled, superseded) while an agent is working,
// sentinel detects the closed writ on its next patrol and reaps the agent.

func TestSentinelReapsAgentOnClosedWrit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)
	registerAgentRole(t)

	worldStore, sphereStore := openStores(t, "ember")
	logger := events.NewLogger(solHome)
	mock := newMockSessionChecker()

	const (
		agentName = "Toast"
		writID    = "sol-abc1234500000000"
	)

	// Create a working agent with a live session tethered to a writ.
	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/"+agentName, store.AgentWorking, writID); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Create writ in the world store.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'tethered', 3, 'test', ?, ?)`,
		writID, "Cancelled task", now, now,
	); err != nil {
		t.Fatalf("insert writ: %v", err)
	}

	// Write tether file so patrol discovers it via tether.List().
	if err := tether.Write("ember", agentName, writID, "outpost"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	// Session is alive.
	sessionName := "sol-ember-" + agentName
	mock.mu.Lock()
	mock.alive[sessionName] = true
	mock.mu.Unlock()

	// Close the writ externally (simulating operator `sol writ close`).
	if _, err := worldStore.CloseWrit(writID, "superseded"); err != nil {
		t.Fatalf("CloseWrit: %v", err)
	}

	// Create sentinel and run one patrol.
	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond
	cfg.MaxRespawns = 2

	w := sentinel.New(cfg, sphereStore, worldStore, mock, logger)

	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Verify: session was stopped.
	mock.mu.Lock()
	stoppedCount := len(mock.stopped)
	var stoppedName string
	if stoppedCount > 0 {
		stoppedName = mock.stopped[0]
	}
	mock.mu.Unlock()

	if stoppedCount != 1 {
		t.Fatalf("expected 1 session stopped, got %d", stoppedCount)
	}
	if stoppedName != sessionName {
		t.Errorf("stopped session = %q, want %q", stoppedName, sessionName)
	}

	// Verify: agent record was deleted (outpost agents are reaped entirely).
	_, err := sphereStore.GetAgent("ember/" + agentName)
	if err == nil {
		t.Error("expected agent to be deleted after reap, but GetAgent succeeded")
	}

	// Verify: tether was cleared.
	if tether.IsTethered("ember", agentName, "outpost") {
		t.Error("tether file should be cleared after reap")
	}

	// Verify: reap event was emitted with close reason.
	eventsFile := filepath.Join(solHome, ".events.jsonl")
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}
	logContent := string(data)
	if !eventsContainField(logContent, "type", "reap") {
		t.Errorf("expected reap event in events log, got:\n%s", logContent)
	}
	if !strings.Contains(logContent, "superseded") {
		t.Errorf("expected close reason 'superseded' in events log, got:\n%s", logContent)
	}
}

// --- Test: Corrupt Workflow State Falls Through to Fresh Launch ---
//
// Exercises the failure mode documented in docs/failure-modes.md:67-69:
// when a workflow state file (.resume_state.json) is corrupted, the agent
// loses its place. Recovery: Respawn detects the corrupt file, logs a
// warning, and falls through to a fresh Launch rather than Resume.

func TestCorruptResumeStateFallsThrough(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)
	registerAgentRole(t)

	worldStore, sphereStore := openStores(t, "ember")
	logger := events.NewLogger(solHome)
	mock := newMockSessionChecker()

	const (
		agentName = "Toast"
		writID    = "sol-abc1234500000000"
	)

	// Create a working agent tethered to a writ.
	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/"+agentName, store.AgentWorking, writID); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Create writ in the world store.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'tethered', 3, 'test', ?, ?)`,
		writID, "Workflow task", now, now,
	); err != nil {
		t.Fatalf("insert writ: %v", err)
	}

	// Write tether file.
	if err := tether.Write("ember", agentName, writID, "outpost"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	// Write corrupt .resume_state.json to the agent's directory.
	// This simulates workflow state corruption — the file exists but
	// contains invalid JSON, so ReadResumeState returns an error.
	agentDir := config.AgentDir("ember", agentName, "outpost")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("create agent dir: %v", err)
	}
	corruptState := []byte("{not valid json!!! current_step: broken")
	if err := os.WriteFile(filepath.Join(agentDir, ".resume_state.json"), corruptState, 0o644); err != nil {
		t.Fatalf("write corrupt resume state: %v", err)
	}

	// Create worktree directory so respawn doesn't fail on missing dir.
	worktreeDir := dispatch.WorktreePath("ember", agentName)
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("create worktree dir: %v", err)
	}

	// Session is dead (not in mock.alive) — sentinel will detect stalled agent.

	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond
	cfg.MaxRespawns = 2

	w := sentinel.New(cfg, sphereStore, worldStore, mock, logger)

	// Run one patrol. Sentinel should detect dead session and attempt respawn.
	// Respawn reads corrupt .resume_state.json, logs warning, falls through
	// to a fresh Launch (graceful degradation per documented recovery path).
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Verify: session was started (respawn fell through to Launch).
	mock.mu.Lock()
	startedCount := len(mock.started)
	mock.mu.Unlock()

	if startedCount != 1 {
		t.Fatalf("expected 1 session started (respawn via Launch), got %d", startedCount)
	}

	// Verify: agent is still in working state (not crashed or deleted).
	agent, err := sphereStore.GetAgent("ember/" + agentName)
	if err != nil {
		t.Fatalf("GetAgent after respawn: %v", err)
	}
	if agent.State != store.AgentWorking {
		t.Errorf("agent state = %q, want %q", agent.State, store.AgentWorking)
	}

	// Verify: respawn event was emitted.
	assertEventEmitted(t, solHome, events.EventRespawn)

	// Verify: corrupt resume state file was consumed/cleared by Respawn.
	// startup.Respawn clears the file regardless of success or failure,
	// preventing retry loops with the same corrupt state.
	resumePath := filepath.Join(agentDir, ".resume_state.json")
	if _, err := os.Stat(resumePath); !os.IsNotExist(err) {
		t.Errorf("expected resume state file to be cleared after respawn, but it still exists")
	}
}

// --- Test: Crash After Push, Before Mark-Merged ---
//
// Exercises the failure mode documented in docs/failure-modes.md (lines 102-105):
// "Crash after push, before mark-merged: the writ is still open and the MR
// is still claimed. On restart, the patrol detects the stale claim (TTL expiry)
// or processes it normally."
//
// The test simulates the intermediate state where the push succeeded (the
// branch exists in origin) but mark-merged never ran (MR is still in
// phase="claimed"). After the TTL expires, ReleaseStaleClaims should recover
// the MR back to "ready" so a subsequent patrol can re-process it.

func TestCrashAfterPushBeforeMarkMerged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnvWithRepo(t)

	// Create a bare origin repo and a working clone with "origin" configured.
	_, workingClone := createSourceRepo(t, gtHome)

	// Initialize the world.
	setupWorld(t, gtHome, "crashtest", workingClone)

	worldStore, _ := openStores(t, "crashtest")

	// Create a writ (still open — mark-merged never ran).
	writID, err := worldStore.CreateWrit(
		"Crash-after-push feature",
		"Test: push succeeded but mark-merged crashed",
		"test", 2, nil,
	)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Simulate agent work: create a feature branch, commit, push to origin.
	branch := "outpost/CrashBot/" + writID
	runGit(t, workingClone, "checkout", "-b", branch)
	writeTestFile(t, filepath.Join(workingClone, "crash-feature.go"), "package main\n\nfunc crashFeature() {}\n")
	runGit(t, workingClone, "add", "crash-feature.go")
	runGit(t, workingClone, "commit", "-m", "Add crash feature ("+writID+")")
	runGit(t, workingClone, "push", "origin", branch)
	runGit(t, workingClone, "checkout", "main")

	// Create MR in ready state, then claim it (simulating forge picking it up).
	mrID, err := worldStore.CreateMergeRequest(writID, branch, 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}

	claimed, err := worldStore.ClaimMergeRequest("forge/crashtest", 3)
	if err != nil {
		t.Fatalf("ClaimMergeRequest: %v", err)
	}
	if claimed == nil || claimed.ID != mrID {
		t.Fatalf("expected to claim MR %s, got %v", mrID, claimed)
	}

	// === THE CRASH HAPPENS HERE ===
	// At this point in a real scenario:
	//   - The forge has claimed the MR (phase=claimed, attempts=1)
	//   - The push to origin succeeded (branch exists with the commit)
	//   - The process crashed BEFORE calling UpdateMergeRequestPhase("merged")
	//   - The writ is still open (status != "closed")

	// Verify pre-conditions: MR is claimed, writ is still open.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.Phase != store.MRClaimed {
		t.Fatalf("MR phase = %q, want %q", mr.Phase, store.MRClaimed)
	}
	if mr.Attempts != 1 {
		t.Fatalf("MR attempts = %d, want 1", mr.Attempts)
	}

	writ, err := worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if writ.Status == store.WritClosed {
		t.Fatal("writ should still be open (mark-merged never ran)")
	}

	// Verify the branch was actually pushed to origin.
	logOut := runGitOutput(t, workingClone, "ls-remote", "origin", branch)
	if !strings.Contains(logOut, branch) {
		t.Fatalf("branch %s not found in origin — push simulation failed", branch)
	}

	// === RECOVERY: TTL expiry ===
	// Backdate claimed_at to simulate the TTL window elapsing after the crash.
	staleTime := time.Now().UTC().Add(-31 * time.Minute).Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`UPDATE merge_requests SET claimed_at = ? WHERE id = ?`, staleTime, mrID,
	); err != nil {
		t.Fatalf("backdate claimed_at: %v", err)
	}

	// ReleaseStaleClaims (called by patrol step 0) should release the MR back
	// to ready, allowing the next patrol to re-process it.
	released, err := worldStore.ReleaseStaleClaims(30*time.Minute, 3)
	if err != nil {
		t.Fatalf("ReleaseStaleClaims: %v", err)
	}
	if released != 1 {
		t.Errorf("ReleaseStaleClaims returned %d, want 1", released)
	}

	// Verify: MR is back to ready with cleared claim fields.
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

	// Verify: the writ is still open — it should NOT have been closed by
	// the TTL release (closing the writ is the responsibility of a
	// subsequent successful merge, not the TTL recovery path).
	writ, err = worldStore.GetWrit(writID)
	if err != nil {
		t.Fatalf("GetWrit after release: %v", err)
	}
	if writ.Status == store.WritClosed {
		t.Error("writ should still be open after TTL release — closing is done by a subsequent merge")
	}

	// Verify: the MR can be re-claimed by a subsequent patrol cycle.
	reclaimed, err := worldStore.ClaimMergeRequest("forge/crashtest", 3)
	if err != nil {
		t.Fatalf("re-ClaimMergeRequest: %v", err)
	}
	if reclaimed == nil {
		t.Fatal("expected MR to be re-claimable after TTL release, got nil")
	}
	if reclaimed.ID != mrID {
		t.Errorf("re-claimed MR ID = %q, want %q", reclaimed.ID, mrID)
	}
	if reclaimed.Attempts != 2 {
		t.Errorf("re-claimed MR attempts = %d, want 2 (incremented from 1)", reclaimed.Attempts)
	}

	// Verify: the pushed branch is still in origin — TTL release does NOT
	// delete the branch, so the next forge attempt can use it.
	logOut = runGitOutput(t, workingClone, "ls-remote", "origin", branch)
	if !strings.Contains(logOut, branch) {
		t.Errorf("branch %s should still exist in origin after TTL release", branch)
	}
}

// --- Test: Crash After Push with Exhausted Attempts ---
//
// Variant of the crash-after-push scenario where the MR has already consumed
// all allowed attempts. ReleaseStaleClaims should mark it as "failed" (not
// "ready"), preventing an infinite retry loop.

func TestCrashAfterPushExhaustedAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnvWithRepo(t)
	_ = gtHome

	worldStore, _ := openStores(t, "exhausttest")

	// Create writ and MR.
	writID, err := worldStore.CreateWrit("Exhausted attempts", "Too many retries", "test", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	mrID, err := worldStore.CreateMergeRequest(writID, "outpost/Bot/"+writID, 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}

	// Claim the MR (attempts → 1).
	claimed, err := worldStore.ClaimMergeRequest("forge/exhausttest", 3)
	if err != nil {
		t.Fatalf("ClaimMergeRequest: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected to claim MR")
	}

	// Artificially set attempts to maxAttempts (3) to simulate exhaustion.
	if _, err := worldStore.DB().Exec(
		`UPDATE merge_requests SET attempts = 3 WHERE id = ?`, mrID,
	); err != nil {
		t.Fatalf("set attempts: %v", err)
	}

	// Backdate claimed_at to make it stale.
	staleTime := time.Now().UTC().Add(-31 * time.Minute).Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`UPDATE merge_requests SET claimed_at = ? WHERE id = ?`, staleTime, mrID,
	); err != nil {
		t.Fatalf("backdate claimed_at: %v", err)
	}

	// ReleaseStaleClaims with maxAttempts=3 — exhausted MRs should go to
	// "failed" not "ready".
	released, err := worldStore.ReleaseStaleClaims(30*time.Minute, 3)
	if err != nil {
		t.Fatalf("ReleaseStaleClaims: %v", err)
	}
	if released != 0 {
		t.Errorf("ReleaseStaleClaims returned %d, want 0 (exhausted claims are not 'released')", released)
	}

	// Verify: MR is in failed phase, not ready.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.Phase != store.MRFailed {
		t.Errorf("MR phase = %q, want %q (exhausted stale claim)", mr.Phase, store.MRFailed)
	}

	// Verify: MR cannot be re-claimed (failed MRs are not claimable).
	reclaimed, err := worldStore.ClaimMergeRequest("forge/exhausttest", 3)
	if err != nil {
		t.Fatalf("re-ClaimMergeRequest: %v", err)
	}
	if reclaimed != nil {
		t.Error("exhausted MR should NOT be re-claimable, but ClaimMergeRequest returned non-nil")
	}
}

// --- Test: Tether Operations Work When Store DB Is Unavailable ---
//
// Exercises the failure mode documented in docs/failure-modes.md (lines 47-50):
// "If the database file is corrupted or locked, operations that require
// coordination state fail. Agents with tethered work continue executing."
//
// The tether is a directory of plain files under
// $SOL_HOME/{world}/outposts/{agent}/.tether/. It has no dependency on the
// SQLite store. This test proves that all tether operations (Write, Read,
// List, IsTethered, IsTetheredTo, ClearOne) work correctly even when the
// world store DB is corrupted and unusable.

func TestTetherOperationsWithCorruptStore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	const (
		world     = "corrupt"
		agentName = "Survivor"
		writID    = "sol-abc1234500000001"
		writID2   = "sol-abc1234500000002"
	)

	// First, create a valid world store and do some work so the DB file exists.
	worldStore, _ := openStores(t, world)
	if _, err := worldStore.CreateWrit("Pre-corruption task", "", "test", 2, nil); err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	worldStore.Close()

	// Write a tether BEFORE corruption — the agent is working on this writ.
	if err := tether.Write(world, agentName, writID, "outpost"); err != nil {
		t.Fatalf("tether.Write (pre-corruption): %v", err)
	}

	// === CORRUPT THE STORE ===
	// Overwrite the world DB with garbage bytes. This simulates disk corruption
	// that makes the SQLite file unreadable.
	dbPath := filepath.Join(solHome, ".store", world+".db")
	if err := os.WriteFile(dbPath, []byte("THIS IS NOT A VALID SQLITE DATABASE"), 0o644); err != nil {
		t.Fatalf("corrupt DB: %v", err)
	}
	// Also corrupt the WAL and SHM files if they exist.
	os.WriteFile(dbPath+"-wal", []byte("corrupted"), 0o644)
	os.WriteFile(dbPath+"-shm", []byte("corrupted"), 0o644)

	// Verify: the store IS actually broken — operations should fail.
	_, err := store.OpenWorld(world)
	if err == nil {
		t.Fatal("expected store.OpenWorld to fail on corrupt DB, but it succeeded")
	}

	// === TETHER OPERATIONS SHOULD STILL WORK ===
	// All of these are pure filesystem operations with no store dependency.

	// 1. Read: should return the pre-corruption tether.
	readID, err := tether.Read(world, agentName, "outpost")
	if err != nil {
		t.Fatalf("tether.Read with corrupt store: %v", err)
	}
	if readID != writID {
		t.Errorf("tether.Read = %q, want %q", readID, writID)
	}

	// 2. List: should return the tethered writ ID.
	ids, err := tether.List(world, agentName, "outpost")
	if err != nil {
		t.Fatalf("tether.List with corrupt store: %v", err)
	}
	if len(ids) != 1 || ids[0] != writID {
		t.Errorf("tether.List = %v, want [%s]", ids, writID)
	}

	// 3. IsTethered: should return true.
	if !tether.IsTethered(world, agentName, "outpost") {
		t.Error("tether.IsTethered should be true with corrupt store")
	}

	// 4. IsTetheredTo: should return true for the correct writ.
	if !tether.IsTetheredTo(world, agentName, writID, "outpost") {
		t.Error("tether.IsTetheredTo should be true for the tethered writ")
	}
	if tether.IsTetheredTo(world, agentName, "sol-nonexistent000000", "outpost") {
		t.Error("tether.IsTetheredTo should be false for a non-tethered writ")
	}

	// 5. Write: should be able to add a NEW tether while store is corrupt.
	if err := tether.Write(world, agentName, writID2, "outpost"); err != nil {
		t.Fatalf("tether.Write (during corruption): %v", err)
	}

	// 6. Verify the new tether is visible.
	ids, err = tether.List(world, agentName, "outpost")
	if err != nil {
		t.Fatalf("tether.List after second write: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("tether.List returned %d items, want 2", len(ids))
	}

	// 7. ClearOne: should be able to remove a specific tether.
	if err := tether.ClearOne(world, agentName, writID2, "outpost"); err != nil {
		t.Fatalf("tether.ClearOne with corrupt store: %v", err)
	}
	if tether.IsTetheredTo(world, agentName, writID2, "outpost") {
		t.Error("tether.IsTetheredTo should be false after ClearOne")
	}

	// 8. Original tether is still intact.
	if !tether.IsTetheredTo(world, agentName, writID, "outpost") {
		t.Error("original tether should still exist after ClearOne of different writ")
	}

	// 9. Clear: should be able to remove all tethers.
	if err := tether.Clear(world, agentName, "outpost"); err != nil {
		t.Fatalf("tether.Clear with corrupt store: %v", err)
	}
	if tether.IsTethered(world, agentName, "outpost") {
		t.Error("tether.IsTethered should be false after Clear")
	}
}

// --- Test helpers ---

// eventsContainField checks if any JSON line in the events log has the given
// key set to the given value.
func eventsContainField(content, key, value string) bool {
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			if v, ok := m[key]; ok && v == value {
				return true
			}
		}
	}
	return false
}
