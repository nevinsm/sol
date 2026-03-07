package forge

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

func TestListReady(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: "ready", BlockedBy: ""},
		{ID: "mr-00000002", Phase: "ready", BlockedBy: "sol-blocker1"},
		{ID: "mr-00000003", Phase: "ready", BlockedBy: ""},
		{ID: "mr-00000004", Phase: "claimed", BlockedBy: ""},
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

func TestListBlocked(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", Phase: "ready", BlockedBy: ""},
		{ID: "mr-00000002", Phase: "ready", BlockedBy: "sol-blocker1"},
		{ID: "mr-00000003", Phase: "ready", BlockedBy: "sol-blocker2"},
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
		{ID: "mr-00000001", WritID: "sol-original1", Branch: "outpost/Toast/sol-original1", Phase: "claimed"},
	}

	r := &Forge{
		world:      "ember",
		agentID:  "ember/forge",
		worktree: repoDir,
		worldStore: worldStore,
		logger:   testLogger(),
		cfg:      DefaultConfig(),
	}

	mr := &store.MergeRequest{
		ID:         "mr-00000001",
		WritID: "sol-original1",
		Branch:     "outpost/Toast/sol-original1",
		Phase:      "claimed",
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
		{ID: "mr-00000001", Phase: "ready", BlockedBy: "sol-resolved1"},
		{ID: "mr-00000002", Phase: "ready", BlockedBy: "sol-pending1"},
	}
	worldStore.items["sol-resolved1"] = &store.Writ{ID: "sol-resolved1", Status: "closed"}
	worldStore.items["sol-pending1"] = &store.Writ{ID: "sol-pending1", Status: "open"}

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
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: "claimed"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID:       "sol-aaa11111",
		Title:    "Test feature",
		Status:   "done",
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
	if phase, ok := worldStore.phaseUpdates["mr-00000001"]; !ok || phase != "failed" {
		t.Errorf("MR phase = %q, want 'failed'", phase)
	}

	// Verify writ reopened.
	item := worldStore.items["sol-aaa11111"]
	if item.Status != "open" {
		t.Errorf("writ status = %q, want 'open'", item.Status)
	}
	if item.Assignee != "" {
		t.Errorf("writ assignee = %q, want empty (cleared)", item.Assignee)
	}

	// Verify escalation created.
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
}

func TestMarkMergedClosesWrit(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: "claimed"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "Test", Status: "done"}

	// Create a temp dir for git operations.
	dir := t.TempDir()
	run(t, "git", "init", dir)

	r := &Forge{
		world:      "ember",
		agentID:  "ember/forge",
		worktree: dir,
		worldStore: worldStore,
		logger:   slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		cfg:      DefaultConfig(),
	}

	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// Verify MR phase.
	if phase, ok := worldStore.phaseUpdates["mr-00000001"]; !ok || phase != "merged" {
		t.Errorf("MR phase = %q, want 'merged'", phase)
	}

	// Verify writ closed.
	if worldStore.items["sol-aaa11111"].Status != "closed" {
		t.Errorf("writ status = %q, want 'closed'", worldStore.items["sol-aaa11111"].Status)
	}
}

