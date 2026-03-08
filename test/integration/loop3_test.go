package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/sentinel"
)

// --- Test 1: Mail Send and Receive ---

func TestMailSendAndReceive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(solHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Send message from operator to ember/Toast.
	msgID, err := sphereStore.SendMessage("operator", "ember/Toast",
		"Deploy config", "Please update deploy.yaml", 2, "notification")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Verify inbox.
	msgs, err := sphereStore.Inbox("ember/Toast")
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
	count, err := sphereStore.CountPending("ember/Toast")
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}
	if count != 1 {
		t.Errorf("unread count: got %d, want 1", count)
	}

	// Read the message.
	msg, err := sphereStore.ReadMessage(msgID)
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
	if err := sphereStore.AckMessage(msgID); err != nil {
		t.Fatalf("AckMessage: %v", err)
	}

	// Verify inbox is empty.
	msgs, err = sphereStore.Inbox("ember/Toast")
	if err != nil {
		t.Fatalf("Inbox after ack: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("inbox after ack: got %d, want 0", len(msgs))
	}

	// Verify unread count is 0.
	count, err = sphereStore.CountPending("ember/Toast")
	if err != nil {
		t.Fatalf("CountPending after ack: %v", err)
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

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(solHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Send AGENT_DONE protocol message.
	payload := store.AgentDonePayload{
		WritID: "sol-abc12345",
		AgentID:    "ember/Toast",
		Branch:     "outpost/Toast/sol-abc12345",
		World:        "ember",
	}
	msgID, err := sphereStore.SendProtocolMessage("ember/Toast", "ember/sentinel",
		store.ProtoAgentDone, payload)
	if err != nil {
		t.Fatalf("SendProtocolMessage: %v", err)
	}
	if msgID == "" {
		t.Fatal("expected non-empty message ID")
	}

	// Verify PendingProtocol returns it.
	msgs, err := sphereStore.PendingProtocol("ember/sentinel", store.ProtoAgentDone)
	if err != nil {
		t.Fatalf("PendingProtocol: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("pending protocol count: got %d, want 1", len(msgs))
	}

	// Parse body.
	var parsed store.AgentDonePayload
	if err := json.Unmarshal([]byte(msgs[0].Body), &parsed); err != nil {
		t.Fatalf("parse protocol body: %v", err)
	}
	if parsed.WritID != "sol-abc12345" {
		t.Errorf("writ_id: got %q, want %q", parsed.WritID, "sol-abc12345")
	}
	if parsed.Branch != "outpost/Toast/sol-abc12345" {
		t.Errorf("branch: got %q, want %q", parsed.Branch, "outpost/Toast/sol-abc12345")
	}

	// Ack the message.
	if err := sphereStore.AckMessage(msgs[0].ID); err != nil {
		t.Fatalf("AckMessage: %v", err)
	}

	// Verify empty after ack.
	msgs, err = sphereStore.PendingProtocol("ember/sentinel", store.ProtoAgentDone)
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

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	logger := events.NewLogger(solHome)

	// Emit events of different types.
	logger.Emit(events.EventCast, "sol", "operator", "both", map[string]string{"item": "1"})
	logger.Emit(events.EventResolve, "sol", "Toast", "both", map[string]string{"item": "1"})
	logger.Emit(events.EventPatrol, "ember/sentinel", "sentinel", "feed", map[string]string{"world": "ember"})

	// Read with no filter — all present.
	reader := events.NewReader(solHome, false)
	evts, err := reader.Read(events.ReadOpts{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(evts) != 3 {
		t.Errorf("unfiltered count: got %d, want 3", len(evts))
	}

	// Read with type filter.
	evts, err = reader.Read(events.ReadOpts{Type: events.EventCast})
	if err != nil {
		t.Fatalf("Read filtered: %v", err)
	}
	if len(evts) != 1 {
		t.Errorf("filtered count: got %d, want 1", len(evts))
	}
	if evts[0].Type != events.EventCast {
		t.Errorf("filtered type: got %q, want %q", evts[0].Type, events.EventCast)
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
	f, err := os.Open(filepath.Join(solHome, ".events.jsonl"))
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

// --- Test 4: Chronicle Dedup and Aggregation ---

func TestChronicleDedupAndAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	cfg := events.DefaultChronicleConfig(solHome)
	cfg.DedupWindow = 10 * time.Second
	cfg.AggWindow = 1 * time.Millisecond // Very short for testing.

	logger := events.NewLogger(solHome)

	// Write duplicate events (same type/source/actor within DedupWindow).
	logger.Emit(events.EventResolve, "sol", "Toast", "both", map[string]string{"run": "1"})
	logger.Emit(events.EventResolve, "sol", "Toast", "both", map[string]string{"run": "2"})
	logger.Emit(events.EventResolve, "sol", "Toast", "both", map[string]string{"run": "3"})

	// Write a burst of cast events (aggregatable type) with different actors
	// so they survive dedup but get aggregated.
	logger.Emit(events.EventCast, "sol", "operator-a", "both", map[string]string{"item": "a"})
	logger.Emit(events.EventCast, "sol", "operator-b", "both", map[string]string{"item": "b"})
	logger.Emit(events.EventCast, "sol", "operator-c", "both", map[string]string{"item": "c"})

	// Write unique done events (different actors).
	logger.Emit(events.EventResolve, "sol", "Jasper", "both", map[string]string{"item": "x"})
	logger.Emit(events.EventResolve, "sol", "Sage", "both", map[string]string{"item": "y"})

	// Ensure aggregation window has passed before processing.
	time.Sleep(50 * time.Millisecond)

	chronicle := events.NewChronicle(cfg)
	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	// Force flush any remaining aggregation buffers.
	if err := chronicle.FlushAllAggBuffers(); err != nil {
		t.Fatalf("FlushAllAggBuffers: %v", err)
	}

	// Read curated feed.
	reader := events.NewReader(solHome, true)
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
	// (same type+source "sol" but different actors).
	// Cast events should be aggregated into cast_batch.
	// Expect: 1 done (Toast deduped) + 1 done (Jasper) + 1 done (Sage) + 1 cast_batch = 4
	// But: Jasper and Sage done events have same type+source("sol"), different actors → not deduped.
	if typeCounts["resolve"] != 3 {
		t.Errorf("expected 3 resolve events (Toast deduped, Jasper+Sage unique), got %d", typeCounts["resolve"])
	}

	// Cast burst should produce a cast_batch.
	if typeCounts["cast_batch"] != 1 {
		t.Errorf("expected 1 cast_batch event, got %d", typeCounts["cast_batch"])
	}

	if len(curated) == 0 {
		t.Error("curated feed should not be empty")
	}
}

// --- Test 5: Chronicle Feed Truncation ---

func TestChronicleFeedTruncation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	cfg := events.DefaultChronicleConfig(solHome)
	cfg.MaxFeedSize = 1024 // 1KB
	cfg.AggWindow = 1 * time.Millisecond

	logger := events.NewLogger(solHome)

	// Write enough events to exceed the 1KB limit.
	for i := 0; i < 100; i++ {
		logger.Emit(events.EventPatrol, "sentinel", "sentinel", "feed",
			map[string]string{"world": "ember", "iteration": "patrol-data-padding-to-make-this-longer"})
	}

	time.Sleep(5 * time.Millisecond)

	chronicle := events.NewChronicle(cfg)
	if err := chronicle.ProcessOnce(); err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	if err := chronicle.FlushAllAggBuffers(); err != nil {
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

// --- Test 6: Sentinel Detects Stalled Agent ---

func TestSentinelDetectsStalledAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	registerAgentRole(t)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(solHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	worldStore, sphereStore := openStores(t, "ember")
	logger := events.NewLogger(solHome)
	mock := newMockSessionChecker()

	// Create agent with state=working, active_writ set.
	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345"); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Create writ.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'tethered', 3, 'test', ?, ?)`,
		"sol-abc12345", "Test task", now, now,
	); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	// Write tether file.
	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	// Session is dead (not in mock.alive).
	// Create worktree directory so respawn doesn't fail.
	worktreeDir := dispatch.WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("create worktree dir: %v", err)
	}

	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond
	cfg.MaxRespawns = 2

	w := sentinel.New(cfg, sphereStore, worldStore, mock, logger)

	// Run one patrol.
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Verify: respawn attempted (session started via startup.Respawn).
	mock.mu.Lock()
	startedCount := len(mock.started)
	mock.mu.Unlock()
	if startedCount != 1 {
		t.Fatalf("expected 1 session started (respawn), got %d", startedCount)
	}

	// Verify: respawn event emitted.
	assertEventEmitted(t, solHome, events.EventRespawn)
}

// --- Test 7: Sentinel Max Respawns Returns Work to Open ---

func TestSentinelMaxRespawnsReturnsWork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	registerAgentRole(t)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(solHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	worldStore, sphereStore := openStores(t, "ember")
	logger := events.NewLogger(solHome)
	mock := newMockSessionChecker()

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345"); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'tethered', 3, 'test', ?, ?)`,
		"sol-abc12345", "Test task", now, now,
	); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	if err := tether.Write("ember", "Toast", "sol-abc12345", "agent"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	worktreeDir := dispatch.WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("create worktree dir: %v", err)
	}

	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond
	cfg.MaxRespawns = 2

	w := sentinel.New(cfg, sphereStore, worldStore, mock, logger)

	// Patrol 1: stalled → respawn (attempt 1).
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("patrol 1: %v", err)
	}
	mock.mu.Lock()
	startedCount := len(mock.started)
	mock.mu.Unlock()
	if startedCount != 1 {
		t.Fatalf("patrol 1: expected 1 start, got %d", startedCount)
	}
	// Kill session.
	mock.mu.Lock()
	delete(mock.alive, "sol-ember-Toast")
	mock.mu.Unlock()

	// Patrol 2: still stalled → respawn (attempt 2).
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("patrol 2: %v", err)
	}
	mock.mu.Lock()
	startedCount = len(mock.started)
	mock.mu.Unlock()
	if startedCount != 2 {
		t.Fatalf("patrol 2: expected 2 starts, got %d", startedCount)
	}
	// Kill session again.
	mock.mu.Lock()
	delete(mock.alive, "sol-ember-Toast")
	mock.mu.Unlock()

	// Patrol 3: max reached → return to open.
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("patrol 3: %v", err)
	}
	mock.mu.Lock()
	startedCount = len(mock.started)
	mock.mu.Unlock()
	if startedCount != 2 {
		t.Fatalf("patrol 3: expected still 2 starts (max reached), got %d", startedCount)
	}

	// Agent should be idle.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state: got %q, want %q", agent.State, "idle")
	}

	// Work item should be open.
	item, err := worldStore.GetWrit("sol-abc12345")
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("writ status: got %q, want %q", item.Status, "open")
	}

	// Tether file should be removed.
	if tether.IsTethered("ember", "Toast", "agent") {
		t.Error("tether file should be removed after max respawns")
	}

	// Stalled event should be emitted.
	assertEventEmitted(t, solHome, events.EventStalled)
}

