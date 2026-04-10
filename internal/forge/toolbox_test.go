package forge

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// mockWritLanded seeds the mockCmdRunner so that isWritLandedOnTarget reports
// the writ as landed on origin/main. This unblocks deleteBranchIfContained
// in tests that exercise MarkMerged with a mock cmd runner — the default
// mock returns nil for git log, which would otherwise look like "no commit
// references writ" and trigger an extra escalation.
func mockWritLanded(m *mockCmdRunner, writID string) {
	m.SetResult(
		"git log refs/remotes/origin/main --grep="+writID+" -n 1 --format=%H",
		[]byte("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"),
		nil,
	)
}

func TestListReady(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: store.MRReady, BlockedBy: ""},
		{ID: "mr-00000002", Phase: store.MRReady, BlockedBy: "sol-blocker1"},
		{ID: "mr-00000003", Phase: store.MRReady, BlockedBy: ""},
		{ID: "mr-00000004", Phase: store.MRClaimed, BlockedBy: ""},
	}

	r := &Forge{
		world:       "ember",
		worldStore:  worldStore,
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
	}

	ready, err := r.ListReady()
	if err != nil {
		t.Fatalf("ListReady() error: %v", err)
	}
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready MRs, got %d", len(ready))
	}
	for _, mr := range ready {
		if mr.BlockedBy != "" {
			t.Errorf("ListReady returned blocked MR %s", mr.ID)
		}
	}
}

func TestListReadyIsPureRead(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: store.MRReady, WritID: "sol-aaa11111", BlockedBy: ""},
		{ID: "mr-00000002", Phase: store.MRReady, WritID: "sol-bbb22222", BlockedBy: ""},
	}

	// Even with caravan deps that would block, ListReady should NOT call BlockMergeRequest.
	sphereStore := newMockSphereStore()
	sphereStore.caravanBlockedMap = map[string]bool{
		"sol-aaa11111": true,
	}

	r := &Forge{
		world:       "ember",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
	}

	ready, err := r.ListReady()
	if err != nil {
		t.Fatalf("ListReady() error: %v", err)
	}

	// ListReady is a pure read — it returns all unblocked ready MRs without checking caravan deps.
	if len(ready) != 2 {
		t.Fatalf("expected 2 ready MRs, got %d", len(ready))
	}

	// Verify no BlockMergeRequest calls were made.
	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()
	if len(worldStore.blockCalls) != 0 {
		t.Errorf("ListReady should not call BlockMergeRequest, got %d calls", len(worldStore.blockCalls))
	}
}

func TestEnforceCaravanBlocks(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: store.MRReady, WritID: "sol-aaa11111", BlockedBy: ""},
		{ID: "mr-00000002", Phase: store.MRReady, WritID: "sol-bbb22222", BlockedBy: ""},
		{ID: "mr-00000003", Phase: store.MRReady, WritID: "sol-ccc33333", BlockedBy: ""},
	}

	sphereStore := newMockSphereStore()
	sphereStore.caravanBlockedMap = map[string]bool{
		"sol-aaa11111": true,
		"sol-ccc33333": true,
	}

	r := &Forge{
		world:       "ember",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
	}

	n, err := r.EnforceCaravanBlocks()
	if err != nil {
		t.Fatalf("EnforceCaravanBlocks() error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 MRs blocked, got %d", n)
	}

	// Verify the correct MRs were blocked.
	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()
	if len(worldStore.blockCalls) != 2 {
		t.Fatalf("expected 2 BlockMergeRequest calls, got %d", len(worldStore.blockCalls))
	}
	for _, call := range worldStore.blockCalls {
		if call.BlockerID != store.CaravanBlockedSentinel {
			t.Errorf("expected blocker %q, got %q", store.CaravanBlockedSentinel, call.BlockerID)
		}
	}

	// Verify mr-00000002 was not blocked.
	for _, mr := range worldStore.mrs {
		if mr.ID == "mr-00000002" && mr.BlockedBy != "" {
			t.Errorf("mr-00000002 should not be blocked, got BlockedBy=%q", mr.BlockedBy)
		}
	}
}

func TestEnforceCaravanBlocksSkipsAlreadyBlocked(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: store.MRReady, WritID: "sol-aaa11111", BlockedBy: "sol-existing-blocker"},
		{ID: "mr-00000002", Phase: store.MRReady, WritID: "sol-bbb22222", BlockedBy: store.CaravanBlockedSentinel},
		{ID: "mr-00000003", Phase: store.MRReady, WritID: "sol-ccc33333", BlockedBy: ""},
	}

	sphereStore := newMockSphereStore()
	sphereStore.caravanBlockedMap = map[string]bool{
		"sol-aaa11111": true,
		"sol-bbb22222": true,
		"sol-ccc33333": true,
	}

	r := &Forge{
		world:       "ember",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
	}

	n, err := r.EnforceCaravanBlocks()
	if err != nil {
		t.Fatalf("EnforceCaravanBlocks() error: %v", err)
	}
	// Only mr-00000003 should be newly blocked (mr-00000001 and mr-00000002 already blocked).
	if n != 1 {
		t.Errorf("expected 1 MR blocked, got %d", n)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()
	if len(worldStore.blockCalls) != 1 {
		t.Fatalf("expected 1 BlockMergeRequest call, got %d", len(worldStore.blockCalls))
	}
	if worldStore.blockCalls[0].MRID != "mr-00000003" {
		t.Errorf("expected mr-00000003 to be blocked, got %q", worldStore.blockCalls[0].MRID)
	}
}

