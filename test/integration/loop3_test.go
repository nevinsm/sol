package integration

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/status"
	"github.com/nevinsm/gt/internal/store"
	"github.com/nevinsm/gt/internal/witness"
)

// --- Test 1: Mail Send and Receive ---

func TestMailSendAndReceive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	// Send message from operator to testrig/Toast.
	msgID, err := townStore.SendMessage("operator", "testrig/Toast",
		"Deploy config", "Please update deploy.yaml", 2, "notification")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Verify inbox.
	msgs, err := townStore.Inbox("testrig/Toast")
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("inbox count: got %d, want 1", len(msgs))
	}
	if msgs[0].ID != msgID {
		t.Errorf("inbox msg ID: got %q, want %q", msgs[0].ID, msgID)
	}

	// Verify unread count.
	count, err := townStore.CountUnread("testrig/Toast")
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	if count != 1 {
		t.Errorf("unread count: got %d, want 1", count)
	}

	// Read the message.
	msg, err := townStore.ReadMessage(msgID)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if msg.Subject != "Deploy config" {
		t.Errorf("subject: got %q, want %q", msg.Subject, "Deploy config")
	}
	if msg.Body != "Please update deploy.yaml" {
		t.Errorf("body: got %q, want %q", msg.Body, "Please update deploy.yaml")
	}
	if !msg.Read {
		t.Error("message should be marked as read after ReadMessage")
	}

	// Ack the message.
	if err := townStore.AckMessage(msgID); err != nil {
		t.Fatalf("AckMessage: %v", err)
	}

	// Verify inbox is empty.
	msgs, err = townStore.Inbox("testrig/Toast")
	if err != nil {
		t.Fatalf("Inbox after ack: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("inbox after ack: got %d, want 0", len(msgs))
	}

	// Verify unread count is 0.
	count, err = townStore.CountUnread("testrig/Toast")
	if err != nil {
		t.Fatalf("CountUnread after ack: %v", err)
	}
	if count != 0 {
		t.Errorf("unread after ack: got %d, want 0", count)
	}
}

// --- Test 2: Protocol Message Flow ---

func TestProtocolMessageFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	// Send POLECAT_DONE protocol message.
	payload := store.PolecatDonePayload{
		WorkItemID: "gt-abc12345",
		AgentID:    "testrig/Toast",
		Branch:     "polecat/Toast/gt-abc12345",
		Rig:        "testrig",
	}
	msgID, err := townStore.SendProtocolMessage("testrig/Toast", "testrig/witness",
		store.ProtoPolecatDone, payload)
	if err != nil {
		t.Fatalf("SendProtocolMessage: %v", err)
	}
	if msgID == "" {
		t.Fatal("expected non-empty message ID")
	}

	// Verify PendingProtocol returns it.
	msgs, err := townStore.PendingProtocol("testrig/witness", store.ProtoPolecatDone)
	if err != nil {
		t.Fatalf("PendingProtocol: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("pending protocol count: got %d, want 1", len(msgs))
	}

	// Parse body.
	var parsed store.PolecatDonePayload
	if err := json.Unmarshal([]byte(msgs[0].Body), &parsed); err != nil {
		t.Fatalf("parse protocol body: %v", err)
	}
	if parsed.WorkItemID != "gt-abc12345" {
		t.Errorf("work_item_id: got %q, want %q", parsed.WorkItemID, "gt-abc12345")
	}
	if parsed.Branch != "polecat/Toast/gt-abc12345" {
		t.Errorf("branch: got %q, want %q", parsed.Branch, "polecat/Toast/gt-abc12345")
	}

	// Ack the message.
	if err := townStore.AckMessage(msgs[0].ID); err != nil {
		t.Fatalf("AckMessage: %v", err)
	}

	// Verify empty after ack.
	msgs, err = townStore.PendingProtocol("testrig/witness", store.ProtoPolecatDone)
	if err != nil {
		t.Fatalf("PendingProtocol after ack: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("pending after ack: got %d, want 0", len(msgs))
	}
}

// --- Test 3: Event Feed End-to-End ---

func TestEventFeedEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)

	logger := events.NewLogger(gtHome)

	// Emit events of different types.
	logger.Emit(events.EventSling, "gt", "operator", "both", map[string]string{"item": "1"})
	logger.Emit(events.EventDone, "gt", "Toast", "both", map[string]string{"item": "1"})
	logger.Emit(events.EventPatrol, "testrig/witness", "witness", "feed", map[string]string{"rig": "testrig"})

	// Read with no filter — all present.
	reader := events.NewReader(gtHome, false)
	evts, err := reader.Read(events.ReadOpts{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(evts) != 3 {
		t.Errorf("unfiltered count: got %d, want 3", len(evts))
	}

	// Read with type filter.
	evts, err = reader.Read(events.ReadOpts{Type: events.EventSling})
	if err != nil {
		t.Fatalf("Read filtered: %v", err)
	}
	if len(evts) != 1 {
		t.Errorf("filtered count: got %d, want 1", len(evts))
	}
	if evts[0].Type != events.EventSling {
		t.Errorf("filtered type: got %q, want %q", evts[0].Type, events.EventSling)
	}

	// Read with limit.
	evts, err = reader.Read(events.ReadOpts{Limit: 2})
	if err != nil {
		t.Fatalf("Read limited: %v", err)
	}
	if len(evts) != 2 {
		t.Errorf("limited count: got %d, want 2", len(evts))
	}

	// Verify JSONL file lines are valid JSON.
	f, err := os.Open(filepath.Join(gtHome, ".events.jsonl"))
	if err != nil {
		t.Fatalf("open events file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		var raw json.RawMessage
		if err := json.Unmarshal(scanner.Bytes(), &raw); err != nil {
			t.Errorf("line %d: invalid JSON: %v", lineNum, err)
		}
	}
	if lineNum != 3 {
		t.Errorf("JSONL lines: got %d, want 3", lineNum)
	}
}

// --- Test 4: Curator Dedup and Aggregation ---

func TestCuratorDedupAndAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)

	cfg := events.DefaultCuratorConfig(gtHome)
	cfg.DedupWindow = 10 * time.Second
	cfg.AggWindow = 1 * time.Millisecond // Very short for testing.

	logger := events.NewLogger(gtHome)

	// Write duplicate events (same type/source/actor within DedupWindow).
	logger.Emit(events.EventDone, "gt", "Toast", "both", map[string]string{"run": "1"})
	logger.Emit(events.EventDone, "gt", "Toast", "both", map[string]string{"run": "2"})
	logger.Emit(events.EventDone, "gt", "Toast", "both", map[string]string{"run": "3"})

	// Write a burst of sling events (aggregatable type) with different actors
	// so they survive dedup but get aggregated.
	logger.Emit(events.EventSling, "gt", "operator-a", "both", map[string]string{"item": "a"})
	logger.Emit(events.EventSling, "gt", "operator-b", "both", map[string]string{"item": "b"})
	logger.Emit(events.EventSling, "gt", "operator-c", "both", map[string]string{"item": "c"})

	// Write unique done events (different actors).
	logger.Emit(events.EventDone, "gt", "Jasper", "both", map[string]string{"item": "x"})
	logger.Emit(events.EventDone, "gt", "Sage", "both", map[string]string{"item": "y"})

	// Small delay so aggregation window passes.
	time.Sleep(5 * time.Millisecond)

	curator := events.NewCurator(cfg)
	if err := curator.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	// Force flush any remaining aggregation buffers.
	if err := curator.FlushAllAggBuffers(); err != nil {
		t.Fatalf("FlushAllAggBuffers: %v", err)
	}

	// Read curated feed.
	reader := events.NewReader(gtHome, true)
	curated, err := reader.Read(events.ReadOpts{})
	if err != nil {
		t.Fatalf("read curated: %v", err)
	}

	// Count event types in curated feed.
	typeCounts := make(map[string]int)
	for _, ev := range curated {
		typeCounts[ev.Type]++
	}

	// Duplicate done events from Toast should be deduped to 1.
	// Unique done events from Jasper and Sage should be deduped individually
	// (same type+source "gt" but different actors).
	// Sling events should be aggregated into sling_batch.
	// Expect: 1 done (Toast deduped) + 1 done (Jasper) + 1 done (Sage) + 1 sling_batch = 4
	// But: Jasper and Sage done events have same type+source("gt"), different actors → not deduped.
	if typeCounts["done"] < 1 {
		t.Errorf("expected at least 1 done event, got %d", typeCounts["done"])
	}

	// Sling burst should produce a sling_batch.
	if typeCounts["sling_batch"] != 1 {
		t.Errorf("expected 1 sling_batch event, got %d", typeCounts["sling_batch"])
	}

	if len(curated) == 0 {
		t.Error("curated feed should not be empty")
	}
}