func TestMarkMergedSupersedesFailedSiblings(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-failed1", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: "failed"},
		{ID: "mr-failed2", WritID: "sol-aaa11111", Branch: "outpost/Blaze/sol-aaa11111", Phase: "failed"},
		{ID: "mr-merged1", WritID: "sol-aaa11111", Branch: "outpost/Nova/sol-aaa11111", Phase: "claimed"},
		{ID: "mr-other1", WritID: "sol-bbb22222", Branch: "outpost/Toast/sol-bbb22222", Phase: "failed"}, // different writ
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{ID: "sol-aaa11111", Title: "Test", Status: "done"}
	worldStore.items["sol-bbb22222"] = &store.Writ{ID: "sol-bbb22222", Title: "Other", Status: "done"}

	sphereStore := newMockSphereStore()
	// Pre-create escalations for the failed MRs.
	sphereStore.CreateEscalation("high", "ember/forge",
		"Merge failed for MR mr-failed1 (branch outpost/Toast/sol-aaa11111, writ sol-aaa11111). Writ reopened for re-dispatch.")
	sphereStore.CreateEscalation("high", "ember/forge",
		"Merge failed for MR mr-failed2 (branch outpost/Blaze/sol-aaa11111, writ sol-aaa11111). Writ reopened for re-dispatch.")
	// Escalation for different writ — should NOT be resolved.
	sphereStore.CreateEscalation("high", "ember/forge",
		"Merge failed for MR mr-other1 (branch outpost/Toast/sol-bbb22222, writ sol-bbb22222). Writ reopened for re-dispatch.")

	dir := t.TempDir()
	run(t, "git", "init", dir)

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worktree:    dir,
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		cfg:         DefaultConfig(),
	}

	if err := r.MarkMerged("mr-merged1"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	worldStore.mu.Lock()
	defer worldStore.mu.Unlock()

	// Verify merged MR is merged.
	if phase := worldStore.phaseUpdates["mr-merged1"]; phase != "merged" {
		t.Errorf("merged MR phase = %q, want 'merged'", phase)
	}

	// Verify failed sibling MRs are superseded.
	if phase := worldStore.phaseUpdates["mr-failed1"]; phase != "superseded" {
		t.Errorf("failed MR 1 phase = %q, want 'superseded'", phase)
	}
	if phase := worldStore.phaseUpdates["mr-failed2"]; phase != "superseded" {
		t.Errorf("failed MR 2 phase = %q, want 'superseded'", phase)
	}

	// Verify MR from different writ is NOT superseded.
	if _, ok := worldStore.phaseUpdates["mr-other1"]; ok {
		t.Error("MR for different writ should not be touched")
	}

	// Verify escalations for failed MRs are resolved.
	sphereStore.mu.Lock()
	defer sphereStore.mu.Unlock()
	for _, esc := range sphereStore.escalations {
		if strings.Contains(esc.description, "mr-failed1") && esc.status != "resolved" {
			t.Errorf("escalation for mr-failed1 status = %q, want 'resolved'", esc.status)
		}
		if strings.Contains(esc.description, "mr-failed2") && esc.status != "resolved" {
			t.Errorf("escalation for mr-failed2 status = %q, want 'resolved'", esc.status)
		}
		if strings.Contains(esc.description, "mr-other1") && esc.status == "resolved" {
			t.Error("escalation for different writ should NOT be resolved")
		}
	}
}

// --- Governor nudge notification tests ---

// setupGovernorNudge sets SOL_HOME and creates the governor agent dir
// so nudgeGovernor does not skip. Returns the governor session name.
func setupGovernorNudge(t *testing.T, world string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	govDir := filepath.Join(dir, world, "governor")
	os.MkdirAll(govDir, 0o755)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)
	return "sol-" + world + "-governor"
}

// drainNudges returns all nudge messages for the given session.
func drainNudges(t *testing.T, session string) []nudge.Message {
	t.Helper()
	msgs, err := nudge.List(session)
	if err != nil {
		t.Fatalf("nudge.List(%q) error: %v", session, err)
	}
	return msgs
}

func TestMarkFailedNudgesGovernor(t *testing.T) {
	govSession := setupGovernorNudge(t, "ember")

	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: "claimed"},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test", Status: "done", Assignee: "Toast",
	}

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
		cfg:         DefaultConfig(),
	}

	if err := r.MarkFailed("mr-00000001"); err != nil {
		t.Fatalf("MarkFailed() error: %v", err)
	}

	msgs := drainNudges(t, govSession)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 nudge, got %d", len(msgs))
	}
	if msgs[0].Type != "MERGE_FAILED" {
		t.Errorf("nudge type = %q, want MERGE_FAILED", msgs[0].Type)
	}
	if !strings.Contains(msgs[0].Body, "mr-00000001") {
		t.Errorf("nudge body should contain MR ID, got: %s", msgs[0].Body)
	}
}