func TestListBlocked(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: store.MRReady, BlockedBy: ""},
		{ID: "mr-00000002", Phase: store.MRReady, BlockedBy: "sol-blocker1"},
		{ID: "mr-00000003", Phase: store.MRReady, BlockedBy: "sol-blocker2"},
	}

	r := &Forge{
		world:      "ember",
		worldStore: worldStore,
		logger:   testLogger(),
	}

	blocked, err := r.ListBlocked()
	if err != nil {
		t.Fatalf("ListBlocked() error: %v", err)
	}
	if len(blocked) != 2 {
		t.Fatalf("expected 2 blocked MRs, got %d", len(blocked))
	}
}

func TestCreateResolutionTask(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	// Set up a mock git repo to satisfy rev-parse.
	repoDir := t.TempDir()
	run(t, "git", "init", repoDir)
	run(t, "git", "-C", repoDir, "commit", "--allow-empty", "-m", "init")

	worldStore := newMockWorldStore()
	worldStore.items["sol-original1"] = &store.Writ{
		ID:       "sol-original1",
		Title:    "Add feature X",
		Priority: 2,
	}
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-original1", Branch: "outpost/Toast/sol-original1", Phase: store.MRClaimed},
	}

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main" // tests run outside world config — set explicitly
	r := &Forge{
		world:      "ember",
		agentID:    "ember/forge",
		worktree:   repoDir,
		worldStore: worldStore,
		logger:     testLogger(),
		cfg:        forgeCfg,
	}

	mr := &store.MergeRequest{
		ID:         "mr-00000001",
		WritID: "sol-original1",
		Branch:     "outpost/Toast/sol-original1",
		Phase:      store.MRClaimed,
	}

	taskID, err := r.CreateResolutionTask(mr)
	if err != nil {
		t.Fatalf("CreateResolutionTask() error: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected non-empty task ID")
	}

	// Verify the resolution task was created.
	item := worldStore.items[taskID]
	if item == nil {
		t.Fatal("resolution task not found in store")
	}
	if !strings.Contains(item.Title, "Resolve merge conflicts") {
		t.Errorf("task title = %q, want to contain 'Resolve merge conflicts'", item.Title)
	}
	if item.Priority != 1 {
		t.Errorf("task priority = %d, want 1 (boosted from 2)", item.Priority)
	}
	if item.ParentID != "sol-original1" {
		t.Errorf("task parent_id = %q, want %q", item.ParentID, "sol-original1")
	}
	if !item.HasLabel("conflict-resolution") {
		t.Error("task missing 'conflict-resolution' label")
	}
	if !item.HasLabel("source-mr:mr-00000001") {
		t.Error("task missing 'source-mr:mr-00000001' label")
	}

	// Verify the description includes explicit rebase instructions.
	desc := item.Description
	for _, want := range []string{
		"WARNING: Do NOT just verify existing code and resolve",
		"git fetch origin",
		"git rebase origin/main",
		"make build && make test",
		"git merge-base origin/main HEAD",
		"git push --force-with-lease origin outpost/Toast/sol-original1",
		"ONLY AFTER the force-push succeeds",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("description missing %q", want)
		}
	}

	// Verify the MR is blocked.
	worldStore.mu.Lock()
	blockedMR := worldStore.mrs[0]
	worldStore.mu.Unlock()
	if blockedMR.BlockedBy != taskID {
		t.Errorf("MR blocked_by = %q, want %q", blockedMR.BlockedBy, taskID)
	}
}

func TestCheckUnblocked(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: store.MRReady, BlockedBy: "sol-resolved1"},
		{ID: "mr-00000002", Phase: store.MRReady, BlockedBy: "sol-pending1"},
	}
	worldStore.items["sol-resolved1"] = &store.Writ{ID: "sol-resolved1", Status: store.WritClosed}
	worldStore.items["sol-pending1"] = &store.Writ{ID: "sol-pending1", Status: store.WritOpen}

	r := &Forge{
		world:      "ember",
		worldStore: worldStore,
		logger:   testLogger(),
	}

	unblocked, err := r.CheckUnblocked()
	if err != nil {
		t.Fatalf("CheckUnblocked() error: %v", err)
	}
	if len(unblocked) != 1 {
		t.Fatalf("expected 1 unblocked MR, got %d", len(unblocked))
	}
	if unblocked[0] != "mr-00000001" {
		t.Errorf("unblocked MR = %q, want %q", unblocked[0], "mr-00000001")
	}

	// Verify the unblocked MR has its BlockedBy cleared.
	worldStore.mu.Lock()
	mr := worldStore.mrs[0]
	worldStore.mu.Unlock()
	if mr.BlockedBy != "" {
		t.Errorf("MR blocked_by after unblock = %q, want empty", mr.BlockedBy)
	}

	// Verify the still-blocked MR is unchanged.
	worldStore.mu.Lock()
	mr2 := worldStore.mrs[1]
	worldStore.mu.Unlock()
	if mr2.BlockedBy != "sol-pending1" {
		t.Errorf("MR blocked_by = %q, want %q", mr2.BlockedBy, "sol-pending1")
	}
}