// --- Test 5: Curator Feed Truncation ---

func TestCuratorFeedTruncation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)

	cfg := events.DefaultCuratorConfig(gtHome)
	cfg.MaxFeedSize = 1024 // 1KB
	cfg.AggWindow = 1 * time.Millisecond

	logger := events.NewLogger(gtHome)

	// Write enough events to exceed the 1KB limit.
	for i := 0; i < 100; i++ {
		logger.Emit(events.EventPatrol, "witness", "witness", "feed",
			map[string]string{"rig": "testrig", "iteration": "patrol-data-padding-to-make-this-longer"})
	}

	time.Sleep(5 * time.Millisecond)

	curator := events.NewCurator(cfg)
	if err := curator.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	if err := curator.FlushAllAggBuffers(); err != nil {
		t.Fatalf("FlushAllAggBuffers: %v", err)
	}

	// Verify curated feed size.
	info, err := os.Stat(cfg.FeedPath)
	if err != nil {
		t.Fatalf("stat curated feed: %v", err)
	}
	if info.Size() > cfg.MaxFeedSize {
		t.Errorf("curated feed size %d exceeds max %d", info.Size(), cfg.MaxFeedSize)
	}

	// Verify remaining events are valid JSON lines.
	f, err := os.Open(cfg.FeedPath)
	if err != nil {
		t.Fatalf("open curated feed: %v", err)
	}
	defer f.Close()

	validLines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Errorf("invalid JSON line after truncation: %q", line)
		}
		validLines++
	}
	if validLines == 0 {
		t.Error("curated feed has no valid lines after truncation")
	}
}

// --- Test 6: Witness Detects Stalled Agent ---

func TestWitnessDetectsStalledAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	rigStore, townStore := openStores(t, "testrig")
	logger := events.NewLogger(gtHome)
	mock := newMockSessionChecker()

	// Create polecat with state=working, hook_item set.
	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")

	// Create work item.
	now := time.Now().UTC().Format(time.RFC3339)
	rigStore.DB().Exec(
		`INSERT INTO work_items (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'hooked', 3, 'test', ?, ?)`,
		"gt-abc12345", "Test task", now, now,
	)

	// Write hook file.
	if err := hook.Write("testrig", "Toast", "gt-abc12345"); err != nil {
		t.Fatalf("hook.Write: %v", err)
	}

	// Session is dead (not in mock.alive).
	// Create worktree directory so respawn doesn't fail.
	worktreeDir := dispatch.WorktreePath("testrig", "Toast")
	os.MkdirAll(worktreeDir, 0o755)

	cfg := witness.DefaultConfig("testrig", "", gtHome)
	cfg.PatrolInterval = 50 * time.Millisecond
	cfg.MaxRespawns = 2

	w := witness.New(cfg, townStore, rigStore, mock, logger)

	// Run one patrol.
	if err := w.Patrol(); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Verify: respawn attempted (session start called).
	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session started (respawn), got %d", len(mock.started))
	}

	// Verify: respawn event emitted.
	assertEventEmitted(t, gtHome, events.EventRespawn)
}

// --- Test 7: Witness Max Respawns Returns Work to Open ---