func TestReleaseNudgesPushRejected(t *testing.T) {
	govSession := setupGovernorNudge(t, "ember")

	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: "claimed", Attempts: 1},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test", Status: "done",
	}

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
		cfg:         DefaultConfig(), // MaxAttempts=3
	}

	failed, err := r.Release("mr-00000001")
	if err != nil {
		t.Fatalf("Release() error: %v", err)
	}
	if failed {
		t.Error("Release() returned failed=true, want false (under max attempts)")
	}

	// Verify MR released back to ready.
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-00000001"]
	worldStore.mu.Unlock()
	if phase != "ready" {
		t.Errorf("MR phase = %q, want 'ready'", phase)
	}

	// Verify PUSH_REJECTED nudge.
	msgs := drainNudges(t, govSession)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 nudge, got %d", len(msgs))
	}
	if msgs[0].Type != "PUSH_REJECTED" {
		t.Errorf("nudge type = %q, want PUSH_REJECTED", msgs[0].Type)
	}
}

func TestReleaseMaxAttemptsMarksFailed(t *testing.T) {
	govSession := setupGovernorNudge(t, "ember")

	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: "claimed", Attempts: 3},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Test", Status: "done", Assignee: "Toast",
	}

	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: newMockSphereStore(),
		logger:      testLogger(),
		cfg:         DefaultConfig(), // MaxAttempts=3
	}

	failed, err := r.Release("mr-00000001")
	if err != nil {
		t.Fatalf("Release() error: %v", err)
	}
	if !failed {
		t.Error("Release() returned failed=false, want true (at max attempts)")
	}

	// Verify MR marked as failed (not released).
	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-00000001"]
	worldStore.mu.Unlock()
	if phase != "failed" {
		t.Errorf("MR phase = %q, want 'failed'", phase)
	}

	// Verify MERGE_FAILED nudge (from MarkFailed).
	msgs := drainNudges(t, govSession)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 nudge, got %d", len(msgs))
	}
	if msgs[0].Type != "MERGE_FAILED" {
		t.Errorf("nudge type = %q, want MERGE_FAILED", msgs[0].Type)
	}
}

func TestCreateResolutionTaskNudgesGovernor(t *testing.T) {
	govSession := setupGovernorNudge(t, "ember")

	// Set up a mock git repo to satisfy rev-parse.
	repoDir := t.TempDir()
	run(t, "git", "init", repoDir)
	run(t, "git", "-C", repoDir, "commit", "--allow-empty", "-m", "init")

	worldStore := newMockWorldStore()
	worldStore.items["sol-original1"] = &store.Writ{
		ID: "sol-original1", Title: "Add feature X", Priority: 2,
	}
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-original1", Branch: "outpost/Toast/sol-original1", Phase: "claimed"},
	}

	r := &Forge{
		world:      "ember",
		agentID:    "ember/forge",
		worktree:   repoDir,
		worldStore: worldStore,
		logger:     testLogger(),
		cfg:        DefaultConfig(),
	}

	mr := &store.MergeRequest{
		ID:         "mr-00000001",
		WritID: "sol-original1",
		Branch:     "outpost/Toast/sol-original1",
		Phase:      "claimed",
	}

	_, err := r.CreateResolutionTask(mr)
	if err != nil {
		t.Fatalf("CreateResolutionTask() error: %v", err)
	}

	// Verify CONFLICT_BLOCKED nudge.
	msgs := drainNudges(t, govSession)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 nudge, got %d", len(msgs))
	}
	if msgs[0].Type != "CONFLICT_BLOCKED" {
		t.Errorf("nudge type = %q, want CONFLICT_BLOCKED", msgs[0].Type)
	}
	if !strings.Contains(msgs[0].Body, "mr-00000001") {
		t.Errorf("nudge body should contain MR ID, got: %s", msgs[0].Body)
	}
}