func TestMarkFailedReopensWrit(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID:       "sol-aaa11111",
		Title:    "Test feature",
		Status:   store.WritDone,
		Assignee: "Toast",
	}

	sphereStore := newMockSphereStore()

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         DefaultConfig(),
	}

	if err := r.MarkFailed("mr-00000001"); err != nil {
		t.Fatalf("MarkFailed() error: %v", err)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// Verify MR phase set to failed.
	if phase, ok := worldStore.phaseUpdates["mr-00000001"]; !ok || phase != store.MRFailed {
		t.Errorf("MR phase = %q, want 'failed'", phase)
	}

	// Verify writ reopened.
	item := worldStore.items["sol-aaa11111"]
	if item.Status != store.WritOpen {
		t.Errorf("writ status = %q, want 'open'", item.Status)
	}
	if item.Assignee != "" {
		t.Errorf("writ assignee = %q, want empty (cleared)", item.Assignee)
	}

	// Verify escalation created with source_ref.
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(sphereStore.escalations))
	}
	esc := sphereStore.escalations[0]
	if esc.severity != "high" {
		t.Errorf("escalation severity = %q, want 'high'", esc.severity)
	}
	if esc.source != "ember/forge" {
		t.Errorf("escalation source = %q, want 'ember/forge'", esc.source)
	}
	if !strings.Contains(esc.description, "sol-aaa11111") {
		t.Errorf("escalation description should mention writ ID, got: %s", esc.description)
	}
	if esc.sourceRef != "mr:mr-00000001" {
		t.Errorf("escalation source_ref = %q, want 'mr:mr-00000001'", esc.sourceRef)
	}

	// Verify agent state reset to idle.
	if len(sphereStore.agentStateUpdates) != 1 {
		t.Fatalf("expected 1 agent state update, got %d", len(sphereStore.agentStateUpdates))
	}
	update := sphereStore.agentStateUpdates[0]
	if update.id != "ember/Toast" {
		t.Errorf("agent state update id = %q, want 'ember/Toast'", update.id)
	}
	if update.state != store.AgentIdle {
		t.Errorf("agent state update state = %q, want 'idle'", update.state)
	}
	if update.activeWrit != "" {
		t.Errorf("agent state update activeWrit = %q, want empty", update.activeWrit)
	}
}

func TestMarkMergedClosesWrit(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "Test", Status: store.WritDone}

	// Create a temp dir for git operations.
	dir := t.TempDir()
	run(t, "git", "init", dir)

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:      "ember",
		agentID:  "ember/forge",
		worktree: dir,
		worldStore: worldStore,
		logger:   slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		cfg:      forgeCfg,
		cmd:      newMockCmdRunner(),
	}

	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// Verify MR phase.
	if phase, ok := worldStore.phaseUpdates["mr-00000001"]; !ok || phase != store.MRMerged {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}

	// Verify writ closed.
	if worldStore.items["sol-aaa11111"].Status != store.WritClosed {
		t.Errorf("writ status = %q, want 'closed'", worldStore.items["sol-aaa11111"].Status)
	}
}

func TestMarkMergedSupersedesFailedSiblings(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-failed1", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRFailed},
		{ID: "mr-failed2", WritID: "sol-aaa11111", Branch: "outpost/Blaze/sol-aaa11111", Phase: store.MRFailed},
		{ID: "mr-merged1", WritID: "sol-aaa11111", Branch: "outpost/Nova/sol-aaa11111", Phase: store.MRClaimed},
		{ID: "mr-other1", WritID: "sol-bbb22222", Branch: "outpost/Toast/sol-bbb22222", Phase: store.MRFailed}, // different writ
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "Test", Status: store.WritDone}
	worldStore.items["sol-bbb22222"] = &store.Writ{ID: "sol-bbb22222", Title: "Other", Status: store.WritDone}

	sphereStore := newMockSphereStore()
	// Pre-create escalations for the failed MRs with source_ref.
	sphereStore.CreateEscalation("high", "ember/forge",
		"Merge failed for MR mr-failed1 (branch outpost/Toast/sol-aaa11111, writ sol-aaa11111). Writ reopened for re-dispatch.",
		"mr:mr-failed1")
	sphereStore.CreateEscalation("high", "ember/forge",
		"Merge failed for MR mr-failed2 (branch outpost/Blaze/sol-aaa11111, writ sol-aaa11111). Writ reopened for re-dispatch.",
		"mr:mr-failed2")
	// Escalation for different writ — should NOT be resolved.
	sphereStore.CreateEscalation("high", "ember/forge",
		"Merge failed for MR mr-other1 (branch outpost/Toast/sol-bbb22222, writ sol-bbb22222). Writ reopened for re-dispatch.",
		"mr:mr-other1")

	dir := t.TempDir()
	run(t, "git", "init", dir)

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	mockCmd := newMockCmdRunner()
	mockWritLanded(mockCmd, "sol-aaa11111")
	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    dir,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		cfg:         forgeCfg,
		cmd:         mockCmd,
	}

	if err := r.MarkMerged("mr-merged1"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// Verify merged MR is merged.
	if phase := worldStore.phaseUpdates["mr-merged1"]; phase != store.MRMerged {
		t.Errorf("merged MR phase = %q, want 'merged'", phase)
	}

	// Verify failed sibling MRs are superseded.
	if phase := worldStore.phaseUpdates["mr-failed1"]; phase != store.MRSuperseded {
		t.Errorf("failed MR 1 phase = %q, want 'superseded'", phase)
	}
	if phase := worldStore.phaseUpdates["mr-failed2"]; phase != store.MRSuperseded {
		t.Errorf("failed MR 2 phase = %q, want 'superseded'", phase)
	}

	// Verify MR from different writ is NOT superseded.
	if _, ok := worldStore.phaseUpdates["mr-other1"]; ok {
		t.Error("MR for different writ should not be touched")
	}

	// Verify escalations for failed MRs are resolved (using source_ref matching).
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	for _, esc := range sphereStore.escalations {
		if esc.sourceRef == "mr:mr-failed1" && esc.status != "resolved" {
			t.Errorf("escalation for mr-failed1 status = %q, want 'resolved'", esc.status)
		}
		if esc.sourceRef == "mr:mr-failed2" && esc.status != "resolved" {
			t.Errorf("escalation for mr-failed2 status = %q, want 'resolved'", esc.status)
		}
		if esc.sourceRef == "mr:mr-other1" && esc.status == "resolved" {
			t.Error("escalation for different writ should NOT be resolved")
		}
	}
}

// --- Partial failure tests ---