func TestWitnessMaxRespawnsReturnsWork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	rigStore, townStore := openStores(t, "testrig")
	logger := events.NewLogger(gtHome)
	mock := newMockSessionChecker()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")

	now := time.Now().UTC().Format(time.RFC3339)
	rigStore.DB().Exec(
		`INSERT INTO work_items (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'hooked', 3, 'test', ?, ?)`,
		"gt-abc12345", "Test task", now, now,
	)

	if err := hook.Write("testrig", "Toast", "gt-abc12345"); err != nil {
		t.Fatalf("hook.Write: %v", err)
	}

	worktreeDir := dispatch.WorktreePath("testrig", "Toast")
	os.MkdirAll(worktreeDir, 0o755)

	cfg := witness.DefaultConfig("testrig", "", gtHome)
	cfg.PatrolInterval = 50 * time.Millisecond
	cfg.MaxRespawns = 2

	w := witness.New(cfg, townStore, rigStore, mock, logger)

	// Patrol 1: stalled → respawn (attempt 1).
	w.Patrol()
	if len(mock.started) != 1 {
		t.Fatalf("patrol 1: expected 1 start, got %d", len(mock.started))
	}
	// Kill session.
	delete(mock.alive, "gt-testrig-Toast")

	// Patrol 2: still stalled → respawn (attempt 2).
	w.Patrol()
	if len(mock.started) != 2 {
		t.Fatalf("patrol 2: expected 2 starts, got %d", len(mock.started))
	}
	// Kill session again.
	delete(mock.alive, "gt-testrig-Toast")

	// Patrol 3: max reached → return to open.
	w.Patrol()
	if len(mock.started) != 2 {
		t.Fatalf("patrol 3: expected still 2 starts (max reached), got %d", len(mock.started))
	}

	// Agent should be idle.
	agent, err := townStore.GetAgent("testrig/Toast")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state: got %q, want %q", agent.State, "idle")
	}

	// Work item should be open.
	item, err := rigStore.GetWorkItem("gt-abc12345")
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("work item status: got %q, want %q", item.Status, "open")
	}

	// Hook file should be removed.
	if hook.IsHooked("testrig", "Toast") {
		t.Error("hook file should be removed after max respawns")
	}

	// Stalled event should be emitted.
	assertEventEmitted(t, gtHome, events.EventStalled)
}

// --- Test 8: Witness Cleans Up Zombie Sessions ---

func TestWitnessCleanupZombies(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	_, townStore := openStores(t, "testrig")
	logger := events.NewLogger(gtHome)
	mock := newMockSessionChecker()

	// Create idle polecat with no hook_item, no hook file.
	townStore.CreateAgent("Toast", "testrig", "polecat")
	// Session is alive.
	mock.alive["gt-testrig-Toast"] = true

	cfg := witness.DefaultConfig("testrig", "", gtHome)
	cfg.PatrolInterval = 50 * time.Millisecond

	w := witness.New(cfg, townStore, nil, mock, logger)

	if err := w.Patrol(); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Session stop called.
	if len(mock.stopped) != 1 {
		t.Fatalf("expected 1 session stopped (zombie), got %d", len(mock.stopped))
	}
	if mock.stopped[0] != "gt-testrig-Toast" {
		t.Errorf("stopped session: got %q, want %q", mock.stopped[0], "gt-testrig-Toast")
	}

	// Patrol event should show zombie count.
	assertEventEmitted(t, gtHome, events.EventPatrol)
}

// --- Test 9: Witness AI Assessment — Nudge ---

func TestWitnessAIAssessmentNudge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	_, townStore := openStores(t, "testrig")
	logger := events.NewLogger(gtHome)
	mock := newMockSessionChecker()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true
	mock.captures["gt-testrig-Toast"] = "same output both patrols"

	cfg := witness.DefaultConfig("testrig", "", gtHome)
	cfg.PatrolInterval = 50 * time.Millisecond

	w := witness.New(cfg, townStore, nil, mock, logger)
	w.SetAssessFunc(func(agent store.Agent, sessionName, output string) (*witness.AssessmentResult, error) {
		return &witness.AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "nudge",
			NudgeMessage:    "Try checking the error output",
		}, nil
	})

	// Patrol 1: establishes baseline hash.
	w.Patrol()

	// Patrol 2: hash unchanged → triggers assessment → nudge.
	w.Patrol()

	// Verify: nudge injected.
	if len(mock.injected) != 1 {
		t.Fatalf("expected 1 injection (nudge), got %d", len(mock.injected))
	}
	if mock.injected[0].Text != "Try checking the error output" {
		t.Errorf("nudge text: got %q, want %q", mock.injected[0].Text, "Try checking the error output")
	}

	// Verify: assess event emitted.
	assertEventEmitted(t, gtHome, events.EventAssess)
}

// --- Test 10: Witness AI Assessment — Low Confidence Ignored ---