// --- Test 8: Sentinel Cleans Up Zombie Sessions ---

func TestSentinelCleanupZombies(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(solHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	_, sphereStore := openStores(t, "ember")
	logger := events.NewLogger(solHome)
	mock := newMockSessionChecker()

	// Create idle agent with no active_writ, no tether file.
	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	// Session is alive.
	mock.alive["sol-ember-Toast"] = true

	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond

	w := sentinel.New(cfg, sphereStore, nil, mock, logger)

	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Session stop called.
	if len(mock.stopped) != 1 {
		t.Fatalf("expected 1 session stopped (zombie), got %d", len(mock.stopped))
	}
	if mock.stopped[0] != "sol-ember-Toast" {
		t.Errorf("stopped session: got %q, want %q", mock.stopped[0], "sol-ember-Toast")
	}

	// Patrol event should show zombie count.
	assertEventEmitted(t, solHome, events.EventPatrol)
}

// --- Test 9: Sentinel AI Assessment — Nudge ---

func TestSentinelAIAssessmentNudge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(solHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	_, sphereStore := openStores(t, "ember")
	logger := events.NewLogger(solHome)
	mock := newMockSessionChecker()

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345"); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "same output both patrols"

	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond

	w := sentinel.New(cfg, sphereStore, nil, mock, logger)
	w.SetAssessFunc(func(agent store.Agent, sessionName, output string) (*sentinel.AssessmentResult, error) {
		return &sentinel.AssessmentResult{
			Status:          "stuck",
			Confidence:      "high",
			SuggestedAction: "nudge",
			NudgeMessage:    "Try checking the error output",
		}, nil
	})

	// Patrol 1: establishes baseline hash.
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("patrol 1: %v", err)
	}

	// Patrol 2: hash unchanged → triggers assessment → nudge.
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("patrol 2: %v", err)
	}

	// Verify: nudge injected.
	if len(mock.injected) != 1 {
		t.Fatalf("expected 1 injection (nudge), got %d", len(mock.injected))
	}
	if mock.injected[0].Text != "Try checking the error output" {
		t.Errorf("nudge text: got %q, want %q", mock.injected[0].Text, "Try checking the error output")
	}

	// Verify: assess event emitted.
	assertEventEmitted(t, solHome, events.EventAssess)
}