func TestMarkMergedCloseWritFailureReturnsError(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "Test", Status: store.WritDone}
	worldStore.closeWritErr = fmt.Errorf("database locked")

	sphereStore := newMockSphereStore()

	dir := t.TempDir()
	run(t, "git", "init", dir)

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    dir,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         DefaultConfig(),
	}

	// With crash-safe ordering, CloseWrit is attempted FIRST.
	// If it fails, MarkMerged returns an error — nothing was changed.
	err := r.MarkMerged("mr-00000001")
	if err == nil {
		t.Fatal("MarkMerged() should return error when CloseWrit fails")
	}
	if !strings.Contains(err.Error(), "database locked") {
		t.Errorf("error should mention root cause, got: %v", err)
	}

	// Verify MR phase was NOT updated (crash-safe: nothing happened).
	worldStore.mu.Lock()
	if _, ok := worldStore.phaseUpdates["mr-00000001"]; ok {
		t.Error("MR phase should not be updated when CloseWrit fails")
	}
	worldStore.mu.Unlock()

	// No escalation needed — operation was cleanly aborted.
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 0 {
		t.Errorf("expected 0 escalations, got %d", len(sphereStore.escalations))
	}
}

func TestMarkMergedPhaseUpdateFailureCreatesEscalation(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "Test", Status: store.WritDone}
	worldStore.updatePhaseErr = fmt.Errorf("database locked")

	sphereStore := newMockSphereStore()

	dir := t.TempDir()
	run(t, "git", "init", dir)

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	mockCmd := newMockCmdRunner()
	mockWritLanded(mockCmd, "sol-aaa11111")
	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    dir,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         forgeCfg,
		cmd:         mockCmd,
	}

	// With crash-safe ordering, CloseWrit succeeds first, then
	// UpdateMergeRequestPhase fails. MarkMerged still returns nil
	// because the critical state change (writ closed) succeeded.
	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	// Verify writ was closed (the critical operation succeeded).
	worldStore.mu.Lock()
	if worldStore.items["sol-aaa11111"].Status != store.WritClosed {
		t.Errorf("writ status = %q, want 'closed'", worldStore.items["sol-aaa11111"].Status)
	}
	worldStore.mu.Unlock()

	// Verify escalation was created for the phase update failure.
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(sphereStore.escalations))
	}
	esc := sphereStore.escalations[0]
	if esc.severity != "low" {
		t.Errorf("escalation severity = %q, want 'low'", esc.severity)
	}
	if esc.source != "ember/forge" {
		t.Errorf("escalation source = %q, want 'ember/forge'", esc.source)
	}
	if !strings.Contains(esc.description, "database locked") {
		t.Errorf("escalation description should mention error, got: %s", esc.description)
	}
	if esc.sourceRef != "mr:mr-00000001" {
		t.Errorf("escalation source_ref = %q, want 'mr:mr-00000001'", esc.sourceRef)
	}
}

func TestMarkFailedUpdateWritFailureCreatesEscalation(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID:       "sol-aaa11111",
		Title:    "Test feature",
		Status:   store.WritDone,
		Assignee: "Toast",
	}
	worldStore.updateWritErr = fmt.Errorf("database locked")

	sphereStore := newMockSphereStore()

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         DefaultConfig(),
	}

	if err := r.MarkFailed("mr-00000001"); err != nil {
		t.Fatalf("MarkFailed() error: %v", err)
	}

	// Verify MR phase set to failed (crash-safe ordering: writ reopen attempted
	// first but failed, MR phase update still proceeds).
	worldStore.mu.Lock()
	if phase := worldStore.phaseUpdates["mr-00000001"]; phase != store.MRFailed {
		t.Errorf("MR phase = %q, want 'failed'", phase)
	}
	worldStore.mu.Unlock()

	// Verify escalation was created — it should mention the reopen failure.
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(sphereStore.escalations))
	}
	esc := sphereStore.escalations[0]
	if esc.severity != "high" {
		t.Errorf("escalation severity = %q, want 'high'", esc.severity)
	}
	if !strings.Contains(esc.description, "database locked") {
		t.Errorf("escalation description should mention reopen error, got: %s", esc.description)
	}
	if esc.sourceRef != "mr:mr-00000001" {
		t.Errorf("escalation source_ref = %q, want 'mr:mr-00000001'", esc.sourceRef)
	}
}

func TestMarkFailedCrashSafetyOrdering(t *testing.T) {
	// This test verifies that MarkFailed reopens the writ BEFORE marking
	// the MR as failed. We simulate a "crash" by injecting a failure on
	// UpdateMergeRequestPhase and verify the writ was already reopened.
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID:       "sol-aaa11111",
		Title:    "Test feature",
		Status:   store.WritDone,
		Assignee: "Toast",
	}
	// Inject failure on MR phase update — simulates crash between the two operations.
	worldStore.updatePhaseErr = fmt.Errorf("simulated crash")

	sphereStore := newMockSphereStore()

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         DefaultConfig(),
	}

	err := r.MarkFailed("mr-00000001")
	// Should return error because MR phase update failed.
	if err == nil {
		t.Fatal("MarkFailed() should return error when UpdateMergeRequestPhase fails")
	}
	if !strings.Contains(err.Error(), "simulated crash") {
		t.Errorf("error should mention root cause, got: %v", err)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// CRITICAL: Writ should already be reopened even though MR phase update
	// failed. This proves crash safety: writ is reopened BEFORE MR is marked
	// failed. If the order were reversed, the writ would still be "done" with
	// assignee "Toast".
	item := worldStore.items["sol-aaa11111"]
	if item.Status != store.WritOpen {
		t.Errorf("writ status = %q, want 'open' (should be reopened before MR phase update)", item.Status)
	}
	if item.Assignee != "" {
		t.Errorf("writ assignee = %q, want empty (should be cleared before MR phase update)", item.Assignee)
	}
}