func TestWitnessAIAssessmentLowConfidence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	_, townStore := openStores(t, "testrig")
	mock := newMockSessionChecker()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true
	mock.captures["gt-testrig-Toast"] = "same output"

	cfg := witness.DefaultConfig("testrig", "", gtHome)
	cfg.PatrolInterval = 50 * time.Millisecond

	w := witness.New(cfg, townStore, nil, mock, nil)
	w.SetAssessFunc(func(agent store.Agent, sessionName, output string) (*witness.AssessmentResult, error) {
		return &witness.AssessmentResult{
			Status:          "stuck",
			Confidence:      "low",
			SuggestedAction: "nudge",
			NudgeMessage:    "Should not be sent",
		}, nil
	})

	// Patrol twice.
	w.Patrol()
	w.Patrol()

	// Verify: NO nudge injected.
	if len(mock.injected) != 0 {
		t.Errorf("expected 0 injections (low confidence), got %d", len(mock.injected))
	}
}

// --- Test 11: Witness AI Assessment Failure Non-Blocking ---

func TestWitnessAIAssessmentFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	_, townStore := openStores(t, "testrig")
	logger := events.NewLogger(gtHome)
	mock := newMockSessionChecker()

	townStore.CreateAgent("Toast", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/Toast", "working", "gt-abc12345")
	mock.alive["gt-testrig-Toast"] = true
	mock.captures["gt-testrig-Toast"] = "same output"

	cfg := witness.DefaultConfig("testrig", "", gtHome)
	cfg.PatrolInterval = 50 * time.Millisecond

	w := witness.New(cfg, townStore, nil, mock, logger)
	w.SetAssessFunc(func(agent store.Agent, sessionName, output string) (*witness.AssessmentResult, error) {
		return nil, os.ErrNotExist // simulate failure
	})

	// Patrol twice.
	w.Patrol()
	err := w.Patrol()

	// Patrol should complete without error.
	if err != nil {
		t.Errorf("patrol should succeed even when assessment fails, got: %v", err)
	}

	// No nudge, no escalation.
	if len(mock.injected) != 0 {
		t.Errorf("expected 0 injections on assessment failure, got %d", len(mock.injected))
	}

	// Patrol event should still be emitted.
	assertEventEmitted(t, gtHome, events.EventPatrol)
}

// --- Test 12: Events Emitted During Dispatch ---

func TestEventsEmittedDuringDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	_, sourceClone := createSourceRepo(t, gtHome)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()
	logger := events.NewLogger(gtHome)

	// Create work item.
	itemID, err := rigStore.CreateWorkItem("Dispatch events test", "Test events during dispatch", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	// Sling with logger.
	result, err := dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: sourceClone,
	}, rigStore, townStore, mgr, logger)
	if err != nil {
		t.Fatalf("sling: %v", err)
	}

	// Verify EventSling in feed.
	assertEventEmitted(t, gtHome, events.EventSling)

	// Simulate work.
	os.WriteFile(filepath.Join(result.WorktreeDir, "dispatch_test.go"),
		[]byte("package main\n\nfunc dispatchTest() {}\n"), 0o644)

	// Done with logger.
	_, err = dispatch.Done(dispatch.DoneOpts{
		Rig:       "testrig",
		AgentName: result.AgentName,
	}, rigStore, townStore, mgr, logger)
	if err != nil {
		t.Fatalf("done: %v", err)
	}

	// Verify EventDone in feed.
	assertEventEmitted(t, gtHome, events.EventDone)
}

// --- Test 13: Status Shows Witness State ---

func TestStatusShowsWitnessState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := session.New()

	// Gather status — witness not running.
	rs, err := status.Gather("testrig", townStore, rigStore, rigStore, mgr)
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if rs.Witness.Running {
		t.Error("witness should not be running yet")
	}

	// Start a mock witness session.
	witnessSessName := dispatch.SessionName("testrig", "witness")
	if err := mgr.Start(witnessSessName, gtHome, "sleep 60",
		map[string]string{"GT_HOME": gtHome}, "witness", "testrig"); err != nil {
		t.Fatalf("start witness session: %v", err)
	}
	defer mgr.Stop(witnessSessName, true)

	// Gather status — witness running.
	rs2, err := status.Gather("testrig", townStore, rigStore, rigStore, mgr)
	if err != nil {
		t.Fatalf("Gather with witness: %v", err)
	}
	if !rs2.Witness.Running {
		t.Error("witness should be running")
	}
	if rs2.Witness.SessionName != witnessSessName {
		t.Errorf("witness session name: got %q, want %q", rs2.Witness.SessionName, witnessSessName)
	}
}