// --- Test 10: Sentinel AI Assessment — Low Confidence Ignored ---

func TestSentinelAIAssessmentLowConfidence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(solHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	_, sphereStore := openStores(t, "ember")
	mock := newMockSessionChecker()

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345"); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "same output"

	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond

	w := sentinel.New(cfg, sphereStore, nil, mock, nil)
	w.SetAssessFunc(func(agent store.Agent, sessionName, output string) (*sentinel.AssessmentResult, error) {
		return &sentinel.AssessmentResult{
			Status:          "stuck",
			Confidence:      "low",
			SuggestedAction: "nudge",
			NudgeMessage:    "Should not be sent",
		}, nil
	})

	// Patrol twice.
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("patrol 1: %v", err)
	}
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("patrol 2: %v", err)
	}

	// Verify: NO nudge injected.
	if len(mock.injected) != 0 {
		t.Errorf("expected 0 injections (low confidence), got %d", len(mock.injected))
	}
}

// --- Test 11: Sentinel AI Assessment Failure Non-Blocking ---

func TestSentinelAIAssessmentFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(solHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	_, sphereStore := openStores(t, "ember")
	logger := events.NewLogger(solHome)
	mock := newMockSessionChecker()

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", "sol-abc12345"); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}
	mock.alive["sol-ember-Toast"] = true
	mock.captures["sol-ember-Toast"] = "same output"

	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond

	w := sentinel.New(cfg, sphereStore, nil, mock, logger)
	w.SetAssessFunc(func(agent store.Agent, sessionName, output string) (*sentinel.AssessmentResult, error) {
		return nil, os.ErrNotExist // simulate failure
	})

	// Patrol twice.
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("patrol 1: %v", err)
	}
	err := w.Patrol(context.Background())

	// Patrol should complete without error.
	if err != nil {
		t.Errorf("patrol should succeed even when assessment fails, got: %v", err)
	}

	// No nudge, no escalation.
	if len(mock.injected) != 0 {
		t.Errorf("expected 0 injections on assessment failure, got %d", len(mock.injected))
	}

	// Patrol event should still be emitted.
	assertEventEmitted(t, solHome, events.EventPatrol)
}