func TestResolveEscalationsForMRUsesSourceRef(t *testing.T) {
	sphereStore := newMockSphereStore()
	// Create escalations: one with source_ref matching, one with different source_ref.
	sphereStore.CreateEscalation("high", "ember/forge", "Failed MR mr-target1", "mr:mr-target1")
	sphereStore.CreateEscalation("high", "ember/forge", "Failed MR mr-other1", "mr:mr-other1")
	// An escalation that mentions mr-target1 in description but has a different source_ref.
	sphereStore.CreateEscalation("low", "ember/forge", "Contains mr-target1 in text", "mr:mr-unrelated")

	worldStore := newMockWorldStore()

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
	}

	r.resolveEscalationsForMR("mr-target1")

	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()

	for _, esc := range sphereStore.escalations {
		if esc.sourceRef == "mr:mr-target1" && esc.status != "resolved" {
			t.Errorf("escalation with source_ref 'mr:mr-target1' should be resolved, status = %q", esc.status)
		}
		if esc.sourceRef == "mr:mr-other1" && esc.status == "resolved" {
			t.Error("escalation for different MR should NOT be resolved")
		}
		if esc.sourceRef == "mr:mr-unrelated" && esc.status == "resolved" {
			t.Error("escalation with different source_ref (even if description matches) should NOT be resolved")
		}
	}
}

func TestMarkMergedResolvesWritLinkedEscalations(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "Test", Status: store.WritDone}

	sphereStore := newMockSphereStore()
	// Create writ-linked escalation.
	sphereStore.CreateEscalation("high", "ember/Toast", "Agent stuck on writ", "writ:sol-aaa11111")
	// Create MR-linked escalation (should NOT be resolved by writ auto-resolve).
	sphereStore.CreateEscalation("high", "ember/forge", "MR merge failed", "mr:mr-00000001")
	// Create escalation for different writ — should NOT be resolved.
	sphereStore.CreateEscalation("low", "ember/Toast", "Other writ issue", "writ:sol-bbb22222")

	dir := t.TempDir()
	run(t, "git", "init", dir)

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    dir,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         forgeCfg,
		cmd:         newMockCmdRunner(),
	}

	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()

	for _, esc := range sphereStore.escalations {
		if esc.sourceRef == "writ:sol-aaa11111" && esc.status != "resolved" {
			t.Errorf("writ-linked escalation status = %q, want 'resolved'", esc.status)
		}
		if esc.sourceRef == "mr:mr-00000001" && esc.status == "resolved" {
			t.Error("MR-linked escalation should NOT be resolved by writ auto-resolve")
		}
		if esc.sourceRef == "writ:sol-bbb22222" && esc.status == "resolved" {
			t.Error("escalation for different writ should NOT be resolved")
		}
	}
}

func TestMarkMergedMultipleWritLinkedEscalations(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "Test", Status: store.WritDone}

	sphereStore := newMockSphereStore()
	// Create multiple writ-linked escalations.
	for i := 0; i < 3; i++ {
		sphereStore.CreateEscalation("high", "ember/Toast",
			fmt.Sprintf("Escalation %d for writ", i), "writ:sol-aaa11111")
	}

	dir := t.TempDir()
	run(t, "git", "init", dir)

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    dir,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         forgeCfg,
		cmd:         newMockCmdRunner(),
	}

	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()

	for _, esc := range sphereStore.escalations {
		if esc.sourceRef == "writ:sol-aaa11111" && esc.status != "resolved" {
			t.Errorf("writ-linked escalation %q status = %q, want 'resolved'", esc.id, esc.status)
		}
	}
}

// --- RecoverOrphanedMerged tests ---

// TestRecoverOrphanedMergedFixesClaimedClosedWrit verifies that a claimed MR
// whose writ is closed (the partial MarkMerged failure state) is recovered to
// "merged" phase without dispatching a new session.
func TestRecoverOrphanedMergedFixesClaimedClosedWrit(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID:     "sol-aaa11111",
		Title:  "Test feature",
		Status: store.WritClosed, // writ already closed — partial MarkMerged failure
	}

	sphereStore := newMockSphereStore()
	// Pre-create an escalation for this MR (created by the partial failure).
	sphereStore.CreateEscalation("low", "ember/forge",
		"MR mr-00000001 not marked merged after writ sol-aaa11111 closed: database locked. The next forge patrol cycle will recover this automatically.",
		"mr:mr-00000001")

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
	}

	n, err := r.RecoverOrphanedMerged()
	if err != nil {
		t.Fatalf("RecoverOrphanedMerged() error: %v", err)
	}
	if n != 1 {
		t.Errorf("recovered = %d, want 1", n)
	}

	// Verify MR phase updated to merged.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-00000001"]
	worldStore.mu.Unlock()
	if phase != store.MRMerged {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}

	// Verify the escalation was resolved.
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	for _, esc := range sphereStore.escalations {
		if esc.sourceRef == "mr:mr-00000001" && esc.status != "resolved" {
			t.Errorf("escalation for mr-00000001 status = %q, want 'resolved'", esc.status)
		}
	}
}

// TestRecoverOrphanedMergedSkipsNonClosedWrit verifies that a claimed MR
// whose writ is NOT closed is left untouched.
func TestRecoverOrphanedMergedSkipsNonClosedWrit(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		// Claimed MR with writ in "working" — normal in-flight merge, leave it alone.
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
		// Claimed MR with writ in "done" — also leave it alone.
		{ID: "mr-00000002", WritID: "sol-bbb22222", Branch: "outpost/Nova/sol-bbb22222", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Status: store.WritWorking}
	worldStore.items["sol-bbb22222"] = &store.Writ{ID: "sol-bbb22222", Status: store.WritDone}

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
	}

	n, err := r.RecoverOrphanedMerged()
	if err != nil {
		t.Fatalf("RecoverOrphanedMerged() error: %v", err)
	}
	if n != 0 {
		t.Errorf("recovered = %d, want 0 (no orphaned MRs)", n)
	}

	// Verify no phase updates were made.
	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()
	if len(worldStore.phaseUpdates) != 0 {
		t.Errorf("expected 0 phase updates, got %d: %v", len(worldStore.phaseUpdates), worldStore.phaseUpdates)
	}
}

// TestRecoverOrphanedMergedMultiple verifies that multiple orphaned MRs are
// all recovered in a single call.
func TestRecoverOrphanedMergedMultiple(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Phase: store.MRClaimed},
		{ID: "mr-00000002", WritID: "sol-bbb22222", Phase: store.MRClaimed},
		{ID: "mr-00000003", WritID: "sol-ccc33333", Phase: store.MRReady},   // ready — leave alone
		{ID: "mr-00000004", WritID: "sol-ddd44444", Phase: store.MRClaimed}, // claimed but writ not closed
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Status: store.WritClosed}
	worldStore.items["sol-bbb22222"] = &store.Writ{ID: "sol-bbb22222", Status: store.WritClosed}
	worldStore.items["sol-ccc33333"] = &store.Writ{ID: "sol-ccc33333", Status: store.WritClosed}
	worldStore.items["sol-ddd44444"] = &store.Writ{ID: "sol-ddd44444", Status: store.WritWorking}

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
	}

	n, err := r.RecoverOrphanedMerged()
	if err != nil {
		t.Fatalf("RecoverOrphanedMerged() error: %v", err)
	}
	if n != 2 {
		t.Errorf("recovered = %d, want 2", n)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// Both orphaned claimed MRs should be merged.
	if phase := worldStore.phaseUpdates["mr-00000001"]; phase != store.MRMerged {
		t.Errorf("mr-00000001 phase = %q, want 'merged'", phase)
	}
	if phase := worldStore.phaseUpdates["mr-00000002"]; phase != store.MRMerged {
		t.Errorf("mr-00000002 phase = %q, want 'merged'", phase)
	}

	// Ready MR and claimed-but-non-closed MR should not be touched.
	if _, ok := worldStore.phaseUpdates["mr-00000003"]; ok {
		t.Error("mr-00000003 (ready) should not have phase updated")
	}
	if _, ok := worldStore.phaseUpdates["mr-00000004"]; ok {
		t.Error("mr-00000004 (claimed, non-closed writ) should not have phase updated")
	}
}

// TestRecoverOrphanedMergedNoOrphans verifies that when no orphaned MRs exist,
// the function returns 0 and makes no changes.
func TestRecoverOrphanedMergedNoOrphans(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Phase: store.MRReady},
		{ID: "mr-00000002", WritID: "sol-bbb22222", Phase: store.MRMerged},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Status: store.WritOpen}
	worldStore.items["sol-bbb22222"] = &store.Writ{ID: "sol-bbb22222", Status: store.WritClosed}

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
	}

	n, err := r.RecoverOrphanedMerged()
	if err != nil {
		t.Fatalf("RecoverOrphanedMerged() error: %v", err)
	}
	if n != 0 {
		t.Errorf("recovered = %d, want 0", n)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()
	if len(worldStore.phaseUpdates) != 0 {
		t.Errorf("expected 0 phase updates, got %d", len(worldStore.phaseUpdates))
	}
}

// --- Branch-cleanup containment tests ---

// setupContainmentRepo creates a real bare origin and a working clone where:
//   - main branch has M0 and M1 ("M1 (sol-merged0001)" — tagged with writID
//     in the same format the forge merge instructions use; see injection.go)
//   - mergedBranch points at M1 — its writID IS present in main's history,
//     so isWritLandedOnTarget reports landed and the branch is safe to delete
//   - unmergedBranch points at an off-main commit U1 ("U1 unmerged work") —
//     no commit on main references its writID, so the writ-grep refuses delete
//
// Returns the working clone path, used as the forge worktree for tests.
func setupContainmentRepo(t *testing.T) (worktree, mergedBranch, unmergedBranch string) {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(dir, "origin.git")
	run(t, "git", "init", "--bare", bare)

	work := filepath.Join(dir, "work")
	run(t, "git", "clone", bare, work)
	run(t, "git", "-C", work, "config", "user.email", "t@t.com")
	run(t, "git", "-C", work, "config", "user.name", "Test")

	mergedBranch = "outpost/Toast/sol-merged0001"
	unmergedBranch = "outpost/Toast/sol-unmerged01"

	// M0
	os.WriteFile(filepath.Join(work, "README"), []byte("init\n"), 0o644)
	run(t, "git", "-C", work, "add", ".")
	run(t, "git", "-C", work, "commit", "-m", "M0")
	// M1 — tag with the merged writ ID in the format injection.go uses.
	os.WriteFile(filepath.Join(work, "main.txt"), []byte("main work\n"), 0o644)
	run(t, "git", "-C", work, "add", ".")
	run(t, "git", "-C", work, "commit", "-m", "M1 (sol-merged0001)")
	run(t, "git", "-C", work, "push", "origin", "HEAD:main")

	// Merged branch points at the same commit as main.
	run(t, "git", "-C", work, "push", "origin", "HEAD:refs/heads/"+mergedBranch)

	// Unmerged branch: a sibling commit off M0 that is NOT in main.
	m0SHA := run(t, "git", "-C", work, "rev-parse", "HEAD~1")
	run(t, "git", "-C", work, "checkout", "--detach", m0SHA)
	os.WriteFile(filepath.Join(work, "feature.txt"), []byte("unmerged feature\n"), 0o644)
	run(t, "git", "-C", work, "add", ".")
	run(t, "git", "-C", work, "commit", "-m", "U1 unmerged work")
	run(t, "git", "-C", work, "push", "origin", "HEAD:refs/heads/"+unmergedBranch)
	// Restore HEAD to main tip for the worktree.
	run(t, "git", "-C", work, "checkout", "main")
	run(t, "git", "-C", work, "fetch", "origin")

	return work, mergedBranch, unmergedBranch
}