// --- Test 12: Events Emitted During Dispatch ---

func TestEventsEmittedDuringDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)
	_, sourceClone := createSourceRepo(t, solHome)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()
	logger := events.NewLogger(solHome)

	// Create writ.
	itemID, err := worldStore.CreateWrit("Dispatch events test", "Test events during dispatch", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}

	// Cast with logger.
	result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		SourceRepo: sourceClone,
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Verify EventCast in feed.
	assertEventEmitted(t, solHome, events.EventCast)

	// Simulate work.
	if err := os.WriteFile(filepath.Join(result.WorktreeDir, "dispatch_test.go"),
		[]byte("package main\n\nfunc dispatchTest() {}\n"), 0o644); err != nil {
		t.Fatalf("write dispatch_test.go: %v", err)
	}

	// Resolve with logger.
	_, err = dispatch.Resolve(context.Background(), dispatch.ResolveOpts{
		World:       "ember",
		AgentName: result.AgentName,
	}, worldStore, sphereStore, mgr, logger)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Verify EventResolve in feed.
	assertEventEmitted(t, solHome, events.EventResolve)
}

// --- Test 13: Status Shows Sentinel State ---

func TestStatusShowsSentinelState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := session.New()

	// Gather status — sentinel not running.
	rs, err := status.Gather("ember", sphereStore, worldStore, worldStore, mgr)
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if rs.Sentinel.Running {
		t.Error("sentinel should not be running yet")
	}

	// Start a mock sentinel session.
	sentinelSessName := config.SessionName("ember", "sentinel")
	if err := mgr.Start(sentinelSessName, solHome, "sleep 60",
		map[string]string{"SOL_HOME": solHome}, "sentinel", "ember"); err != nil {
		t.Fatalf("start sentinel session: %v", err)
	}
	defer mgr.Stop(sentinelSessName, true)

	// Gather status — sentinel running.
	rs2, err := status.Gather("ember", sphereStore, worldStore, worldStore, mgr)
	if err != nil {
		t.Fatalf("Gather with sentinel: %v", err)
	}
	if !rs2.Sentinel.Running {
		t.Error("sentinel should be running")
	}
	if rs2.Sentinel.SessionName != sentinelSessName {
		t.Errorf("sentinel session name: got %q, want %q", rs2.Sentinel.SessionName, sentinelSessName)
	}
}