// TestMarkMergedRefusesDeleteOfUnmergedBranch verifies that MarkMerged does
// NOT delete a remote branch when no commit on the target branch references
// the writ ID, and that a high-severity escalation is created.
func TestMarkMergedRefusesDeleteOfUnmergedBranch(t *testing.T) {
	work, _, unmerged := setupContainmentRepo(t)

	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-unmerged", WritID: "sol-unmerged01", Branch: unmerged, Phase: store.MRClaimed},
	}
	worldStore.items["sol-unmerged01"] = &store.Writ{
		ID: "sol-unmerged01", Title: "Unmerged work", Status: store.WritDone,
	}

	sphereStore := newMockSphereStore()

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    work,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         forgeCfg,
		cmd:         &realCmdRunner{},
	}

	if err := r.MarkMerged("mr-unmerged"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	// Branch must still exist on origin: writ-grep refused the delete.
	if err := exec.Command("git", "-C", work, "rev-parse", "--verify", "--quiet",
		"refs/remotes/origin/"+unmerged).Run(); err != nil {
		t.Errorf("unmerged remote branch was deleted despite writ-grep check: %v", err)
	}

	// A high-severity escalation must have been created.
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	var found bool
	for _, esc := range sphereStore.escalations {
		if esc.sourceRef == "mr:mr-unmerged" && esc.severity == "high" &&
			strings.Contains(esc.description, "Refusing to delete branch") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected high-severity 'Refusing to delete branch' escalation for mr-unmerged, got %d escalations", len(sphereStore.escalations))
		for _, esc := range sphereStore.escalations {
			t.Logf("  esc: severity=%s sourceRef=%s desc=%s", esc.severity, esc.sourceRef, esc.description)
		}
	}
}

// TestMarkMergedDeletesContainedBranch verifies the happy path: when a commit
// on target references the writ ID, MarkMerged deletes the remote ref and
// does NOT create a writ-grep escalation.
func TestMarkMergedDeletesContainedBranch(t *testing.T) {
	work, merged, _ := setupContainmentRepo(t)

	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-merged", WritID: "sol-merged0001", Branch: merged, Phase: store.MRClaimed},
	}
	worldStore.items["sol-merged0001"] = &store.Writ{
		ID: "sol-merged0001", Title: "Merged work", Status: store.WritDone,
	}

	sphereStore := newMockSphereStore()

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    work,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         forgeCfg,
		cmd:         &realCmdRunner{},
	}

	if err := r.MarkMerged("mr-merged"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	// Refresh refs and check the remote branch is gone.
	run(t, "git", "-C", work, "fetch", "origin", "--prune")
	if err := exec.Command("git", "-C", work, "rev-parse", "--verify", "--quiet",
		"refs/remotes/origin/"+merged).Run(); err == nil {
		t.Errorf("merged remote branch should have been deleted")
	}

	// No 'Refusing to delete' escalation expected.
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	for _, esc := range sphereStore.escalations {
		if strings.Contains(esc.description, "Refusing to delete branch") {
			t.Errorf("unexpected writ-grep escalation: %s", esc.description)
		}
	}
}

// TestActOnResultRejectsFalseNoOpClaim verifies that the no_op merged
// pre-check in actOnResult rejects a no-op claim whose branch is not
// contained in target — the writ must NOT be closed.
func TestActOnResultRejectsFalseNoOpClaim(t *testing.T) {
	state, worldStore, _ := setupOrchestratorTest(t)
	defer state.fl.Close()

	// Replace the orchestrator's worktree+cmd with a real git repo so the
	// no-op pre-check (isBranchAncestorOfTarget) can run for real. The mock
	// cmdRunner returns nil/nil for merge-base which would falsely report
	// success.
	work, _, unmerged := setupContainmentRepo(t)
	state.forge.worktree = work
	state.forge.cmd = &realCmdRunner{}
	state.cmd = &realCmdRunner{}

	mr := &store.MergeRequest{
		ID:     "mr-falsenoop",
		WritID: "sol-unmerged01",
		Branch: unmerged,
	}
	worldStore.mrs = []store.MergeRequest{*mr}
	worldStore.items["sol-unmerged01"] = &store.Writ{
		ID: "sol-unmerged01", Title: "Unmerged work", Status: store.WritDone,
	}

	result := &ForgeResult{
		Result:  "merged",
		Summary: "false no-op claim",
		NoOp:    true,
	}

	state.actOnResult(context.Background(), mr, result, 1)

	// Writ must NOT be closed — pre-check rejected the claim before
	// MarkMerged could mutate state.
	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()
	if writ := worldStore.items["sol-unmerged01"]; writ.Status == store.WritClosed {
		t.Error("writ should not be closed when no-op claim is rejected")
	}
	// MR phase should be 'failed' (precheck called MarkFailed).
	if phase := worldStore.phaseUpdates["mr-falsenoop"]; phase != store.MRFailed {
		t.Errorf("MR phase = %q, want 'failed'", phase)
	}
	// Remote branch must still exist.
	if err := exec.Command("git", "-C", work, "rev-parse", "--verify", "--quiet",
		"refs/remotes/origin/"+unmerged).Run(); err != nil {
		t.Errorf("unmerged remote branch should still exist after rejected no-op: %v", err)
	}
}

// TestDeleteBranchIfContainedRemoteBranchMissing verifies the benign no-op
// path: when the remote branch ref does not exist (already deleted, never
// pushed), deleteBranchIfContained does not create an escalation and just
// best-effort cleans the local ref.
func TestDeleteBranchIfContainedRemoteBranchMissing(t *testing.T) {
	work, _, _ := setupContainmentRepo(t)
	missingBranch := "outpost/Toast/sol-missing001"

	sphereStore := newMockSphereStore()
	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    work,
		worldStore:  newMockWorldStore(),
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         forgeCfg,
		cmd:         &realCmdRunner{},
	}

	r.deleteBranchIfContained("mr-missing", missingBranch, "sol-missing001", "mr:mr-missing")

	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	if len(sphereStore.escalations) != 0 {
		t.Errorf("expected no escalations for missing remote branch, got %d", len(sphereStore.escalations))
		for _, esc := range sphereStore.escalations {
			t.Logf("  esc: %s", esc.description)
		}
	}
}

// TestDeleteBranchAfterRealSquashMerge is the canonical regression for the
// bug this writ fixes: the previous ancestor-based containment check fired
// on every successful squash merge because squash merges produce a new
// commit on main whose parent is the previous main tip — the source branch
// tip is never reachable from that squash commit.
//
// This test runs a REAL `git merge --squash` in a temp repo, commits with
// the writ-id tag injection.go produces, pushes, and then asserts that
// deleteBranchIfContained successfully removes the source branch (writ-grep
// finds the writ ID on main even though merge-base --is-ancestor would
// return false). This is the coverage gap the existing mock-based tests
// failed to catch.
func TestDeleteBranchAfterRealSquashMerge(t *testing.T) {
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
	run(t, "git", "-C", work, "commit", "-m", "initial")
	run(t, "git", "-C", work, "push", "origin", "HEAD:main")

	// Create a feature branch with a real change and push it. The forge
	// would normally cast this as outpost/<Agent>/sol-xxx.
	const writID = "sol-realsquash01"
	branch := "outpost/Toast/" + writID
	run(t, "git", "-C", work, "checkout", "-b", branch)
	os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature work\n"), 0o644)
	run(t, "git", "-C", work, "add", ".")
	run(t, "git", "-C", work, "commit", "-m", "feature commit on branch")
	run(t, "git", "-C", work, "push", "origin", branch)

	// Switch to main and perform the same `git merge --squash` the forge
	// runs (see internal/forge/injection.go). The result is a new commit
	// on main whose parent is main's previous tip — the branch tip is
	// NOT an ancestor of this commit, which is what broke the old check.
	run(t, "git", "-C", work, "checkout", "main")
	run(t, "git", "-C", work, "merge", "--squash", branch)
	run(t, "git", "-C", work, "commit", "-m", "Feature work ("+writID+")")
	run(t, "git", "-C", work, "push", "origin", "main")
	run(t, "git", "-C", work, "fetch", "origin")

	// Sanity: confirm the squash created a fresh commit and that
	// merge-base --is-ancestor would FAIL (this is the bug condition).
	if err := exec.Command("git", "-C", work, "merge-base", "--is-ancestor",
		"refs/remotes/origin/"+branch, "refs/remotes/origin/main").Run(); err == nil {
		t.Fatal("test setup invalid: branch tip is unexpectedly an ancestor of main; squash should have produced a divergent commit")
	}

	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-realsquash", WritID: writID, Branch: branch, Phase: store.MRClaimed},
	}
	worldStore.items[writID] = &store.Writ{
		ID: writID, Title: "Feature work", Status: store.WritDone,
	}
	sphereStore := newMockSphereStore()

	forgeCfg := DefaultConfig()
	forgeCfg.TargetBranch = "main"
	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    work,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         forgeCfg,
		cmd:         &realCmdRunner{},
	}

	r.deleteBranchIfContained("mr-realsquash", branch, writID, "mr:mr-realsquash")

	// Verify NO escalation was created — writ-grep on main should have
	// matched the (sol-realsquash01) tag in the squash commit message.
	sphereStore.mu.Lock()
	for _, esc := range sphereStore.escalations {
		t.Errorf("unexpected escalation after real squash merge: %s", esc.description)
	}
	sphereStore.mu.Unlock()

	// Verify the remote branch was actually deleted.
	run(t, "git", "-C", work, "fetch", "origin", "--prune")
	if err := exec.Command("git", "-C", work, "rev-parse", "--verify", "--quiet",
		"refs/remotes/origin/"+branch).Run(); err == nil {
		t.Errorf("remote branch %s should have been deleted after squash merge", branch)
	}
}

// TestIsWritLandedOnTargetMockedSignals exercises the new helper against a
// mock cmd runner to assert it issues the expected git commands and maps
// the results to the documented return values.
func TestIsWritLandedOnTargetMockedSignals(t *testing.T) {
	tests := []struct {
		name        string
		gitLogOut   []byte
		gitLogErr   error
		wantLanded  bool
		wantErr     bool
		errContains string
	}{
		{
			name:       "writ found on target",
			gitLogOut:  []byte("cafebabecafebabecafebabecafebabecafebabe\n"),
			wantLanded: true,
		},
		{
			name:       "writ not found on target",
			gitLogOut:  []byte(""),
			wantLanded: false,
		},
		{
			name:        "git log errors",
			gitLogErr:   fmt.Errorf("git log fatal"),
			wantErr:     true,
			errContains: "git log --grep failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockCmdRunner()
			mock.SetResult(
				"git log refs/remotes/origin/main --grep=sol-aaaaaaaaaaaaaaaa -n 1 --format=%H",
				tt.gitLogOut, tt.gitLogErr,
			)
			r := &Forge{
				world:    "ember",
				worktree: t.TempDir(),
				logger:   testLogger(),
				cfg:      Config{TargetBranch: "main"},
				cmd:      mock,
			}
			landed, err := r.isWritLandedOnTarget("outpost/Toast/sol-aaaaaaaaaaaaaaaa", "sol-aaaaaaaaaaaaaaaa")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if landed != tt.wantLanded {
				t.Errorf("landed = %v, want %v", landed, tt.wantLanded)
			}
		})
	}
}

